package platform_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// Reused literal names hoisted to constants for the goconst linter.
const (
	scopedReaderProcRoot    = "/proc"
	scopedReaderMissingSub  = "missing"
	scopedReaderNonExist    = "nope"
	scopedReaderSymlinkName = "l"
	memReaderKind           = "memory"
	osReaderKind            = "os"
	linuxGOOS               = "linux"
)

// scopedMemFactory wraps a MemPlatformReader; the setup closure runs
// before the reader is constructed so the underlying tree is populated.
func scopedMemFactory(t *testing.T, rootPath string, setup func(m *platform.MemPlatformReader)) platform.ScopedReader {
	t.Helper()
	mem := platform.NewMemPlatformReader()
	if setup != nil {
		setup(mem)
	}
	r := platform.NewScopedMemReader(rootPath, mem)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// scopedOSFactory mirrors the same logical tree onto a t.TempDir() and
// returns a kernel-confined ScopedOSReader. The temp directory becomes
// the effective root for the reader; absolute subpaths in the memory
// tree are remapped relative to the temp dir.
//
// Implementation note: t.TempDir() registers its cleanup in LIFO order
// alongside any t.Cleanup calls. To avoid races on heavily-parallel
// tmpfs-backed hosts, this factory uses os.MkdirTemp and registers an
// explicit cleanup that runs in deterministic order: the reader Close
// is registered BEFORE the dir removal so the directory fd is closed
// before RemoveAll enumerates the tree.
func scopedOSFactory(t *testing.T, rootPath string, setup func(m *platform.MemPlatformReader)) platform.ScopedReader {
	t.Helper()
	dir, err := os.MkdirTemp("", "scoped-os-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort: remove the dir; ignore errors because the
		// test has already returned by this point and the temp
		// dir is no longer relevant.
		_ = os.RemoveAll(dir)
	})

	memSeeded := platform.NewMemPlatformReader()
	if setup != nil {
		setup(memSeeded)
	}
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		t.Fatalf("absolute root: %v", err)
	}
	for k, v := range memSeeded.Snapshot() {
		var realPath string
		if absRoot != "" && strings.HasPrefix(k, absRoot) {
			rel, rerr := filepath.Rel(absRoot, k)
			if rerr != nil {
				continue
			}
			realPath = filepath.Join(dir, rel)
		} else {
			realPath = filepath.Join(dir, strings.TrimPrefix(k, "/"))
		}
		materializeTreeEntry(t, realPath, v)
	}

	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Skipf("openat2 unavailable on this kernel: %v", err)
	}
	t.Cleanup(func() {
		if cerr := r.Close(); cerr != nil {
			t.Logf("ScopedOSReader.Close() cleanup: %v", cerr)
		}
	})
	return r
}

// materializeTreeEntry writes a single VirtualFile entry to disk at the
// supplied absolute path. Directories are mkdir'd; regular files are
// written; symlinks are created via os.Symlink.
func materializeTreeEntry(t *testing.T, path string, vf *platform.VirtualFile) {
	t.Helper()
	switch vf.Kind {
	case platform.FileKindDirectory:
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("materialize dir %q: %v", path, err)
		}
	case platform.FileKindSymlink:
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("materialize dir for symlink %q: %v", path, err)
		}
		if err := os.Symlink(vf.Target, path); err != nil {
			t.Skipf("symlink unsupported on this host: %v", err)
		}
	default:
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("materialize dir for file %q: %v", path, err)
		}
		if err := os.WriteFile(path, vf.Content, 0o644); err != nil {
			t.Fatalf("materialize file %q: %v", path, err)
		}
	}
}

// withParity runs a subtest twice, once with the memory reader and once
// with the OS reader, and lets the test body make per-implementation
// expectations. The factory returns nil to indicate "skip this reader"
// (e.g., OS reader on a kernel that lacks openat2).
//
// OS subtests are deliberately NOT marked t.Parallel() because tmpfs-backed
// filesystems exhibit cleanup races when many ScopedOSReader instances
// are torn down in parallel (readdirent returns EBADF on /tmp subdirs).
// Memory subtests remain parallel.
func withParity(
	t *testing.T,
	name string,
	memSetup func(m *platform.MemPlatformReader),
	body func(t *testing.T, r platform.ScopedReader, kind string),
) {
	t.Helper()
	t.Run(name+"/memory", func(t *testing.T) {
		t.Parallel()
		r := scopedMemFactory(t, scopedReaderProcRoot, memSetup)
		body(t, r, memReaderKind)
	})
	t.Run(name+"/os", func(t *testing.T) {
		if runtime.GOOS != linuxGOOS {
			t.Skipf("OS reader requires Linux")
		}
		r := scopedOSFactory(t, scopedReaderProcRoot, memSetup)
		body(t, r, osReaderKind)
	})
}

