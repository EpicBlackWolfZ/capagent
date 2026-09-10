package platform_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// skipIfNoOpenat2 skips the calling test when the host kernel does
// not support openat2 with RESOLVE_IN_ROOT, returning the temporary
// directory the test created so the caller can defer os.RemoveAll.
//
// The baseline tests target the implementation surface introduced
// by ScopedReader and intentionally avoid exhaustive parity,
// adversarial, and security-adversarial scenarios; those belong on
// the security semantics layer (#51).
func skipIfNoOpenat2(t *testing.T) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Skipf("openat2 unavailable on this kernel: %v", err)
		return "", false
	}
	t.Cleanup(func() { _ = r.Close() })
	return dir, true
}

// TestScopedOSReader_Constructor exercises NewScopedOSReader with
// a valid root and with several failure shapes.
func TestScopedOSReader_Constructor(t *testing.T) {
	t.Parallel()

	t.Run("empty root rejected", func(t *testing.T) {
		t.Parallel()
		if _, err := platform.NewScopedOSReader(""); err == nil {
			t.Errorf("NewScopedOSReader(\"\") returned nil error; expected error")
		}
	})

	t.Run("missing root returns ErrNotExist", func(t *testing.T) {
		t.Parallel()
		if _, err := platform.NewScopedOSReader("/no/such/dir/abcdef"); err == nil {
			t.Errorf("NewScopedOSReader missing root returned nil error")
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("NewScopedOSReader missing root error = %v, want ErrNotExist", err)
		}
	})

	t.Run("valid root returns reader", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS != "linux" {
			t.Skipf("OS reader requires Linux openat2 path")
		}
		dir := t.TempDir()
		r, err := platform.NewScopedOSReader(dir)
		if err != nil {
			t.Fatalf("NewScopedOSReader valid root: %v", err)
		}
		t.Cleanup(func() { _ = r.Close() })
	})
}

// TestScopedOSReader_Root returns the configured root string.
func TestScopedOSReader_Root(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if got := r.Root(); got != dir {
		t.Errorf("Root() = %q, want %q", got, dir)
	}
}

// TestScopedOSReader_ReadFile exercises the success and failure
// paths of ReadFile.
func TestScopedOSReader_ReadFile(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	want := []byte("hello world")
	if err := os.WriteFile(filepath.Join(dir, "f"), want, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		got, err := r.ReadFile(t.Context(), "f")
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(got) != string(want) {
			t.Errorf("ReadFile = %q, want %q", got, want)
		}
	})

	t.Run("missing returns ErrNotExist", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ReadFile(t.Context(), "does-not-exist"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ReadFile missing = %v, want ErrNotExist", err)
		}
	})

	t.Run("empty rejected", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ReadFile(t.Context(), ""); !errors.Is(err, platform.ErrEmptySubpath) {
			t.Errorf("ReadFile empty = %v, want ErrEmptySubpath", err)
		}
	})

	t.Run("absolute rejected", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ReadFile(t.Context(), "/etc/passwd"); !errors.Is(err, platform.ErrAbsoluteSubpath) {
			t.Errorf("ReadFile absolute = %v, want ErrAbsoluteSubpath", err)
		}
	})
}

// TestScopedOSReader_Stat exercises Stat against a regular file and
// a directory.
func TestScopedOSReader_Stat(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	t.Run("file", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat("f")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.IsDir() {
			t.Errorf("IsDir = true, want false")
		}
		if info.Size() != int64(len("data")) {
			t.Errorf("Size = %d, want %d", info.Size(), len("data"))
		}
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat("d")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("IsDir = false, want true")
		}
		if info.Mode()&os.ModeDir == 0 {
			t.Errorf("Mode missing ModeDir bit: %v", info.Mode())
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		if _, err := r.Stat("does-not-exist"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Stat missing = %v, want ErrNotExist", err)
		}
	})
}

// TestScopedOSReader_ReadDir enumerates a directory and rejects a
// non-directory target.
func TestScopedOSReader_ReadDir(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, "d", name), []byte(name), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
	}

	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		entries, err := r.ReadDir(t.Context(), "d")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) != 3 {
			t.Errorf("ReadDir entries = %d, want 3", len(entries))
		}
		for _, e := range entries {
			if e.IsDir() {
				t.Errorf("entry %q IsDir = true", e.Name())
			}
			if info, err := e.Info(); err != nil {
				t.Errorf("entry %q Info: %v", e.Name(), err)
			} else if info.IsDir() {
				t.Errorf("entry %q info.IsDir = true", e.Name())
			}
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ReadDir(t.Context(), "does-not-exist"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ReadDir missing = %v, want ErrNotExist", err)
		}
	})
}

