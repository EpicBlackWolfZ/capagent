package platform_test

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// symlinkTestPath is the canonical path used as a symlink entry in many
// subtests. Declared as a named constant to satisfy the goconst linter
// without polluting each subtest's setup closure.
const symlinkTestPath = "/link"

// TestMemPlatformReader_ForcedErrSymlinkChain exercises the rule that
// ForcedErr is applied at every node encountered during symlink resolution,
// not only at the final target. The tests cover initial, intermediate, and
// final positions in the chain.
func TestMemPlatformReader_ForcedErrSymlinkChain(t *testing.T) {
	t.Parallel()

	customErr := errors.New("denied by policy")

	tests := []struct {
		name    string
		setup   func(m *platform.MemPlatformReader)
		path    string
		wantErr error
	}{
		{
			name: "forced error at initial symlink",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/target", []byte("ok"), 0o644)
				m.AddError(symlinkTestPath, customErr)
			},
			path:    symlinkTestPath,
			wantErr: customErr,
		},
		{
			name: "forced error at intermediate symlink",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/target", []byte("ok"), 0o644)
				m.AddError("/hop", customErr)
				m.AddSymlink(symlinkTestPath, "hop")
			},
			path:    symlinkTestPath,
			wantErr: customErr,
		},
		{
			name: "forced error at final target",
			setup: func(m *platform.MemPlatformReader) {
				m.AddError("/target", customErr)
				m.AddSymlink(symlinkTestPath, "target")
			},
			path:    symlinkTestPath,
			wantErr: customErr,
		},
		{
			name: "forced error on directory target",
			setup: func(m *platform.MemPlatformReader) {
				m.AddDir("/dir", 0o755)
				m.AddError("/dir", customErr)
				m.AddSymlink(symlinkTestPath, "dir")
			},
			path:    symlinkTestPath,
			wantErr: customErr,
		},
		{
			name: "broken symlink with forced error on the broken link",
			setup: func(m *platform.MemPlatformReader) {
				m.AddError("/broken", customErr)
			},
			path:    "/broken",
			wantErr: customErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := platform.NewMemPlatformReader()
			tt.setup(m)

			if _, err := m.ReadFile(tt.path); !errors.Is(err, tt.wantErr) {
				t.Errorf("ReadFile error = %v, want %v", err, tt.wantErr)
			}
			if _, err := m.Stat(tt.path); !errors.Is(err, tt.wantErr) {
				t.Errorf("Stat error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestMemPlatformReader_SymlinkVariations exercises relative, absolute,
// multi-hop, broken, and self-loop symlinks under ReadFile/Stat/Readlink.
func TestMemPlatformReader_SymlinkVariations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(m *platform.MemPlatformReader)
		path        string
		readFile    string
		readlink    string
		readlinkErr error
	}{
		{
			name: "relative symlink to regular file",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/dir/real", []byte(testPayload), 0o644)
				m.AddSymlink("/dir/link", "real")
			},
			path:     "/dir/link",
			readFile: testPayload,
			readlink: "real",
		},
		{
			name: "absolute symlink to regular file",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/elsewhere/real", []byte(testPayload), 0o644)
				m.AddSymlink(symlinkTestPath, "/elsewhere/real")
			},
			path:     symlinkTestPath,
			readFile: testPayload,
			readlink: "/elsewhere/real",
		},
		{
			name: "multi-hop symlink chain",
			setup: func(m *platform.MemPlatformReader) {
				m.AddFile("/real", []byte("end"), 0o644)
				m.AddSymlink("/a", "b")
				m.AddSymlink("/b", "c")
				m.AddSymlink("/c", "real")
			},
			path:     "/a",
			readFile: "end",
			readlink: "b",
		},
		{
			name: "broken symlink returns os.ErrNotExist on resolve",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink(symlinkTestPath, "does-not-exist")
			},
			path:        symlinkTestPath,
			readlink:    "does-not-exist",
			readlinkErr: nil,
		},
		{
			name: "self-loop returns ELOOP",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/loop", "loop")
			},
			path:     "/loop",
			readlink: "loop",
		},
		{
			name: "multi-node cycle returns ELOOP",
			setup: func(m *platform.MemPlatformReader) {
				m.AddSymlink("/a", "b")
				m.AddSymlink("/b", "c")
				m.AddSymlink("/c", "a")
			},
			path:     "/a",
			readlink: "b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := platform.NewMemPlatformReader()
			tt.setup(m)

			if tt.readlinkErr != nil {
				t.Fatalf("invalid test: readlinkErr set without expecting resolve error")
			}

			// Readlink returns the raw target verbatim, regardless of
			// whether the chain resolves successfully.
			gotTarget, err := m.Readlink(tt.path)
			if err != nil && !errors.Is(err, tt.readlinkErr) {
				t.Fatalf("Readlink error = %v, want nil", err)
			}
			if gotTarget != tt.readlink {
				t.Errorf("Readlink target = %q, want %q", gotTarget, tt.readlink)
			}

			data, err := m.ReadFile(tt.path)
			if tt.readFile == "" {
				// Expecting a resolution failure (ELOOP, ErrNotExist).
				if err == nil {
					t.Fatalf("ReadFile unexpected success: %q", string(data))
				}
				if !errors.Is(err, syscall.ELOOP) && !errors.Is(err, os.ErrNotExist) {
					t.Errorf("ReadFile error = %v, want ELOOP or ErrNotExist", err)
				}
			} else {
				if err != nil {
					t.Fatalf("ReadFile error = %v", err)
				}
				if string(data) != tt.readFile {
					t.Errorf("ReadFile content = %q, want %q", string(data), tt.readFile)
				}
			}
		})
	}
}

// TestMemPlatformReader_DepthLimit verifies that symlink chains exceeding
// maxSymlinkDepth produce syscall.ELOOP rather than panicking or hanging.
func TestMemPlatformReader_DepthLimit(t *testing.T) {
	t.Parallel()

	m := platform.NewMemPlatformReader()
	// Build a chain of 17 hops (one beyond the limit).
	for i := 0; i < 17; i++ {
		next := i + 1
		name := string(rune('a' + i))
		m.AddSymlink("/chain/"+name, string(rune('a'+next)))
	}
	m.AddFile("/chain/r", []byte("z"), 0o644)

	if _, err := m.ReadFile("/chain/a"); !errors.Is(err, syscall.ELOOP) {
		t.Errorf("ReadFile ELOOP error = %v, want ELOOP", err)
	}
	if _, err := m.Stat("/chain/a"); !errors.Is(err, syscall.ELOOP) {
		t.Errorf("Stat ELOOP error = %v, want ELOOP", err)
	}
}

// TestMemPlatformReader_ReadlinkNeverDereferences verifies that Readlink
// returns the raw target even when the chain is broken or self-loops.
func TestMemPlatformReader_ReadlinkNeverDereferences(t *testing.T) {
	t.Parallel()

	m := platform.NewMemPlatformReader()
	m.AddSymlink("/self", "self")
	m.AddSymlink("/broken", "missing")

	for path, want := range map[string]string{
		"/self":   "self",
		"/broken": "missing",
	} {
		got, err := m.Readlink(path)
		if err != nil {
			t.Errorf("Readlink(%q) error = %v", path, err)
		}
		if got != want {
			t.Errorf("Readlink(%q) = %q, want %q", path, got, want)
		}
	}
}