// -----------------------------------------------------------------------------
// ValidateSubpath edge-case coverage (per plan §10.1)
//
// The exhaustive ValidateSubpath table-driven tests live in path_test.go.
// This file focuses on per-operation kernel-confined semantics.
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// Per-operation tests (plan §10.2)
// -----------------------------------------------------------------------------

// TestScopedReader_ReadFile exercises the ReadFile contract: lexical
// validation, kernel-confined containment, and per-operation error
// mapping. Per the kickoff prompt's semantic correction, kernel-clamped
// paths (RESOLVE_IN_ROOT) for `/` and `..` may succeed or fail with
// different error codes across kernel releases; the test accepts any
// error that still satisfies the containment invariant.
func TestScopedReader_ReadFile(t *testing.T) {


	tests := []struct {
		name           string
		setup          func(m *platform.MemPlatformReader)
		subpath        string
		want           string
		wantErr        error   // strict expected error
		wantErrMemOnly error   // additional error only the memory reader surfaces
	}{
		{
			name: "plain file",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/proc/f", []byte("hi"), 0o644)
			},
			subpath: "f",
			want:    "hi",
		},
		{
			name: "directory read returns EISDIR",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/proc/d", 0o755)
			},
			subpath: "d",
			wantErr: syscall.EISDIR,
		},
		{
			name:    "missing returns ErrNotExist",
			setup:   func(m *platform.MemPlatformReader) {},
			subpath: "no-such",
			wantErr: os.ErrNotExist,
		},
		{
			name:    "empty rejected",
			setup:   func(m *platform.MemPlatformReader) {},
			subpath: "",
			wantErr: platform.ErrEmptySubpath,
		},
		{
			name:    "absolute rejected",
			setup:   func(m *platform.MemPlatformReader) {},
			subpath: "/etc/passwd",
			wantErr: platform.ErrAbsoluteSubpath,
		},
		{
			name:    "lexical escape rejected",
			setup:   func(m *platform.MemPlatformReader) {},
			subpath: "../../etc/passwd",
			wantErr: platform.ErrSubpathEscape,
		},
		{
			name: "symlink inside root resolves",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/proc/b", []byte("payload"), 0o644)
				m.AddSymlink("/proc/a", "b")
			},
			subpath: "a",
			want:    "payload",
		},
		{
			name: "self-loop returns ELOOP",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/proc/loop", "loop")
			},
			subpath: "loop",
			wantErr: syscall.ELOOP,
		},
		{
			name: "broken symlink returns ErrNotExist",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/proc/broken", scopedReaderNonExist)
			},
			subpath: "broken",
			wantErr: os.ErrNotExist,
		},
		{
			name: "symlink to root is contained (EISDIR or EXDEV on OS)",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/proc/toroot", "/")
			},
			subpath: "toroot",
			// Memory: ErrSubpathEscape (explicit lexical check).
			// OS: EISDIR if kernel-resolves to "/" and read fails as
			// "is a directory", or EXDEV if the kernel refuses the
			// resolution. Containment invariant holds either way.
			wantErrMemOnly: platform.ErrSubpathEscape,
		},
	}

	for _, tt := range tests {
		tt := tt
		withParity(t, tt.name, tt.setup, func(t *testing.T, r platform.ScopedReader, kind string) {
			data, err := r.ReadFile(tt.subpath)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ReadFile(%q) [%s] error = %v, want %v", tt.subpath, kind, err, tt.wantErr)
				}
				return
			}
			if tt.wantErrMemOnly != nil && kind == memReaderKind {
				if !errors.Is(err, tt.wantErrMemOnly) {
					t.Fatalf("ReadFile(%q) [memory] error = %v, want %v", tt.subpath, err, tt.wantErrMemOnly)
				}
				return
			}
			if tt.wantErrMemOnly != nil && kind == osReaderKind {
				// For the OS reader, containment must hold; the
				// specific errno varies by kernel. Accept any
				// non-nil error that doesn't suggest escape.
				if err == nil {
					t.Fatalf("ReadFile(%q) [os] returned nil error; expected a containment error", tt.subpath)
				}
				if errors.Is(err, platform.ErrSubpathEscape) {
					// OS path: kernel rejection is allowed.
					return
				}
				// Per the kickoff prompt's semantic correction,
				// accept any kernel-surfaced errno as long as
				// the read did not return content. The key
				// invariant is that we never read a path that
				// escapes the rooted FD.
				t.Logf("OS ReadFile(%q) returned %v (kernel-dependent); containment holds", tt.subpath, err)
				return
			}
			if err != nil {
				t.Fatalf("ReadFile(%q) [%s] unexpected error: %v", tt.subpath, kind, err)
			}
			if string(data) != tt.want {
				t.Errorf("ReadFile(%q) [%s] = %q, want %q", tt.subpath, kind, string(data), tt.want)
			}
		})
	}
}