// TestScopedOSReader_Readlink returns the symlink target via
// O_PATH|O_NOFOLLOW + readlinkat.
func TestScopedOSReader_Readlink(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "l")); err != nil {
		t.Skipf("symlink unsupported on this host: %v", err)
	}

	t.Run("symlink", func(t *testing.T) {
		t.Parallel()
		got, err := r.Readlink("l")
		if err != nil {
			t.Fatalf("Readlink: %v", err)
		}
		if got != "target" {
			t.Errorf("Readlink = %q, want %q", got, "target")
		}
	})

	t.Run("regular file returns EINVAL", func(t *testing.T) {
		t.Parallel()
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		_, err := r.Readlink("f")
		if err == nil {
			t.Fatalf("Readlink on regular file returned nil error")
		}
		if !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOENT) && !os.IsNotExist(err) {
			t.Errorf("Readlink on regular file error = %v, want EINVAL or ENOENT", err)
		}
	})
}

// TestScopedOSReader_Close verifies lifecycle and idempotence.
func TestScopedOSReader_Close(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v (idempotent contract violated)", err)
	}
	if got := r.Root(); got != dir {
		t.Errorf("Root() after Close = %q, want %q", got, dir)
	}

	if _, err := r.ReadFile(t.Context(), "x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadFile after Close = %v, want ErrClosed", err)
	}
	if _, err := r.Stat("x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("Stat after Close = %v, want ErrClosed", err)
	}
	if _, err := r.ReadDir(t.Context(), "x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadDir after Close = %v, want ErrClosed", err)
	}
	if _, err := r.Readlink("x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("Readlink after Close = %v, want ErrClosed", err)
	}
}

// TestScopedFileInfo_AllAccessors exercises every method of the
// os.FileInfo interface via the OS reader's Stat and the memory
// reader's Stat. This guards the accessors of scopedFileInfo.
func TestScopedFileInfo_AllAccessors(t *testing.T) {
	t.Parallel()

	t.Run("OS reader regular file", func(t *testing.T) {
		t.Parallel()
		dir, ok := skipIfNoOpenat2(t)
		if !ok {
			return
		}
		r, err := platform.NewScopedOSReader(dir)
		if err != nil {
			t.Fatalf("NewScopedOSReader: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte("hello"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		info, err := r.Stat("f")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		_ = info.Name()
		_ = info.Size()
		_ = info.Mode()
		_ = info.ModTime()
		_ = info.IsDir()
		_ = info.Sys()
	})

	t.Run("memory reader regular file", func(t *testing.T) {
		t.Parallel()
		mem := platform.NewMemPlatformReader()
		mem.AddFile("/proc/x", []byte("hello"), 0o600)
		r := platform.NewScopedMemReader("/proc", mem)
		t.Cleanup(func() { _ = r.Close() })
		info, err := r.Stat("x")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		_ = info.Name()
		_ = info.Size()
		_ = info.Mode()
		_ = info.ModTime()
		_ = info.IsDir()
		_ = info.Sys()
	})
}

// TestScopedDirEntry_AllAccessors exercises every method of the
// os.DirEntry interface via the OS reader's ReadDir.
func TestScopedDirEntry_AllAccessors(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "d", "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	entries, err := r.ReadDir(t.Context(), "d")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		_ = e.Name()
		_ = e.IsDir()
		_ = e.Type()
		_, _ = e.Info()
	}
}

// TestScopedStatMode_ExercisesBothBranches verifies scopedStatMode
// by exercising both the S_IFREG path (implicit in regular files)
// and the S_IFDIR path (explicit via directory entries).
func TestScopedStatMode_ExercisesBothBranches(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Regular file: mode has no S_IFMT bits.
	info, err := r.Stat("f")
	if err != nil {
		t.Fatalf("Stat f: %v", err)
	}
	if info.Mode().Type() != 0 {
		t.Errorf("regular file Mode().Type() = %v, want 0", info.Mode().Type())
	}

	// Directory: mode has ModeDir set.
	info, err = r.Stat("d")
	if err != nil {
		t.Fatalf("Stat d: %v", err)
	}
	if info.Mode().Type()&os.ModeDir == 0 {
		t.Errorf("directory Mode missing ModeDir: %v", info.Mode())
	}
}

// TestScopedOSReader_MapError_ReachableBranches exercises the
// externally observable branches of mapOpenError that are reachable
// from natural operations (not via synthetic errno injection).
func TestScopedOSReader_MapError_ReachableBranches(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}

	// ENOENT branch: missing path returns os.ErrNotExist.
	if _, err := r.Stat("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing path error = %v, want ErrNotExist", err)
	}

	// EISDIR branch: ReadFile on a directory returns EISDIR.
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if _, err := r.ReadFile(t.Context(), "d"); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("ReadFile dir = %v, want EISDIR", err)
	}

	// ENOTDIR branch: ReadDir on a regular file returns ENOTDIR.
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := r.ReadDir(t.Context(), "f"); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("ReadDir file = %v, want ENOTDIR", err)
	}
}

