package platform

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const interruptedRetries = 8

type descriptorOperation func(flags uint64, fn func(int) error) error

// checkedRegular rejects observed special files before data-open. Both opens
// use the caller's authority. A replacement is rejected before reading, but
// preflight is not an atomic regular-only open: hostile device replacement and
// unresponsive filesystem drivers are outside the cooperative I/O contract.
func checkedRegular(ctx context.Context, withFD descriptorOperation, fn func(int) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return withFD(unix.O_PATH, func(fd int) error {
		var before unix.Stat_t
		if err := unix.Fstat(fd, &before); err != nil {
			return err
		}
		if err := regularMode(scopedStatMode(before.Mode)); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return withFD(unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY, func(dataFD int) error {
			var after unix.Stat_t
			if err := unix.Fstat(dataFD, &after); err != nil {
				return err
			}
			if err := regularMode(scopedStatMode(after.Mode)); err != nil {
				return err
			}
			if before.Dev != after.Dev || before.Ino != after.Ino {
				return ErrFileChanged
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return fn(dataFD)
		})
	})
}

func regularMode(mode os.FileMode) error {
	if mode.IsDir() {
		return unix.EISDIR
	}
	if !mode.IsRegular() {
		return ErrFileType
	}
	return nil
}

// readBounded retains at most limit bytes, inspecting one extra byte to prove
// overflow. Scratch space is fixed; EINTR retries are finite and cancellable.
func readBounded(ctx context.Context, limit int, read func([]byte) (int, error)) ([]byte, error) {
	var data []byte
	scratch := make([]byte, min(ioChunkBytes, limit+1))
	retries := 0
	for {
		if err := ctx.Err(); err != nil {
			return data, incomplete(err)
		}
		n, err := read(scratch[:min(len(scratch), limit-len(data)+1)])
		remaining := limit - len(data)
		if n > 0 {
			data = append(data, scratch[:min(n, remaining)]...)
			retries = 0
		}
		if n > remaining {
			return data, errors.Join(&LimitError{Resource: "file bytes", Limit: limit}, incomplete(err), incomplete(ctx.Err()))
		}
		if errors.Is(err, io.EOF) {
			return data, incomplete(ctx.Err())
		}
		if err != nil {
			if errors.Is(err, unix.EINTR) && retries < interruptedRetries {
				retries++
				continue
			}
			return data, incomplete(errors.Join(err, ctx.Err()))
		}
		if n == 0 {
			return data, incomplete(ctx.Err())
		}
	}
}

func readAllFD(ctx context.Context, fd, limit int) ([]byte, error) {
	return readBounded(ctx, limit, func(p []byte) (int, error) { return unix.Read(fd, p) })
}

func readVirtual(ctx context.Context, node *VirtualFile, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, incomplete(err)
	}
	if err := regularMode(node.Mode); err != nil {
		return nil, err
	}
	n := min(len(node.Content), limit)
	data := append([]byte(nil), node.Content[:n]...)
	if len(node.Content) > limit {
		return data, &LimitError{Resource: "file bytes", Limit: limit}
	}
	return data, incomplete(ctx.Err())
}

// withHostFD is only for explicitly trusted host paths; untrusted subpaths
// always use ScopedOSReader.readSubpath and its openat2 confinement.
func withHostFD(path string, flags uint64, fn func(int) error) error {
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: flags | unix.O_CLOEXEC})
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	// The callback borrows the descriptor. Closing it here is the only close path.
	defer func() { _ = unix.Close(fd) }() // Close is not retried: the kernel may already have released the descriptor.
	return fn(fd)
}
