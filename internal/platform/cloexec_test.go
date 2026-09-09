package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const descriptorHelperTimeout = 10 * time.Second

func descriptorArguments(fd int) ([]string, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, err
	}
	return []string{"-test.run=^TestScopedDescriptorHelper$", "--", "check", strconv.Itoa(fd),
		strconv.FormatUint(st.Dev, 10), strconv.FormatUint(st.Ino, 10)}, nil
}

// TestScopedDescriptorHelper executes only in an explicitly selected subprocess.
func TestScopedDescriptorHelper(t *testing.T) {
	const argumentCount = 4
	index := 0
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index == 0 {
		return
	}
	args := os.Args[index:]
	if args[0] == "check" {
		if len(args) != argumentCount {
			t.Fatal("invalid descriptor check arguments")
		}
		fd, err := strconv.Atoi(args[1])
		if err != nil {
			t.Fatal(err)
		}
		dev, err := strconv.ParseUint(args[2], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		ino, err := strconv.ParseUint(args[3], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err == nil && st.Dev == dev && st.Ino == ino {
			t.Fatalf("scoped descriptor %d survived exec", fd)
		}
		return
	}
	// Create the operation root before freeing stdin. The root case instead
	// frees stdin immediately before opening the root. No parent process loses stdin.
	var reader ScopedReader
	var err error
	if args[0] == "operation-zero" {
		reader, err = NewScopedOSReader(args[1])
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Close(0); err != nil {
		t.Fatal(err)
	}
	replace := func(fd int) error {
		if fd != 0 {
			return fmt.Errorf("allocated fd %d, expected zero", fd)
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			return err
		}
		if flags&unix.FD_CLOEXEC == 0 {
			return fmt.Errorf("FD zero lacks close-on-exec")
		}
		childArgs, err := descriptorArguments(fd)
		if err != nil {
			return err
		}
		// Exec replaces this helper without os/exec's stdin remapping. This is
		// essential: a child with /dev/null on stdin would not test CLOEXEC on FD 0.
		return syscall.Exec(os.Args[0], append([]string{os.Args[0]}, childArgs...), os.Environ())
	}
	if args[0] == "root-zero" {
		reader, err = NewScopedOSReader(args[1])
		if err != nil {
			t.Fatal(err)
		}
		fd, ferr := reader.(*ScopedOSReader).rootFD()
		if ferr != nil {
			t.Fatal(ferr)
		}
		t.Fatal(replace(fd))
	}
	t.Fatal(reader.(*ScopedOSReader).readSubpath("file", unix.O_RDONLY, replace))
}

func TestScopedDescriptorsCloseOnExec(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("measured"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	reader, err := NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	// Subtests finish before Cleanup runs; readers are never closed during I/O.
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	scoped := reader.(*ScopedOSReader)
	check := func(t *testing.T, fd int) {
		t.Helper()
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			t.Fatal(err)
		}
		if flags&unix.FD_CLOEXEC == 0 {
			t.Errorf("fd %d lacks FD_CLOEXEC", fd)
		}
		args, err := descriptorArguments(fd)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), descriptorHelperTimeout)
		defer cancel()
		result, err := NewOSCommandRunner(descriptorHelperTimeout).Run(ctx, os.Args[0], args...)
		if err != nil {
			t.Errorf("exec isolation: %v\n%s%s", err, result.Stdout, result.Stderr)
		}
		// The parent descriptor remains usable after the child's exec.
		if _, err := descriptorArguments(fd); err != nil {
			t.Error(err)
		}
	}
	fd, err := scoped.rootFD()
	if err != nil {
		t.Fatal(err)
	}
	check(t, fd)
	tests := []struct {
		name, path string
		flags      uint64
	}{
		{"read", "file", unix.O_RDONLY}, {"stat", "file", unix.O_PATH},
		{"directory", ".", unix.O_RDONLY | unix.O_DIRECTORY}, {"link", "link", unix.O_PATH | unix.O_NOFOLLOW},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := scoped.readSubpath(tt.path, tt.flags, func(fd int) error { check(t, fd); return nil }); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestScopedDescriptorsZeroExec(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"root-zero", "operation-zero"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "file"), []byte("zero"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), descriptorHelperTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScopedDescriptorHelper$", "--", mode, root)
			cmd.WaitDelay = time.Second
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
		})
	}
}