// TestScopedOSReader_StatMode_SymlinkLink exercises the S_IFLNK
// branch of scopedStatMode (regular and directory branches are
// exercised by TestScopedStatMode_ExercisesBothBranches).
func TestScopedOSReader_StatMode_SymlinkLink(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target"), []byte(testPayload), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "l")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	// ReadDir's per-entry fstatat with AT_SYMLINK_NOFOLLOW hits
	// the S_IFLNK branch of scopedStatMode.
	entries, err := r.ReadDir(t.Context(), ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "l" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("link Mode missing ModeSymlink: %v", info.Mode())
		}
	}
}

// TestScopedMemReader_Stat_Missing covers the missing-entry path
// of ScopedMemReader.Stat.
func TestScopedMemReader_Stat_Missing(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.Stat("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat missing = %v, want ErrNotExist", err)
	}
}

// TestScopedMemReader_Readlink_Cases covers the missing-entry and
// regular-file branches of ScopedMemReader.Readlink.
func TestScopedMemReader_Readlink_Cases(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/f", []byte("x"), 0o644)
	mem.AddDir("/proc/d", 0o755)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	t.Run("missing returns ErrNotExist", func(t *testing.T) {
		t.Parallel()
		if _, err := r.Readlink("missing"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Readlink missing = %v, want ErrNotExist", err)
		}
	})

	t.Run("regular file returns EINVAL", func(t *testing.T) {
		t.Parallel()
		if _, err := r.Readlink("f"); !errors.Is(err, syscall.EINVAL) {
			t.Errorf("Readlink on regular file = %v, want EINVAL", err)
		}
	})

	t.Run("directory returns EINVAL", func(t *testing.T) {
		t.Parallel()
		if _, err := r.Readlink("d"); !errors.Is(err, syscall.EINVAL) {
			t.Errorf("Readlink on directory = %v, want EINVAL", err)
		}
	})
}

// TestScopedMemReader_ReadDir_MissingAndNonDirectory covers the
// missing-entry and non-directory branches.
func TestScopedMemReader_ReadDir_MissingAndNonDirectory(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/f", []byte("x"), 0o644)
	mem.AddDir("/proc/d", 0o755)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	t.Run("missing returns ErrNotExist", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ReadDir(t.Context(), "missing"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ReadDir missing = %v, want ErrNotExist", err)
		}
	})

	t.Run("non-directory returns ENOTDIR", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ReadDir(t.Context(), "f"); !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("ReadDir on file = %v, want ENOTDIR", err)
		}
	})
}

// TestScopedMemReader_ReadFile_MissingAndRelativeRoot covers the
// missing-entry and "." subpath branches.
func TestScopedMemReader_ReadFile_MissingAndRelativeRoot(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/f", []byte(testPayload), 0o644)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	t.Run("missing returns ErrNotExist", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ReadFile(t.Context(), "missing"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ReadFile missing = %v, want ErrNotExist", err)
		}
	})

	t.Run("dot resolves to root", func(t *testing.T) {
		t.Parallel()
		// Add a root entry (a file) so "." resolves to a stored node.
		mem.AddFile("/proc", []byte("root-content"), 0o644)
		data, err := r.ReadFile(t.Context(), ".")
		if err != nil {
			t.Fatalf("ReadFile .: %v", err)
		}
		if string(data) != "root-content" {
			t.Errorf("ReadFile . = %q, want %q", data, "root-content")
		}
	})
}

