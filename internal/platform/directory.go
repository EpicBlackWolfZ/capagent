package platform

import (
	"context"
	"errors"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// captureDirectory bounds encountered names before metadata lookup. Only ENOENT
// may disappear. Other metadata errors discard the result; limits and cancellation
// return a sorted eager subset which is explicitly incomplete.
func captureDirectory(ctx context.Context, limits ReadLimits, names iter.Seq2[string, error],
	lookup func(string) (os.FileInfo, error)) ([]os.DirEntry, error) {
	out := make([]os.DirEntry, 0)
	finish := func(err error) ([]os.DirEntry, error) {
		sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
		return out, incomplete(err)
	}
	count, bytes := 0, 0
	if err := ctx.Err(); err != nil {
		return finish(err)
	}
	for name, err := range names {
		if err != nil {
			return finish(errors.Join(err, ctx.Err()))
		}
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		if name == "." || name == ".." {
			continue
		}
		if count == limits.DirectoryEntries {
			return finish(&LimitError{Resource: "directory entries", Limit: limits.DirectoryEntries})
		}
		if len(name) > limits.DirectoryNameBytes-bytes {
			return finish(&LimitError{Resource: "directory name bytes", Limit: limits.DirectoryNameBytes})
		}
		count++
		bytes += len(name)
		info, err := lookup(name)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return nil, pathError("readdir metadata", name, errors.Join(err, ctx.Err()))
		}
		out = append(out, fs.FileInfoToDirEntry(info))
	}
	return finish(ctx.Err())
}

func readDirectoryFD(ctx context.Context, fd int, limits ReadLimits) ([]os.DirEntry, error) {
	return captureDirectory(ctx, limits, directoryNames(ctx, func(p []byte) (int, error) { return unix.Getdents(fd, p) }),
		func(name string) (os.FileInfo, error) {
			var st unix.Stat_t
			if err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return nil, err
			}
			return statInfo(name, &st), nil
		})
}

func directoryNames(ctx context.Context, read func([]byte) (int, error)) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		scratch := make([]byte, ioChunkBytes)
		retries := 0
		for {
			if err := ctx.Err(); err != nil {
				yield("", err)
				return
			}
			n, err := read(scratch)
			if errors.Is(err, unix.EINTR) && retries < interruptedRetries {
				retries++
				continue
			}
			if err != nil {
				yield("", err)
				return
			}
			if n == 0 {
				return
			}
			retries = 0
			// Only one fixed-size getdents chunk is decoded at a time.
			_, _, names := unix.ParseDirent(scratch[:n], -1, nil)
			for _, name := range names {
				if !yield(name, nil) {
					return
				}
			}
		}
	}
}

func (m *MemPlatformReader) captureChildren(ctx context.Context, dir string, limits ReadLimits) ([]os.DirEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefix := strings.TrimRight(dir, "/") + "/"
	if dir == "." {
		prefix = ""
	}
	names := func(yield func(string, error) bool) {
		for path := range m.files {
			if err := ctx.Err(); err != nil {
				yield("", err)
				return
			}
			if path == dir || !strings.HasPrefix(path, prefix) {
				continue
			}
			name := strings.TrimPrefix(path, prefix)
			if name != "" && !strings.Contains(name, "/") {
				if !yield(name, nil) {
					return
				}
			}
		}
	}
	return captureDirectory(ctx, limits, names, func(name string) (os.FileInfo, error) {
		node := m.files[filepath.Join(dir, name)]
		if node.ForcedErr != nil {
			return nil, node.ForcedErr
		}
		return virtualInfo(name, node), nil
	})
}
