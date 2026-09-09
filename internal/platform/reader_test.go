package platform_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// readerTestLinkPath is the canonical path used as a symlink entry in
// table-driven reader tests. Declared as a named constant to satisfy the
// goconst linter without scattering repeated string literals.
const readerTestLinkPath = "/link"

func TestOSPlatformReader_DelegatesToOS(t *testing.T) {
	t.Parallel()

	r := platform.NewOSPlatformReader()

	// Create a temp file and exercise every reader method.
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o600); err != nil {
		t.Fatalf("failed to seed fixture: %v", err)
	}

	data, err := r.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("ReadFile content = %q, want %q", string(data), "hello")
	}

	info, err := r.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.IsDir() {
		t.Error("Stat reports IsDir for regular file")
	}
	if info.Size() != int64(len("hello")) {
		t.Errorf("Stat size = %d, want %d", info.Size(), len("hello"))
	}

	entries, err := r.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "real.txt" {
		t.Errorf("ReadDir entries = %v, want [real.txt]", entries)
	}

	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	tgt, err := r.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if tgt != filepath.Base(target) && tgt != target {
		t.Errorf("Readlink target = %q, want %q or %q", tgt, filepath.Base(target), target)
	}
}

func TestMemPlatformReader_ReadFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(m *platform.MemPlatformReader)
		path    string
		want    []byte
		wantErr error
	}{
		{
			name: "regular file read",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/etc/os-release", []byte("NAME=test"), 0o644)
			},
			path: "/etc/os-release",
			want: []byte("NAME=test"),
		},
		{
			name: "regular file with dotted path normalization",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/etc/os-release", []byte("x"), 0o644)
			},
			path: "/etc/./os-release",
			want: []byte("x"),
		},
		{
			name:    "missing file returns os.ErrNotExist",
			setup:   func(m *platform.MemPlatformReader) {},
			path:    "/no/such/file",
			wantErr: os.ErrNotExist,
		},
		{
			name: "read on directory returns EISDIR",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/var", 0o755)
			},
			path:    "/var",
			wantErr: syscall.EISDIR,
		},
		{
			name: "single-hop symlink resolved",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/target", []byte(testPayload), 0o644)
				m.AddSymlink(readerTestLinkPath, "target")
			},
			path: readerTestLinkPath,
			want: []byte(testPayload),
		},
		{
			name: "multi-hop symlink resolved",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/real", []byte("end"), 0o644)
				m.AddSymlink("/hop1", "hop2")
				m.AddSymlink("/hop2", "hop3")
				m.AddSymlink("/hop3", "real")
			},
			path: "/hop1",
			want: []byte("end"),
		},
		{
			name: "broken symlink returns os.ErrNotExist",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/broken", "does-not-exist")
			},
			path:    "/broken",
			wantErr: os.ErrNotExist,
		},
		{
			name: "cyclic symlink returns ELOOP",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/loop/a", "../loop/b")
				m.AddSymlink("/loop/b", "../loop/a")
				m.AddDir("/loop", 0o755)
			},
			path:    "/loop/a",
			wantErr: syscall.ELOOP,
		},
		{
			name: "deep symlink chain returns ELOOP",
			setup: func(m *platform.MemPlatformReader) {
				// 17 hops exceed maxSymlinkDepth=16.
				for i := 0; i < 18; i++ {
					m.AddSymlink("/chain/"+string(rune('a'+i)), string(rune('a'+i+1)))
				}
				m.AddFile("/chain/s", []byte("z"), 0o644)
				m.AddDir("/chain", 0o755)
			},
			path:    "/chain/a",
			wantErr: syscall.ELOOP,
		},
		{
			name: "forced EACCES error surfaces verbatim",
			setup: func(m *platform.MemPlatformReader) {
				m.AddError("/private", syscall.EACCES)
			},
			path:    "/private",
			wantErr: syscall.EACCES,
		},
		{
			name: "custom error surfaces verbatim",
			setup: func(m *platform.MemPlatformReader) {
				m.AddError("/io", errors.New("disk on fire"))
			},
			path:    "/io",
			wantErr: errors.New("disk on fire"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := platform.NewMemPlatformReader()
			tt.setup(m)

			got, err := m.ReadFile(tt.path)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) && err.Error() != tt.wantErr.Error() {
					t.Fatalf("ReadFile error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadFile unexpected error: %v", err)
			}
			if string(got) != string(tt.want) {
				t.Errorf("ReadFile content = %q, want %q", string(got), string(tt.want))
			}
		})
	}
}