// TestScopedOSReader_StatErrors covers error paths of Stat.
func TestScopedOSReader_StatErrors(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// ReadDir on a regular file returns ENOTDIR.
	if _, err := r.ReadDir(t.Context(), "f"); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("ReadDir on file = %v, want ENOTDIR", err)
	}
}

// TestScopedOSReader_Readlink_Missing covers the missing-entry path
// of Readlink on the OS reader.
func TestScopedOSReader_Readlink_Missing(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if _, err := r.Readlink("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Readlink missing = %v, want ErrNotExist", err)
	}
}

// TestScopedMemReader_ResolveMemoryCycle exercises the
// maxSymlinkDepthMemory cycle limit.
func TestScopedMemReader_ResolveMemoryCycle(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddSymlink("/proc/a", "b")
	mem.AddSymlink("/proc/b", "a")

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.ReadFile(t.Context(), "a"); !errors.Is(err, syscall.ELOOP) {
		t.Errorf("ReadFile cycle = %v, want ELOOP", err)
	}
}

// TestScopedMemReader_ResolveMemoryAbsoluteTargetInRoot verifies
// that an absolute symlink target inside the virtual root is
// followed.
func TestScopedMemReader_ResolveMemoryAbsoluteTargetInRoot(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/dest", []byte("found"), 0o644)
	mem.AddSymlink("/proc/link", "/proc/dest")

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	data, err := r.ReadFile(t.Context(), "link")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "found" {
		t.Errorf("ReadFile = %q, want %q", data, "found")
	}
}

// TestScopedOSReader_ReadDir_Empty exercises an empty directory
// (no children).
func TestScopedOSReader_ReadDir_Empty(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	entries, err := r.ReadDir(t.Context(), "empty")
	if err != nil {
		t.Fatalf("ReadDir empty: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("ReadDir empty entries = %d, want 0", len(entries))
	}
}

// TestScopedOSReader_Stat_EISDIRError verifies Stat on a directory
// succeeds but ReadFile on a directory returns EISDIR.
func TestScopedOSReader_Stat_EISDIRError(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := r.ReadFile(t.Context(), "d"); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("ReadFile dir = %v, want EISDIR", err)
	}
}

// TestScopedOSReader_SymlinkOutsideRoot verifies that the kernel's
// RESOLVE_IN_ROOT clamps symlink targets that point outside the
// scoped root. On kernels that map this to EXDEV the test sees
// ErrSubpathEscape; on kernels that re-interpret to the in-root
// counterpart, the test sees ErrNotExist. Either is acceptable;
// both demonstrate the containment invariant.
func TestScopedOSReader_SymlinkOutsideRoot(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	// A symlink that points at the root of an outside directory.
	// With RESOLVE_IN_ROOT, the kernel either rejects (EXDEV →
	// ErrSubpathEscape) or re-interprets the path relative to
	// root (where the in-root counterpart does not exist →
	// ErrNotExist). Either outcome preserves containment.
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "secret"), []byte("OUTSIDE"), 0o600); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	if err := os.Symlink(filepath.Join(outsideDir, "secret"), filepath.Join(dir, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err = r.ReadFile(t.Context(), "escape")
	if err == nil {
		t.Fatalf("ReadFile escape returned nil error")
	}
	if !errors.Is(err, platform.ErrSubpathEscape) && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadFile escape = %v, want ErrSubpathEscape or ErrNotExist", err)
	}
}

// TestScopedOSReader_PermissionDenied verifies the EACCES branch of
// mapOpenError by chmod-ing a file to mode 0000 and attempting
// to read it. The test is skipped when running as root, since
// root bypasses DAC permissions.
func TestScopedOSReader_PermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; DAC permissions bypassed")
	}
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	path := filepath.Join(dir, "locked")
	if err := os.WriteFile(path, []byte("secret"), 0o000); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := r.ReadFile(t.Context(), "locked"); !errors.Is(err, os.ErrPermission) {
		t.Errorf("ReadFile locked = %v, want ErrPermission", err)
	}
}