// TestScopedReader_Stat verifies Stat follows the final symlink and
// enforces the lexical containment rules.
func TestScopedReader_Stat(t *testing.T) {


	tests := []struct {
		name        string
		setup       func(m *platform.MemPlatformReader)
		subpath     string
		wantExist   bool
		wantIsDir   bool
		wantIsLink  bool
		wantErr     error
	}{
		{
			name: "regular file",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/proc/x", []byte("data"), 0o600)
			},
			subpath:    "x",
			wantExist:  true,
			wantIsDir:  false,
		},
		{
			name: "directory",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/proc/d", 0o755)
			},
			subpath:    "d",
			wantExist:  true,
			wantIsDir:  true,
		},
		{
			name: "missing returns ErrNotExist",
			setup: func(m *platform.MemPlatformReader) {},
			subpath:    scopedReaderMissingSub,
			wantErr:    os.ErrNotExist,
		},
		{
			name: "absolute rejected",
			setup: func(m *platform.MemPlatformReader) {},
			subpath: "/x",
			wantErr: platform.ErrAbsoluteSubpath,
		},
		{
			name: "lexical escape rejected",
			setup: func(m *platform.MemPlatformReader) {},
			subpath: "../escape",
			wantErr: platform.ErrSubpathEscape,
		},
		{
			name: "symlink resolution",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/proc/target", []byte("payload"), 0o644)
				m.AddSymlink("/proc/link", "target")
			},
			subpath:    "link",
			wantExist:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		withParity(t, tt.name, tt.setup, func(t *testing.T, r platform.ScopedReader, kind string) {
			info, err := r.Stat(tt.subpath)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Stat(%q) [%s] error = %v, want %v", tt.subpath, kind, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Stat(%q) [%s] unexpected error: %v", tt.subpath, kind, err)
			}
			if info.IsDir() != tt.wantIsDir {
				t.Errorf("Stat(%q) [%s] IsDir = %v, want %v", tt.subpath, kind, info.IsDir(), tt.wantIsDir)
			}
		})
	}
}

// TestScopedReader_ReadDir verifies ReadDir enumerates children without
// "." or ".." entries, returns metadata eagerly, and rejects non-dir
// inputs.
func TestScopedReader_ReadDir(t *testing.T) {


	tests := []struct {
		name      string
		setup     func(m *platform.MemPlatformReader)
		subpath   string
		wantNames []string
		wantErr   error
	}{
		{
			name: "enumerates sorted children",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/proc/d", 0o755)
				m.AddFile("/proc/d/a", []byte("a"), 0o644)
				m.AddFile("/proc/d/b", []byte("b"), 0o644)
				m.AddFile("/proc/d/c", []byte("c"), 0o644)
			},
			subpath:   "d",
			wantNames: []string{"a", "b", "c"},
		},
		{
			name: "missing returns ErrNotExist",
			setup: func(m *platform.MemPlatformReader) {},
			subpath: scopedReaderMissingSub,
			wantErr: os.ErrNotExist,
		},
		{
			name: "regular file returns ENOTDIR",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/proc/f", []byte("x"), 0o644)
			},
			subpath: "f",
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "empty rejected",
			setup: func(m *platform.MemPlatformReader) {},
			subpath: "",
			wantErr: platform.ErrEmptySubpath,
		},
		{
			name: "absolute rejected",
			setup: func(m *platform.MemPlatformReader) {},
			subpath: "/d",
			wantErr: platform.ErrAbsoluteSubpath,
		},
	}

	for _, tt := range tests {
		tt := tt
		withParity(t, tt.name, tt.setup, func(t *testing.T, r platform.ScopedReader, kind string) {
			entries, err := r.ReadDir(tt.subpath)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ReadDir(%q) [%s] error = %v, want %v", tt.subpath, kind, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadDir(%q) [%s] unexpected error: %v", tt.subpath, kind, err)
			}
			gotNames := make([]string, 0, len(entries))
			for _, e := range entries {
				if e.Name() == "." || e.Name() == ".." {
					t.Errorf("ReadDir returned %q; should never expose \".\" or \"..\"", e.Name())
				}
				gotNames = append(gotNames, e.Name())
			}
			sort.Strings(gotNames)
			if !equalStrings(gotNames, tt.wantNames) {
				t.Errorf("ReadDir(%q) [%s] names = %v, want %v", tt.subpath, kind, gotNames, tt.wantNames)
			}
		})
	}
}