func TestMemPlatformReader_Stat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(m *platform.MemPlatformReader)
		path      string
		wantExist bool
		wantIsDir bool
		wantSize  int64
		wantErr   error
		wantErrIs error
	}{
		{
			name: "regular file stat",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/x", []byte("hello"), 0o600)
			},
			path:      "/x",
			wantExist: true,
			wantSize:  5,
		},
		{
			name: "directory stat",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/d", 0o755)
			},
			path:      "/d",
			wantExist: true,
			wantIsDir: true,
		},
		{
			name: "missing file returns ErrNotExist",
			setup: func(m *platform.MemPlatformReader) {
			},
			path:      "/missing",
			wantExist: false,
			wantErrIs: os.ErrNotExist,
		},
		{
			name: "symlink resolution",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/data", []byte(testPayload), 0o644)
				m.AddSymlink("/link", "data")
			},
			path:      "/link",
			wantExist: true,
			wantSize:  7,
		},
		{
			name: "cyclic symlink returns ELOOP",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/c", 0o755)
				m.AddSymlink("/c/a", "b")
				m.AddSymlink("/c/b", "a")
			},
			path:    "/c/a",
			wantErr: syscall.ELOOP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := platform.NewMemPlatformReader()
			tt.setup(m)

			info, err := m.Stat(tt.path)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Stat error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("Stat error = %v, want %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Stat unexpected error: %v", err)
			}
			if info.IsDir() != tt.wantIsDir {
				t.Errorf("Stat IsDir = %v, want %v", info.IsDir(), tt.wantIsDir)
			}
			if info.Size() != tt.wantSize {
				t.Errorf("Stat Size = %d, want %d", info.Size(), tt.wantSize)
			}
		})
	}
}

func TestMemPlatformReader_ReadDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(m *platform.MemPlatformReader)
		path      string
		wantNames []string
		wantErr   error
	}{
		{
			name: "enumerate children sorted alphabetically",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/etc", 0o755)
				m.AddFile("/etc/zeta", []byte("z"), 0o644)
				m.AddFile("/etc/alpha", []byte("a"), 0o644)
				m.AddFile("/etc/middle", []byte("m"), 0o644)
			},
			path:      "/etc",
			wantNames: []string{"alpha", "middle", "zeta"},
		},
		{
			name: "directory entries expose type",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/d", 0o755)
				m.AddFile("/d/file", []byte("f"), 0o644)
				m.AddDir("/d/subdir", 0o755)
			},
			path:      "/d",
			wantNames: []string{"file", "subdir"},
		},
		{
			name: "missing path returns ErrNotExist",
			setup: func(m *platform.MemPlatformReader) {
			},
			path:    "/nope",
			wantErr: os.ErrNotExist,
		},
		{
			name: "readdir on regular file returns ENOTDIR",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/file", []byte("x"), 0o644)
			},
			path:    "/file",
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "readdir on symlink to file returns ENOTDIR",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/real", []byte("r"), 0o644)
				m.AddSymlink("/alias", "real")
			},
			path:    "/alias",
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "readdir on symlink to directory works",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/realdir", 0o755)
				m.AddFile("/realdir/child", []byte("c"), 0o644)
				m.AddSymlink("/alias", "realdir")
			},
			path:      "/alias",
			wantNames: []string{"child"},
		},
		{
			name: "forced error propagates",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/perm", 0o755)
				m.AddError("/perm/secret", syscall.EACCES)
			},
			path:    "/perm/secret",
			wantErr: syscall.EACCES,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := platform.NewMemPlatformReader()
			tt.setup(m)

			entries, err := m.ReadDir(tt.path)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ReadDir error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadDir unexpected error: %v", err)
			}
			if len(entries) != len(tt.wantNames) {
				t.Fatalf("ReadDir returned %d entries, want %d (%v)", len(entries), len(tt.wantNames), tt.wantNames)
			}
			for i, name := range tt.wantNames {
				if entries[i].Name() != name {
					t.Errorf("entry[%d] = %q, want %q", i, entries[i].Name(), name)
				}
			}
		})
	}
}

func TestMemPlatformReader_Readlink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(m *platform.MemPlatformReader)
		path    string
		want    string
		wantErr error
	}{
		{
			name: "symlink returns raw target",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/link", "../some/target")
			},
			path: "/link",
			want: "../some/target",
		},
		{
			name: "absolute symlink target returned verbatim",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/link", "/abs/path")
			},
			path: "/link",
			want: "/abs/path",
		},
		{
			name: "missing path returns ErrNotExist",
			setup: func(m *platform.MemPlatformReader) {
			},
			path:    "/no/such",
			wantErr: os.ErrNotExist,
		},
		{
			name: "regular file returns EINVAL",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/file", []byte("x"), 0o644)
			},
			path:    "/file",
			wantErr: syscall.EINVAL,
		},
		{
			name: "directory returns EINVAL",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/dir", 0o755)
			},
			path:    "/dir",
			wantErr: syscall.EINVAL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := platform.NewMemPlatformReader()
			tt.setup(m)

			got, err := m.Readlink(tt.path)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Readlink error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Readlink unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Readlink = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMemPlatformReader_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	m := platform.NewMemPlatformReader()
	m.AddFile("/x", []byte(testPayload), 0o644)

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				if _, err := m.ReadFile("/x"); err != nil {
					t.Errorf("ReadFile error: %v", err)
					return
				}
				if _, err := m.Stat("/x"); err != nil {
					t.Errorf("Stat error: %v", err)
					return
				}
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