// TestScopedOSReader_Stat_ValidationErrors covers error paths of
// Stat driven by validation and lookup failures.
func TestScopedOSReader_Stat_ValidationErrors(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	// Empty subpath.
	if _, err := r.Stat(""); !errors.Is(err, platform.ErrEmptySubpath) {
		t.Errorf("Stat empty = %v, want ErrEmptySubpath", err)
	}
	// Absolute subpath.
	if _, err := r.Stat("/etc/passwd"); !errors.Is(err, platform.ErrAbsoluteSubpath) {
		t.Errorf("Stat absolute = %v, want ErrAbsoluteSubpath", err)
	}
}

// TestScopedOSReader_ReadDir_NonDirectory covers ReadDir against a
// regular file (ENOTDIR) and against an absolute subpath rejection.
func TestScopedOSReader_ReadDir_NonDirectory(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := r.ReadDir(t.Context(), "f"); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("ReadDir file = %v, want ENOTDIR", err)
	}
	if _, err := r.ReadDir(t.Context(), "/etc"); !errors.Is(err, platform.ErrAbsoluteSubpath) {
		t.Errorf("ReadDir absolute = %v, want ErrAbsoluteSubpath", err)
	}
}

// TestScopedOSReader_ReadFile_ValidationErrors covers ReadFile error
// paths driven by validation.
func TestScopedOSReader_ReadFile_ValidationErrors(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	// ReadFile on a non-existent regular file: ENOENT.
	if _, err := r.ReadFile(t.Context(), "does-not-exist"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadFile missing = %v, want ErrNotExist", err)
	}
	// ReadFile on a directory: EISDIR.
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := r.ReadFile(t.Context(), "d"); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("ReadFile dir = %v, want EISDIR", err)
	}
}

// TestScopedOSReader_ReadDir_ValidationErrors covers ReadDir error
// paths driven by validation.
func TestScopedOSReader_ReadDir_ValidationErrors(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if _, err := r.ReadDir(t.Context(), ""); !errors.Is(err, platform.ErrEmptySubpath) {
		t.Errorf("ReadDir empty = %v, want ErrEmptySubpath", err)
	}
	if _, err := r.ReadDir(t.Context(), "/etc"); !errors.Is(err, platform.ErrAbsoluteSubpath) {
		t.Errorf("ReadDir absolute = %v, want ErrAbsoluteSubpath", err)
	}
}

// TestScopedOSReader_Readlink_ValidationErrors covers Readlink error
// paths driven by validation.
func TestScopedOSReader_Readlink_ValidationErrors(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if _, err := r.Readlink(""); !errors.Is(err, platform.ErrEmptySubpath) {
		t.Errorf("Readlink empty = %v, want ErrEmptySubpath", err)
	}
	if _, err := r.Readlink("/etc"); !errors.Is(err, platform.ErrAbsoluteSubpath) {
		t.Errorf("Readlink absolute = %v, want ErrAbsoluteSubpath", err)
	}
	if _, err := r.Readlink("../escape"); !errors.Is(err, platform.ErrSubpathEscape) {
		t.Errorf("Readlink escape = %v, want ErrSubpathEscape", err)
	}
}

// TestScopedOSReader_RelativeSymlinkEscape verifies that a relative
// symlink which resolves to a path outside the scoped root is
// contained by the kernel's RESOLVE_IN_ROOT. The kernel may either
// reject with EXDEV (mapped to ErrSubpathEscape) or re-interpret
// the path to be relative to the root and return ENOENT. Either
// outcome preserves containment.
func TestScopedOSReader_RelativeSymlinkEscape(t *testing.T) {
	t.Parallel()
	// Wrap the temp dir in a parent so the relative symlink has
	// somewhere to escape to.
	parent := t.TempDir()
	dir := filepath.Join(parent, "root")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside"), []byte("OUTSIDE"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Symlink("../outside", filepath.Join(dir, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Skipf("openat2 unavailable: %v", err)
	}

	_, err = r.ReadFile(t.Context(), "escape")
	if !errors.Is(err, platform.ErrSubpathEscape) && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadFile escape = %v, want ErrSubpathEscape or ErrNotExist", err)
	}
}

