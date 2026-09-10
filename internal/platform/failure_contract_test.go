package platform

import (
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func namesForTest(names ...string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for _, name := range names {
			if !yield(name, nil) {
				return
			}
		}
	}
}

func TestDirectoryInterruptedAndIncomplete(t *testing.T) {
	t.Parallel()
	limits := DefaultReadLimits()
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	entries, err := captureDirectory(ctx, limits, namesForTest("a", "b"), func(name string) (os.FileInfo, error) {
		calls++
		cancel()
		return &memFileInfo{name: name}, nil
	})
	if calls != 1 || len(entries) != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%d %v %v", calls, entries, err)
	}
	calls = 0
	for _, err := range directoryNames(t.Context(), func([]byte) (int, error) { calls++; return 0, unix.EINTR }) {
		if !errors.Is(err, unix.EINTR) {
			t.Fatal(err)
		}
	}
	if calls != interruptedRetries+1 {
		t.Fatalf("unbounded retries: %d", calls)
	}
	for _, err := range directoryNames(ctx, func([]byte) (int, error) { t.Fatal("getdents after cancel"); return 0, nil }) {
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if _, err := readDirectoryFD(t.Context(), -1, limits); !errors.Is(err, unix.EBADF) {
		t.Fatal(err)
	}
	_, err = captureDirectory(t.Context(), limits, func(yield func(string, error) bool) { yield("", unix.EIO) },
		func(string) (os.FileInfo, error) { t.Fatal("metadata after enumeration error"); return nil, nil })
	if !errors.Is(err, unix.EIO) || !errors.Is(err, ErrIncomplete) {
		t.Fatal(err)
	}
	limits.DirectoryEntries = 1
	entries, err = captureDirectory(t.Context(), limits, namesForTest("gone", "another"),
		func(string) (os.FileInfo, error) { return nil, unix.ENOENT })
	if len(entries) != 0 || !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("ENOENT did not consume budget: %v %v", entries, err)
	}
	m := NewMemPlatformReader()
	if _, err := m.captureChildren(ctx, "/", limits); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestReadInterrupted(t *testing.T) {
	t.Parallel()
	calls := 0
	_, err := readBounded(t.Context(), 1, func([]byte) (int, error) { calls++; return 0, unix.EINTR })
	if calls != interruptedRetries+1 || !errors.Is(err, unix.EINTR) {
		t.Fatalf("retries=%d %v", calls, err)
	}
	for _, cause := range []error{unix.EAGAIN, unix.EIO} {
		_, err := readBounded(t.Context(), 1, func([]byte) (int, error) { return 0, cause })
		if !errors.Is(err, cause) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readVirtual(ctx, &VirtualFile{}, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCheckedOpenFailureBoundaries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, fifoFixtureName)
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name              string
		invalid, cancelAt int
		replace           bool
		want              error
	}{
		{name: "bad preflight FD", invalid: 1, want: unix.EBADF},
		{name: "bad data FD", invalid: 2, want: unix.EBADF},
		{name: "cancel after preflight", cancelAt: 1, want: context.Canceled},
		{name: "cancel after data open", cancelAt: 2, want: context.Canceled},
		{name: "replacement FIFO", replace: true, want: ErrFileType},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			opens := 0
			err := checkedRegular(ctx, func(flags uint64, fn func(int) error) error {
				opens++
				if opens == tt.invalid {
					return fn(-1)
				}
				target := path
				if tt.replace && opens == 2 {
					if flags&unix.O_NONBLOCK == 0 {
						t.Fatal("replacement FIFO must use nonblocking open")
					}
					target = fifo
				}
				return withHostFD(target, flags, func(fd int) error {
					if opens == tt.cancelAt {
						cancel()
					}
					return fn(fd)
				})
			}, func(int) error { t.Fatal("unsafe callback"); return nil })
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestFixtureErrorsAndSpecialNodes(t *testing.T) {
	t.Parallel()
	m := NewMemPlatformReader()
	m.AddDir("/", 0o755)
	for _, mode := range []os.FileMode{os.ModeNamedPipe, os.ModeSocket, os.ModeDevice, os.ModeDevice | os.ModeCharDevice, os.ModeIrregular} {
		if err := m.AddSpecial("/special", mode|0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := m.ReadFile(t.Context(), "/special"); !errors.Is(err, ErrFileType) {
			t.Fatal(err)
		}

		scoped := NewScopedMemReader("/", m)
		if _, err := scoped.ReadFile(t.Context(), "special"); !errors.Is(err, ErrFileType) {
			t.Fatal(err)
		}
		if _, err := scoped.FileCapabilities(t.Context(), "special"); !errors.Is(err, ErrFileType) {
			t.Fatal(err)
		}
	}
	for _, mode := range []os.FileMode{0, os.ModeDir, os.ModeCharDevice} {
		if err := m.AddSpecial("/bad", mode); !errors.Is(err, unix.EINVAL) {
			t.Fatal(err)
		}
	}
	if err := m.AddSpecial("", os.ModeSocket); err == nil {
		t.Fatal("empty special path accepted")
	}
	if err := m.SetOwnership("", FileOwnership{}); err == nil {
		t.Fatal("empty owner path accepted")
	}
	if err := m.SetOwnership("missing", FileOwnership{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, known := OwnershipOf(nil); known {
		t.Fatal("nil owner known")
	}
	if _, known := OwnershipOf(unknownFileInfo{}); known {
		t.Fatal("unknown owner known")
	}
	if mapOpenError(nil, "") != nil {
		t.Fatal("nil error changed")
	}
}

type unknownFileInfo struct{ os.FileInfo }

func (unknownFileInfo) Sys() any { return nil }

func TestCapabilitiesScopedFixtureAndFailures(t *testing.T) {
	t.Parallel()
	m := NewMemPlatformReader()
	m.AddDir("/", 0o755)
	m.AddFile("/file", nil, 0o600)
	scoped := NewScopedMemReader("/", m)
	for _, cause := range []error{nil, unix.ENOTSUP, unix.EACCES, unix.EIO} {
		if err := m.SetFileCapabilities("/file", CapabilityAttribute{Present: true, Bytes: []byte("raw")}, cause); err != nil {
			t.Fatal(err)
		}
		got, err := scoped.FileCapabilities(t.Context(), "file")
		if !errors.Is(err, cause) {
			t.Fatalf("%v: %v", cause, err)
		}
		if cause == nil && string(got.Bytes) != "raw" {
			t.Fatal("raw attribute lost")
		}
	}
	if err := m.SetFileCapabilities("/file", CapabilityAttribute{Present: true, Bytes: make([]byte, capabilityBytes+1)}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := scoped.FileCapabilities(t.Context(), "file"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatal(err)
	}
	if err := m.SetFileCapabilities("/file", CapabilityAttribute{Bytes: []byte("x")}, nil); !errors.Is(err, ErrMalformed) {
		t.Fatal(err)
	}
	if _, err := scoped.FileCapabilities(t.Context(), "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := scoped.FileCapabilities(t.Context(), "../outside"); !errors.Is(err, ErrSubpathEscape) {
		t.Fatal(err)
	}
	if err := scoped.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := scoped.FileCapabilities(t.Context(), "file"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

type contextCheckingReader struct {
	ScopedReader
	want context.Context
	t    *testing.T
}

func (r contextCheckingReader) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if ctx != r.want {
		r.t.Fatal("adapter replaced the caller context")
	}
	return r.ScopedReader.ReadFile(ctx, path)
}

func TestEnvironmentReadContext(t *testing.T) {
	t.Parallel()
	m := NewMemPlatformReader()
	for _, path := range []string{"/", "/proc", "/sys", "/sys/fs", "/sys/fs/cgroup"} {
		m.AddDir(path, 0o755)
	}
	m.AddFile("/proc/filesystems", []byte("nodev proc\n"), 0o600)
	m.AddFile("/sys/fs/cgroup/cgroup.controllers", []byte("cpu\n"), 0o600)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	procReader := contextCheckingReader{ScopedReader: NewScopedMemReader("/proc", m), want: ctx, t: t}
	sysReader := contextCheckingReader{ScopedReader: NewScopedMemReader("/sys", m), want: ctx, t: t}
	env := NewEnvironment(m, NewProcfsReader(procReader), NewSysfsReader(sysReader), nil)
	if got, err := env.Procfs().Filesystems(ctx); err != nil || len(got) != 1 {
		t.Fatalf("proc=%v %v", got, err)
	}
	if got, err := env.Sysfs().CgroupControllers(ctx); err != nil || len(got) != 1 {
		t.Fatalf("sys=%v %v", got, err)
	}
	cancel()
	if _, err := env.Procfs().Filesystems(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := env.Sysfs().CgroupControllers(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