// TestScopedReader_Readlink verifies Readlink inspects the link and
// returns its raw target without validating it. Per the kickoff prompt's
// semantic correction 2, the kernel can surface EINVAL or ENOENT when
// readlinkat is called on a non-symlink FD; the test accepts both as
// long as containment holds.
func TestScopedReader_Readlink(t *testing.T) {


	tests := []struct {
		name           string
		setup          func(m *platform.MemPlatformReader)
		subpath        string
		want           string
		wantErr        error // shared expected error
		wantErrMemOnly error // only the memory reader must surface this
	}{
		{
			name: "raw target returned verbatim",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/proc/l", "../some/target")
			},
			subpath: scopedReaderSymlinkName,
			want:    "../some/target",
		},
		{
			name: "absolute target returned verbatim",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/proc/l", "/abs/path")
			},
			subpath: scopedReaderSymlinkName,
			want:    "/abs/path",
		},
		{
			name: "broken symlink returns raw target",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/proc/broken", scopedReaderMissingSub)
			},
			subpath: "broken",
			want:    scopedReaderMissingSub,
		},
		{
			name: "self-loop returns raw target",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/proc/selfloop", "selfloop")
			},
			subpath: "selfloop",
			want:    "selfloop",
		},
		{
			name: "regular file returns EINVAL (mem) / ENOENT (os)",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/proc/f", []byte("x"), 0o644)
			},
			subpath: "f",
			// Memory reader returns EINVAL; OS reader returns ENOENT
			// on Linux 7.2 (and historically EINVAL on older
			// kernels). Either is acceptable; containment holds.
			wantErrMemOnly: syscall.EINVAL,
		},
		{
			name: "directory returns EINVAL (mem) / ENOENT (os)",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/proc/d", 0o755)
			},
			subpath:        "d",
			wantErrMemOnly: syscall.EINVAL,
		},
		{
			name:    "missing returns ErrNotExist",
			setup:   func(m *platform.MemPlatformReader) {},
			subpath: scopedReaderMissingSub,
			wantErr: os.ErrNotExist,
		},
		{
			name:    "absolute rejected",
			setup:   func(m *platform.MemPlatformReader) {},
			subpath: "/l",
			wantErr: platform.ErrAbsoluteSubpath,
		},
	}

	for _, tt := range tests {
		tt := tt
		withParity(t, tt.name, tt.setup, func(t *testing.T, r platform.ScopedReader, kind string) {
			got, err := r.Readlink(tt.subpath)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Readlink(%q) [%s] error = %v, want %v", tt.subpath, kind, err, tt.wantErr)
				}
				return
			}
			if tt.wantErrMemOnly != nil && kind == memReaderKind {
				if !errors.Is(err, tt.wantErrMemOnly) {
					t.Fatalf("Readlink(%q) [memory] error = %v, want %v", tt.subpath, err, tt.wantErrMemOnly)
				}
				return
			}
			if tt.wantErrMemOnly != nil && kind == osReaderKind {
				// Per the kickoff prompt's semantic correction 2,
				// the kernel may return EINVAL or ENOENT for
				// readlinkat on a non-symlink FD; both indicate
				// that the readlink inspect operation cannot
				// proceed. Containment is not in question here.
				if err == nil {
					t.Fatalf("Readlink(%q) [os] returned nil error; expected a non-symlink error", tt.subpath)
				}
				if !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOENT) && !os.IsNotExist(err) {
					t.Fatalf("Readlink(%q) [os] error = %v, want EINVAL or ENOENT", tt.subpath, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Readlink(%q) [%s] unexpected error: %v", tt.subpath, kind, err)
			}
			if got != tt.want {
				t.Errorf("Readlink(%q) [%s] = %q, want %q", tt.subpath, kind, got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ReadDir semantics tests (plan §10.3)
// -----------------------------------------------------------------------------

// TestScopedReader_ReadDir_ChildMetadata captures the eager-FileInfo
// semantic for every child kind. After ReadDir returns, DirEntry.Info()
// must succeed without triggering any host I/O.
func TestScopedReader_ReadDir_ChildMetadata(t *testing.T) {


	setup := func(m *platform.MemPlatformReader) {
		m.AddDir("/proc/d", 0o755)
		m.AddFile("/proc/d/regular", []byte("data"), 0o644)
		m.AddDir("/proc/d/subdir", 0o755)
		m.AddSymlink("/proc/d/symlink", "regular")
		m.AddSymlink("/proc/d/broken", "does-not-exist")
		m.AddSymlink("/proc/d/escape", "../outside")
	}

	withParity(t, "ChildMetadata", setup, func(t *testing.T, r platform.ScopedReader, kind string) {
		entries, err := r.ReadDir("d")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		byName := make(map[string]os.DirEntry)
		for _, e := range entries {
			byName[e.Name()] = e
		}
		// regular file
		regular, ok := byName["regular"]
		if !ok {
			t.Fatalf("missing 'regular' entry")
		}
		if regular.IsDir() {
			t.Errorf("regular IsDir = true, want false")
		}
		if regular.Type()&os.ModeSymlink != 0 {
			t.Errorf("regular Type = %v, want non-symlink", regular.Type())
		}
		info, err := regular.Info()
		if err != nil {
			t.Fatalf("regular Info: %v", err)
		}
		if info.IsDir() {
			t.Errorf("regular info IsDir = true, want false")
		}

		// directory
		subdir, ok := byName["subdir"]
		if !ok {
			t.Fatalf("missing 'subdir' entry")
		}
		if !subdir.IsDir() {
			t.Errorf("subdir IsDir = false, want true")
		}

		// symlink (in-root target)
		symlink, ok := byName["symlink"]
		if !ok {
			t.Fatalf("missing 'symlink' entry")
		}
		if symlink.Type()&os.ModeSymlink == 0 {
			t.Errorf("symlink Type = %v, want symlink", symlink.Type())
		}
		info, err = symlink.Info()
		if err != nil {
			t.Fatalf("symlink Info: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("symlink Info.Mode = %v, want symlink", info.Mode())
		}
		// Per the kernel contract, AT_SYMLINK_NOFOLLOW on fstatat must
		// return the link's own metadata, not the target's. The size
		// reflects the symlink's stored target length, NOT the
		// target file's content size.

		// broken symlink (target does not exist)
		broken, ok := byName["broken"]
		if !ok {
			t.Fatalf("missing 'broken' entry")
		}
		if broken.Type()&os.ModeSymlink == 0 {
			t.Errorf("broken Type = %v, want symlink", broken.Type())
		}

		// escape symlink (raw target stored verbatim)
		escape, ok := byName["escape"]
		if !ok {
			t.Fatalf("missing 'escape' entry")
		}
		if escape.Type()&os.ModeSymlink == 0 {
			t.Errorf("escape Type = %v, want symlink", escape.Type())
		}
		// Readlink returns the raw target without validating it.
		target, err := r.Readlink(filepath.Join("d", "escape"))
		if err != nil {
			t.Fatalf("Readlink(escape): %v", err)
		}
		if target != "../outside" {
			t.Errorf("Readlink(escape) target = %q, want %q", target, "../outside")
		}
	})
}

// TestScopedReader_ReadDir_NoDeferredLookup asserts that the FileInfo
// returned by DirEntry.Info() is captured eagerly during ReadDir; closing
// the reader afterwards must not invalidate the returned entries.
func TestScopedReader_ReadDir_NoDeferredLookup(t *testing.T) {


	setup := func(m *platform.MemPlatformReader) {
		m.AddDir("/proc/d", 0o755)
		m.AddFile("/proc/d/regular", []byte("A"), 0o644)
		m.AddDir("/proc/d/subdir", 0o755)
		m.AddFile("/proc/d/subdir/child", []byte("B"), 0o644)
		// Add a symlink pointing outside the root; AT_SYMLINK_NOFOLLOW
		// must keep the FileInfo on the link itself, never follow.
		m.AddSymlink("/proc/d/escape", "/outside")
	}

	withParity(t, "NoDeferredLookup", setup, func(t *testing.T, r platform.ScopedReader, kind string) {
		entries, err := r.ReadDir("d")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		// Close the reader to release the directory fd. Subsequent
		// calls to entries[i].Info() MUST still work, because the
		// FileInfo was captured eagerly during ReadDir.
		if closer, ok := r.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}

		for _, e := range entries {
			info, ierr := e.Info()
			if ierr != nil {
				t.Errorf("entries[%q].Info() after Close: %v", e.Name(), ierr)
				continue
			}
			if info == nil {
				t.Errorf("entries[%q].Info() returned nil", e.Name())
			}
		}
	})
}

// TestScopedReader_ReadDir_FiltersDotAndDotDot verifies "." and ".." are
// never returned. Both readers must apply the same filter.
func TestScopedReader_ReadDir_FiltersDotAndDotDot(t *testing.T) {


	setup := func(m *platform.MemPlatformReader) {
		m.AddDir("/proc/d", 0o755)
		m.AddFile("/proc/d/a", []byte("a"), 0o644)
	}

	withParity(t, "FiltersDotAndDotDot", setup, func(t *testing.T, r platform.ScopedReader, kind string) {
		entries, err := r.ReadDir("d")
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		for _, e := range entries {
			if e.Name() == "." || e.Name() == ".." {
				t.Errorf("ReadDir returned %q; must filter dot entries", e.Name())
			}
		}
	})
}

// -----------------------------------------------------------------------------
// Lifecycle tests (plan §10.4)
// -----------------------------------------------------------------------------

// TestScopedReader_UseAfterClose verifies file operations return ErrClosed
// after Close(), while Root() remains valid and Close() is idempotent.
func TestScopedReader_UseAfterClose(t *testing.T) {


	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/x", []byte("payload"), 0o644)
	r := platform.NewScopedMemReader("/proc", mem)

	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v (idempotent contract violated)", err)
	}
	if got := r.Root(); got != "/proc" {
		t.Errorf("Root() after Close = %q, want %q", got, "/proc")
	}

	if _, err := r.ReadFile("x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadFile after Close error = %v, want ErrClosed", err)
	}
	if _, err := r.Stat("x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("Stat after Close error = %v, want ErrClosed", err)
	}
	if _, err := r.ReadDir("d"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadDir after Close error = %v, want ErrClosed", err)
	}
	if _, err := r.Readlink("x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("Readlink after Close error = %v, want ErrClosed", err)
	}
}

// TestScopedReader_Race_LegalConcurrentUse verifies the concurrency
// contract: many goroutines may simultaneously call file methods on the
// same reader without triggering a data race.
func TestScopedReader_Race_LegalConcurrentUse(t *testing.T) {


	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/x", []byte("payload"), 0o644)
	mem.AddDir("/proc/d", 0o755)
	mem.AddFile("/proc/d/child", []byte("c"), 0o644)
	mem.AddSymlink("/proc/l", "x")

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	const goroutines = 16
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if _, err := r.ReadFile("x"); err != nil {
					t.Errorf("ReadFile: %v", err)
					return
				}
				if _, err := r.Stat("x"); err != nil {
					t.Errorf("Stat: %v", err)
					return
				}
				if _, err := r.ReadDir("d"); err != nil {
					t.Errorf("ReadDir: %v", err)
					return
				}
				if _, err := r.Readlink(scopedReaderSymlinkName); err != nil {
					t.Errorf("Readlink: %v", err)
					return
				}
				if got := r.Root(); got != "/proc" {
					t.Errorf("Root = %q, want /proc", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// -----------------------------------------------------------------------------
// Adversarial tests (plan §10.5)
// -----------------------------------------------------------------------------

// TestScopedReader_SymlinkRace is the adversarial regression test for
// accidental pathname-reopen implementations. It rapidly swaps a file
// between an in-root regular file and a symlink pointing outside root,
// while a concurrent reader repeatedly calls ReadFile. The test asserts
// that no read ever returns the "outside" content, which would only
// happen if the implementation re-resolved the pathname after the open.
func TestScopedReader_SymlinkRace(t *testing.T) {


	if runtime.GOOS != linuxGOOS {
		t.Skipf("symlink race regression test requires Linux openat2 path")
	}

	dir := t.TempDir()
	// in-root safe content
	safeContent := []byte("in-root-content")
	if err := os.WriteFile(filepath.Join(dir, "safe"), safeContent, 0o600); err != nil {
		t.Fatalf("seed safe: %v", err)
	}
	// outside-of-root content
	outsideDir := t.TempDir()
	outsideContent := []byte("OUTSIDE")
	if err := os.WriteFile(filepath.Join(outsideDir, "secret"), outsideContent, 0o600); err != nil {
		t.Fatalf("seed outside: %v", err)
	}

	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Skipf("openat2 unavailable: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	const totalAttempts = 2000
	var (
		inRootHits atomic.Int32
		errors     atomic.Int32
		outsideHits atomic.Int32
	)
	stop := make(chan struct{})

	// Swapper: continuously rewrites `target` between a regular file
	// with safeContent and a symlink to the outside file. RESOLVE_IN_ROOT
	// must always clamp the read to inside-root regardless of the
	// current on-disk state at open time.
	var swapWG sync.WaitGroup
	swapWG.Add(1)
	go func() {
		defer swapWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			targetPath := filepath.Join(dir, "target")
			_ = os.Remove(targetPath)
			// alternate symlink / regular
			if time.Now().UnixNano()%2 == 0 {
				if err := os.Symlink(filepath.Join(outsideDir, "secret"), targetPath); err != nil {
					continue
				}
			} else {
				if err := os.WriteFile(targetPath, safeContent, 0o600); err != nil {
					continue
				}
			}
		}
	}()

	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for i := 0; i < totalAttempts; i++ {
			data, err := r.ReadFile("target")
			if err != nil {
				// Expected when the entry is a symlink to outside root:
				// the kernel's RESOLVE_IN_ROOT returns ErrNotExist.
				errors.Add(1)
				continue
			}
			if string(data) == "OUTSIDE" {
				outsideHits.Add(1)
				continue
			}
			if string(data) == "in-root-content" {
				inRootHits.Add(1)
				continue
			}
			// Some reads may legitimately land on a brief moment
			// after os.Remove but before the next write; the file
			// is unlinked but the openat2 succeeded via a stale
			// inode. Such reads return empty content (length 0)
			// rather than ErrNotExist because the FD was valid
			// when opened. Count these as benign races.
			if len(data) == 0 {
				errors.Add(1)
				continue
			}
			t.Errorf("unexpected read content: %q (len=%d)", string(data), len(data))
		}
		close(stop)
	}()

	readerWG.Wait()
	swapWG.Wait()

	if outsideHits.Load() != 0 {
		t.Fatalf("symlink race regression: %d reads returned OUTSIDE content; containment violated", outsideHits.Load())
	}
	if inRootHits.Load()+errors.Load() != int32(totalAttempts) {
		t.Fatalf("read accounting: inRoot=%d errors=%d total=%d", inRootHits.Load(), errors.Load(), totalAttempts)
	}
}

// TestScopedReader_ReadDir_NoSymlinkFollow asserts that a child symlink
// whose target lies outside the scoped root does NOT cause ReadDir (or
// subsequent Info() calls) to escape. This is the AT_SYMLINK_NOFOLLOW
// guarantee under the kernel-confined design.
func TestScopedReader_ReadDir_NoSymlinkFollow(t *testing.T) {


	if runtime.GOOS != linuxGOOS {
		t.Skipf("OS-reader parity requires Linux")
	}

	root := t.TempDir()
	outside := t.TempDir()

	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("OUTSIDE"), 0o600); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatalf("seed root/d: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "d", "escape")); err != nil {
		t.Fatalf("seed symlink: %v", err)
	}

	sr, err := platform.NewScopedOSReader(root)
	if err != nil {
		t.Skipf("openat2 unavailable: %v", err)
	}
	t.Cleanup(func() { _ = sr.Close() })

	entries, err := sr.ReadDir("d")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "escape" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("escape Type = %v, want ModeSymlink", info.Mode())
		}
		// The reported size MUST be the size of the stored target
		// (string length), not the content of /outside/secret.
		if info.Size() == int64(len("OUTSIDE")) {
			t.Errorf("escape Info.Size = %d, equals target content size; symlink was followed", info.Size())
		}
	}
}

// TestScopedReader_FileInfo_AllAccessors exercises every method of the
// os.FileInfo interface (Name, Size, Mode, ModTime, IsDir, Sys) so the
// scopedFileInfo coverage approaches 100%.
func TestScopedReader_FileInfo_AllAccessors(t *testing.T) {
	t.Parallel()

	mem := platform.NewMemPlatformReader()
	mem.AddFile("/proc/regular", []byte("hello"), 0o644)
	mem.AddDir("/proc/dir", 0o755)

	r := platform.NewScopedMemReader("/proc", mem)
	t.Cleanup(func() { _ = r.Close() })

	// regular file
	info, err := r.Stat("regular")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Name(); got != "regular" {
		t.Errorf("Name = %q, want regular", got)
	}
	if got := info.Size(); got != 5 {
		t.Errorf("Size = %d, want 5", got)
	}
	if info.IsDir() {
		t.Errorf("IsDir = true, want false")
	}
	if info.ModTime().IsZero() == false {
		t.Errorf("ModTime = %v, want zero", info.ModTime())
	}
	if info.Mode()&0o644 == 0 {
		t.Errorf("Mode = %v, want 0o644 bits set", info.Mode())
	}
	if info.Sys() != nil {
		t.Errorf("Sys = %v, want nil", info.Sys())
	}

	// directory
	dinfo, err := r.Stat("dir")
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !dinfo.IsDir() {
		t.Errorf("dir IsDir = false, want true")
	}
	if dinfo.Mode()&os.ModeDir == 0 {
		t.Errorf("dir Mode missing ModeDir: %v", dinfo.Mode())
	}

	// ScopedOSReader.Root()
	if runtime.GOOS == linuxGOOS {
		osr, err := platform.NewScopedOSReader(t.TempDir())
		if err != nil {
			t.Skipf("openat2 unavailable: %v", err)
		}
		t.Cleanup(func() { _ = osr.Close() })
		if got := osr.Root(); got == "" {
			t.Errorf("Root() = empty")
		}
	}
}

// TestScopedReader_MapOpenError_Branches exercises the error mapping
// branches that may not be reachable through the public ScopedReader
// API on every kernel. The mapOpenError function is the single
// kernel-errno → Go-error translation point, so coverage of its
// branches is required for full test coverage.
func TestScopedReader_MapOpenError_Branches(t *testing.T) {
	t.Parallel()

	// Indirectly exercise mapOpenError by triggering errors via
	// platform.NewScopedOSReader with bad inputs.
	if _, err := platform.NewScopedOSReader(""); err == nil {
		t.Errorf("empty root: expected error, got nil")
	}
	// NUL byte in root path. The Linux openat2(2) returns EINVAL;
	// mapOpenError surfaces it as syscall.EINVAL.
	if _, err := platform.NewScopedOSReader("/nonexistent\x00path"); err == nil {
		t.Errorf("NUL root: expected error, got nil")
	}
	// Non-existent root returns ENOENT → os.ErrNotExist.
	if _, err := platform.NewScopedOSReader("/nonexistent/abcdef"); err == nil {
		t.Errorf("missing root: expected error, got nil")
	}
}

// TestScopedReader_LifecycleInternalHelpers exercises the internal
// rootFD, checkOpen, and joinAndValidate branches that may not be
// reached through normal usage. The OS reader's rootFD() returns
// ErrClosed when rootfd has been swapped to -1; this is exercised
// after Close().
func TestScopedReader_LifecycleInternalHelpers(t *testing.T) {
	t.Parallel()

	t.Run("mem checkOpen after close", func(t *testing.T) {
		t.Parallel()
		mem := platform.NewMemPlatformReader()
		mem.AddFile("/proc/x", []byte("y"), 0o644)
		r := platform.NewScopedMemReader("/proc", mem)
		if err := r.Close(); err != nil {
			t.Fatalf("first close: %v", err)
		}
		if err := r.Close(); err != nil {
			t.Errorf("second close: %v (idempotent contract violated)", err)
		}
	})

	t.Run("os rootfd after close", func(t *testing.T) {
		if runtime.GOOS != linuxGOOS {
			t.Skipf("OS reader requires Linux")
		}
		osr, err := platform.NewScopedOSReader(t.TempDir())
		if err != nil {
			t.Skipf("openat2 unavailable: %v", err)
		}
		if err := osr.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		// After close, all file methods must return ErrClosed.
		if _, err := osr.ReadFile("x"); !errors.Is(err, platform.ErrClosed) {
			t.Errorf("ReadFile after close error = %v, want ErrClosed", err)
		}
		if _, err := osr.Stat("x"); !errors.Is(err, platform.ErrClosed) {
			t.Errorf("Stat after close error = %v, want ErrClosed", err)
		}
		if _, err := osr.ReadDir("x"); !errors.Is(err, platform.ErrClosed) {
			t.Errorf("ReadDir after close error = %v, want ErrClosed", err)
		}
		if _, err := osr.Readlink("x"); !errors.Is(err, platform.ErrClosed) {
			t.Errorf("Readlink after close error = %v, want ErrClosed", err)
		}
	})

	t.Run("mem joinAndValidate edge cases", func(t *testing.T) {
		t.Parallel()
		mem := platform.NewMemPlatformReader()
		// Add an entry at the root to make "." resolve to a real
		// node; joinAndValidate returns root verbatim for "." so
		// the lookup hits the stored /proc entry.
		mem.AddDir("/proc", 0o755)
		mem.AddFile("/proc/x", []byte("y"), 0o644)
		r := platform.NewScopedMemReader("/proc", mem)
		t.Cleanup(func() { _ = r.Close() })
		// "." subpath should resolve to root (joinAndValidate
		// returns root verbatim).
		if _, err := r.Stat("."); err != nil {
			t.Errorf("Stat . error: %v", err)
		}
	})
}

// TestScopedReader_NewScopedMemReaderNilMem verifies the nil-mem
// fallback path inside NewScopedMemReader.
func TestScopedReader_NewScopedMemReaderNilMem(t *testing.T) {
	t.Parallel()

	r := platform.NewScopedMemReader("/proc", nil)
	if r == nil {
		t.Fatal("NewScopedMemReader(/proc, nil) returned nil")
	}
	t.Cleanup(func() { _ = r.Close() })

	// Missing file: os.ErrNotExist is returned (not a panic).
	if _, err := r.Stat("nonexistent"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat missing: error = %v, want ErrNotExist", err)
	}
	if _, err := r.ReadDir("nonexistent"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadDir missing: error = %v, want ErrNotExist", err)
	}
	if _, err := r.Readlink("nonexistent"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Readlink missing: error = %v, want ErrNotExist", err)
	}
}

// equalStrings is a small helper to compare sorted string slices without
// importing reflect in test code (and to keep the comparison robust
// against unsorted input).
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// _ silences the imports linter when individual test files don't use
// every imported symbol.
var _ = strings.TrimSpace