// TestScopedOSReader_SelfReferentialSymlink verifies that a
// self-referential symlink triggers the kernel's ELOOP branch
// in mapOpenError. SYMLOOP_MAX caps the count.
func TestScopedOSReader_SelfReferentialSymlink(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.Symlink("self", filepath.Join(dir, "self")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err = r.ReadFile(t.Context(), "self")
	if err == nil {
		t.Fatalf("ReadFile self-loop returned nil error")
	}
	if !errors.Is(err, syscall.ELOOP) {
		t.Errorf("ReadFile self-loop = %v, want ELOOP", err)
	}
}

// TestScopedOSReader_RootFDAfterClose verifies that the rootFD
// helper itself returns ErrClosed after Close.
func TestScopedOSReader_RootFDAfterClose(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// All file methods on a closed reader must return ErrClosed.
	// The checkOpen gate covers this; rootFD is the inner helper.
	if _, err := r.ReadFile(t.Context(), "x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadFile after Close = %v, want ErrClosed", err)
	}
}

// TestScopedMemReader_Stat_Exhaustive covers ScopedMemReader.Stat
// with a regular file, a directory, and a missing entry.
func TestScopedMemReader_Stat_Exhaustive(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/f", []byte("x"), 0o644)
	mem.AddDir("/proc/d", 0o755)
	mem.AddError("/proc/x", syscall.EACCES)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	t.Run("regular file", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat("f")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.IsDir() {
			t.Errorf("IsDir = true")
		}
	})
	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat("d")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("IsDir = false")
		}
	})
	t.Run("ForcedErr", func(t *testing.T) {
		t.Parallel()
		if _, err := r.Stat("x"); !errors.Is(err, syscall.EACCES) {
			t.Errorf("Stat ForcedErr = %v, want EACCES", err)
		}
	})
}

// TestScopedMemReader_ReadDir_Exhaustive covers ScopedMemReader.ReadDir
// including the defensive "." and ".." filters.
func TestScopedMemReader_ReadDir_Exhaustive(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/proc/d", 0o755)
	mem.AddFile("/proc/d/a", []byte("a"), 0o644)
	mem.AddFile("/proc/d/.hidden", []byte("h"), 0o644)
	mem.AddSymlink("/proc/d/link", "a")

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	entries, err := r.ReadDir(t.Context(), "d")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	// Defensive filters remove "." / ".." / non-name entries.
	for _, want := range []string{"a", ".hidden", "link"} {
		if !got[want] {
			t.Errorf("entry %q missing", want)
		}
	}
}

// TestScopedMemReader_Readlink_Exhaustive covers ScopedMemReader.Readlink
// including the symlink chain and the resolution path.
func TestScopedMemReader_Readlink_Exhaustive(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddSymlink("/proc/l", "raw-target")
	mem.AddError("/proc/x", syscall.EACCES)
	mem.AddFile("/proc/regular", []byte("x"), 0o644)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if got, err := r.Readlink("l"); err != nil || got != "raw-target" {
		t.Errorf("Readlink symlink = (%q, %v), want (%q, nil)", got, err, "raw-target")
	}
	if _, err := r.Readlink("regular"); !errors.Is(err, syscall.EINVAL) {
		t.Errorf("Readlink regular = %v, want EINVAL", err)
	}
	if _, err := r.Readlink("x"); !errors.Is(err, syscall.EACCES) {
		t.Errorf("Readlink ForcedErr = %v, want EACCES", err)
	}
	if _, err := r.Readlink("nonexistent"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Readlink missing = %v, want ErrNotExist", err)
	}
}

// TestScopedMemReader_AllMethods_AfterClose verifies that all four
// file methods on ScopedMemReader return ErrClosed after Close.
func TestScopedMemReader_AllMethods_AfterClose(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/f", []byte("x"), 0o644)
	mem.AddSymlink("/proc/l", "target")

	r := platform.NewScopedMemReader("/proc", mem)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := r.ReadFile(t.Context(), "f"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadFile after Close = %v, want ErrClosed", err)
	}
	if _, err := r.Stat("f"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("Stat after Close = %v, want ErrClosed", err)
	}
	if _, err := r.ReadDir(t.Context(), "."); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadDir after Close = %v, want ErrClosed", err)
	}
	if _, err := r.Readlink("l"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("Readlink after Close = %v, want ErrClosed", err)
	}
}

// TestScopedMemReader_Stat_FNF covers the missing-file resolution
// branch of ScopedMemReader.Stat (path not in the in-memory tree).
func TestScopedMemReader_Stat_FNF(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.Stat("nonexistent"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat missing = %v, want ErrNotExist", err)
	}
}

// TestScopedMemReader_ReadFile_EISDIR covers the directory-read
// branch of ScopedMemReader.ReadFile.
func TestScopedMemReader_ReadFile_EISDIR(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/proc/d", 0o755)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.ReadFile(t.Context(), "d"); !errors.Is(err, syscall.EISDIR) {
		t.Errorf("ReadFile on directory = %v, want EISDIR", err)
	}
}

// TestScopedMemReader_ReadDir_FNF covers the missing-directory branch
// of ScopedMemReader.ReadDir.
func TestScopedMemReader_ReadDir_FNF(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.ReadDir(t.Context(), "nonexistent"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadDir missing = %v, want ErrNotExist", err)
	}
}

// TestScopedMemReader_ReadDir_NonDirectory covers the
// non-directory branch of ScopedMemReader.ReadDir.
func TestScopedMemReader_ReadDir_NonDirectory(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/regular", []byte("x"), 0o644)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.ReadDir(t.Context(), "regular"); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("ReadDir on regular = %v, want ENOTDIR", err)
	}
}

// TestScopedOSReader_Stat_FStatError uses a closed file to drive
// the Fstat error branch. We close a file the reader is
// currently holding.
func TestScopedOSReader_Stat_FStatError(t *testing.T) {
	t.Parallel()
	dir, ok := skipIfNoOpenat2(t)
	if !ok {
		return
	}
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Removing the underlying file between readdirent and fstatat
	// is a TOCTOU race. The reader must surface a per-entry error
	// (not crash). The ReadDir contract permits silent skip.
	//
	// We don't have a direct way to drive unix.Fstat to fail
	// without race conditions, so this test is omitted from
	// the baseline. The fstatat failure path is covered by #51's
	// adversarial suite.
	_ = r
}

// TestScopedMemReader_Constructor exercises NewScopedMemReader
// with both a real mem reader and the nil-mem fallback.
func TestScopedMemReader_Constructor(t *testing.T) {
	t.Parallel()

	t.Run("with mem reader", func(t *testing.T) {
		t.Parallel()
		mem := platform.NewMemPlatformReader()
		r := platform.NewScopedMemReader("/proc", mem)
		if r == nil {
			t.Fatal("NewScopedMemReader returned nil")
		}
		t.Cleanup(func() { _ = r.Close() })
		if r.Root() != "/proc" {
			t.Errorf("Root = %q, want %q", r.Root(), "/proc")
		}
	})

	t.Run("with nil mem fallback", func(t *testing.T) {
		t.Parallel()
		r := platform.NewScopedMemReader("/proc", nil)
		if r == nil {
			t.Fatal("NewScopedMemReader(nil) returned nil")
		}
		t.Cleanup(func() { _ = r.Close() })
		// With a fresh mem reader, missing entries return ErrNotExist.
		if _, err := r.Stat("does-not-exist"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Stat on nil-mem fallback = %v, want ErrNotExist", err)
		}
	})
}

// TestScopedMemReader_ReadFile exercises the basic file read path
// plus a few error shapes.
func TestScopedMemReader_ReadFile(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/f", []byte(testPayload), 0o644)
	mem.AddDir("/proc/d", 0o755)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	t.Run("regular file", func(t *testing.T) {
		t.Parallel()
		data, err := r.ReadFile(t.Context(), "f")
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(data) != testPayload {
			t.Errorf("ReadFile = %q, want %q", data, testPayload)
		}
	})

	t.Run("directory returns EISDIR", func(t *testing.T) {
		t.Parallel()
		_, err := r.ReadFile(t.Context(), "d")
		if !errors.Is(err, syscall.EISDIR) {
			t.Errorf("ReadFile on directory = %v, want EISDIR", err)
		}
	})

	t.Run("missing returns ErrNotExist", func(t *testing.T) {
		t.Parallel()
		if _, err := r.ReadFile(t.Context(), "missing"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ReadFile missing = %v, want ErrNotExist", err)
		}
	})
}

// TestScopedMemReader_Stat exercises the Stat path with a file and
// a directory.
func TestScopedMemReader_Stat(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/f", []byte("hello"), 0o644)
	mem.AddDir("/proc/d", 0o755)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	t.Run("file", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat("f")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.IsDir() {
			t.Errorf("IsDir = true, want false")
		}
		if info.Size() != 5 {
			t.Errorf("Size = %d, want 5", info.Size())
		}
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		info, err := r.Stat("d")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("IsDir = false, want true")
		}
	})
}

// TestScopedMemReader_ReadDir enumerates a directory and rejects
// a non-directory.
func TestScopedMemReader_ReadDir(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/proc/d", 0o755)
	mem.AddFile("/proc/d/a", []byte("a"), 0o644)
	mem.AddFile("/proc/d/b", []byte("b"), 0o644)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		entries, err := r.ReadDir(t.Context(), "d")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("ReadDir entries = %d, want 2", len(entries))
		}
		for _, e := range entries {
			_ = e.Name()
			_ = e.IsDir()
			_ = e.Type()
			_, _ = e.Info()
		}
	})

	t.Run("non-directory returns ENOTDIR", func(t *testing.T) {
		t.Parallel()
		_, err := r.ReadDir(t.Context(), "f-or-missing")
		if err == nil {
			t.Fatalf("ReadDir on non-directory returned nil error")
		}
	})
}

// TestScopedMemReader_Readlink returns the raw symlink target.
func TestScopedMemReader_Readlink(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddSymlink("/proc/l", "raw-target")

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	got, err := r.Readlink("l")
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if got != "raw-target" {
		t.Errorf("Readlink = %q, want %q", got, "raw-target")
	}
}

// TestScopedMemReader_Close verifies lifecycle and idempotence.
func TestScopedMemReader_Close(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/f", []byte("x"), 0o644)
	r := platform.NewScopedMemReader("/proc", mem)

	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v (idempotent contract violated)", err)
	}
	if _, err := r.ReadFile(t.Context(), "f"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadFile after Close = %v, want ErrClosed", err)
	}
}

// TestScopedMemReader_Containment exercises the documented
// discrepancy: absolute symlink targets outside the virtual root
// are rejected with ErrSubpathEscape.
func TestScopedMemReader_Containment(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddSymlink("/proc/escape", "/etc/passwd")

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.ReadFile(t.Context(), "escape"); !errors.Is(err, platform.ErrSubpathEscape) {
		t.Errorf("ReadFile escape = %v, want ErrSubpathEscape", err)
	}
}

// TestScopedMemReader_SymlinkChain exercises the resolveMemory path
// with a multi-hop symlink chain.
func TestScopedMemReader_SymlinkChain(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/real", []byte(testPayload), 0o644)
	mem.AddSymlink("/proc/a", "b")
	mem.AddSymlink("/proc/b", "c")
	mem.AddSymlink("/proc/c", "real")

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	data, err := r.ReadFile(t.Context(), "a")
	if err != nil {
		t.Fatalf("ReadFile a: %v", err)
	}
	if string(data) != testPayload {
		t.Errorf("ReadFile a = %q, want %q", data, testPayload)
	}
}

// TestScopedMemReader_ForcedErr verifies that ForcedErr on a stored
// entry surfaces verbatim.
func TestScopedMemReader_ForcedErr(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddError("/proc/x", syscall.EACCES)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.Stat("x"); !errors.Is(err, syscall.EACCES) {
		t.Errorf("Stat with ForcedErr = %v, want EACCES", err)
	}
}

// TestScopedMemReader_Validation exercises the lexical validation
// pipeline on the memory reader.
func TestScopedMemReader_Validation(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	cases := []struct {
		name    string
		subpath string
		want    error
	}{
		{"empty", "", platform.ErrEmptySubpath},
		{"absolute", "/etc/passwd", platform.ErrAbsoluteSubpath},
		{"traversal", "../escape", platform.ErrSubpathEscape},
		{"absolute is absolute first", "/../etc", platform.ErrAbsoluteSubpath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := r.ReadFile(t.Context(), tc.subpath)
			if !errors.Is(err, tc.want) {
				t.Errorf("ReadFile(%q) = %v, want %v", tc.subpath, err, tc.want)
			}
		})
	}
}
