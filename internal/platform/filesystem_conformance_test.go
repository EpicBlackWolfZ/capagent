package platform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const (
	resolvedValue    = "right"
	absoluteLinkName = "absolute"
	physicalBackend  = "physical"
)

func pathFixture() *platform.MemPlatformReader {
	m := platform.NewMemPlatformReader()
	for _, p := range []string{"/", "/scope", "/scope/real", "/scope/real/nested"} {
		m.AddDir(p, 0o755)
	}
	m.AddFile("/scope/value", []byte("wrong"), 0o644)
	m.AddFile("/scope/real/value", []byte(resolvedValue), 0o644)
	m.AddFile("/scope/real/nested/leaf", []byte("leaf"), 0o644)
	m.AddSymlink("/scope/link", "real/nested")
	m.AddSymlink("/scope/absolute", "/scope/real/value")
	m.AddSymlink("/scope/real/nested/raw", "../missing")
	return m
}

func TestFilesystemPathConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path, want string
		err        error
	}{
		{"link/../value", resolvedValue, nil}, {"link/leaf", "leaf", nil},
		{absoluteLinkName, resolvedValue, nil}, {"value/", "", syscall.ENOTDIR},
		{"value/../value", "", syscall.ENOTDIR}, {"missing/../value", "", os.ErrNotExist},
	}
	for _, backend := range []string{"scoped", physicalBackend, "root", "os"} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()
			m := pathFixture()
			var r platform.PlatformReader = platform.NewScopedMemReader("/scope", m)
			prefix := ""
			if backend == physicalBackend {
				r = m
				prefix = "/scope/"
			}
			if backend == "root" {
				r = platform.NewScopedMemReader("/", m)
				prefix = "scope/"
			}
			if backend == "os" {
				r = openConformanceReader(t, rawOSFixture(t, m))
			}
			for _, tt := range tests {
				t.Run(tt.path, func(t *testing.T) {
					wantErr := tt.err
					if backend == "os" && tt.path == absoluteLinkName {
						wantErr = os.ErrNotExist
					}
					want := tt.want
					if wantErr != nil {
						want = ""
					}
					got, err := r.ReadFile(t.Context(), prefix+tt.path)
					if !errors.Is(err, wantErr) || string(got) != want {
						t.Fatalf("ReadFile = %q, %v; want %q, %v", got, err, tt.want, tt.err)
					}
				})
			}
			got, err := r.Readlink(prefix + "link/raw")
			if err != nil || got != "../missing" {
				t.Fatalf("Readlink = %q, %v", got, err)
			}
		})
	}
}

func TestDirectorySnapshotConformance(t *testing.T) {
	t.Parallel()
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: physicalBackend, true: "scoped"}[scoped], func(t *testing.T) {
			t.Parallel()
			m := pathFixture()
			var r platform.PlatformReader = m
			dir := "/scope/real"
			if scoped {
				r = platform.NewScopedMemReader("/scope", m)
				dir = "real"
			}
			entries, err := r.ReadDir(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			m.AddFile("/scope/real/value", []byte("replacement"), 0o600)
			for _, e := range entries {
				info, ierr := e.Info()
				if ierr != nil {
					t.Fatal(ierr)
				}
				if e.Name() == "value" && (info.Size() != int64(len(resolvedValue)) || info.Mode() != 0o644) {
					t.Fatalf("snapshot changed: %+v", info)
				}
			}
			m.AddError("/scope/real/failure", syscall.EIO)
			got, err := r.ReadDir(t.Context(), dir)
			if !errors.Is(err, syscall.EIO) || got != nil {
				t.Fatalf("ReadDir = %v, %v; want nil, EIO", got, err)
			}
		})
	}
}

// rawOSFixture reuses the existing materializer for nodes, but stores link
// targets verbatim. Unlike the historical parity factory it permits broken links.
func rawOSFixture(t *testing.T, mem *platform.MemPlatformReader) string {
	t.Helper()
	root := t.TempDir()
	for path, node := range mem.Snapshot() {
		if path == "/" {
			continue
		}
		target, ok := translateVirtualPath(path, "/scope", root)
		if !ok {
			t.Fatalf("fixture path outside scope: %q", path)
		}
		if node.Kind == platform.FileKindSymlink {
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(node.Target, target); err != nil {
				t.Fatal(err)
			}
		} else {
			materializeTreeEntry(t, target, node, "/scope", root)
		}
	}
	return root
}

func openConformanceReader(t *testing.T, root string) platform.ScopedReader {
	t.Helper()
	r, err := platform.NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestOSAdapterTargetSelection(t *testing.T) {
	t.Parallel()
	mem := pathFixture()
	mem.AddSymlink("/scope/self", "real/nested")
	root := rawOSFixture(t, mem)
	r := openConformanceReader(t, root)
	p := platform.NewProcfsReader(r)
	s := platform.NewSysfsReader(r)
	if p.Root() != root || s.Root() != root {
		t.Fatal("adapter root differs from reader")
	}
	for _, read := range []func(context.Context, string) ([]byte, error){p.ReadProcFile, s.ReadSysFile} {
		got, err := read(t.Context(), "link/../value")
		if err != nil || string(got) != resolvedValue {
			t.Fatalf("read = %q, %v", got, err)
		}
	}
	got, err := p.ReadSelf(t.Context(), "../value")
	if !errors.Is(err, platform.ErrSubpathEscape) || got != nil {
		t.Fatalf("ReadSelf accepted lexical escape: %q, %v", got, err)
	}
	got, err = p.ReadSelf(t.Context(), "raw/../value")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("broken intermediate link must remain broken: %q, %v", got, err)
	}
}

func TestAbsoluteTargetPolicies(t *testing.T) {
	t.Parallel()
	mem := pathFixture()
	mem.AddSymlink("/scope/rebased", "/real/value")
	mem.AddSymlink("/scope/broken", "/missing")
	root := rawOSFixture(t, mem)
	osReader := openConformanceReader(t, root)
	memReader := platform.NewScopedMemReader("/scope", mem)
	for _, tt := range []struct {
		name       string
		reader     platform.ScopedReader
		path, want string
		err        error
	}{
		{"OS rebase", osReader, "rebased", resolvedValue, nil},
		{"memory rejects external absolute", memReader, "rebased", "", platform.ErrSubpathEscape},
		{"memory physical absolute", memReader, absoluteLinkName, resolvedValue, nil},
		{"OS reinterprets physical absolute", osReader, absoluteLinkName, "", os.ErrNotExist},
		{"OS broken absolute", osReader, "broken", "", os.ErrNotExist},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.reader.ReadFile(t.Context(), tt.path)
			if string(got) != tt.want || !errors.Is(err, tt.err) {
				t.Fatalf("got %q, %v; want %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
	for _, r := range []platform.ScopedReader{osReader, memReader} {
		got, err := r.Readlink("broken")
		if err != nil || got != "/missing" {
			t.Fatalf("raw broken target = %q, %v", got, err)
		}
	}
}

func TestOSRootSlash(t *testing.T) {
	t.Parallel()
	root := rawOSFixture(t, pathFixture())
	r := openConformanceReader(t, "/")
	got, err := r.ReadFile(t.Context(), strings.TrimPrefix(root, "/")+"/link/../value")
	if err != nil || string(got) != resolvedValue {
		t.Fatalf("root / read = %q, %v", got, err)
	}
}

func TestTraversalOperations(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"memory", "os", physicalBackend, "physical-os"} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()
			mem := pathFixture()
			mem.AddSymlink("/scope/second", "link")
			mem.AddSymlink("/scope/alias", "real/value")
			mem.AddSymlink("/scope/complex", "link/../value")
			mem.AddFile(`/scope/real/nested/a\b`, []byte("literal"), 0o644)
			var r platform.PlatformReader = platform.NewScopedMemReader("/scope", mem)
			prefix := ""
			if backend == "os" {
				r = openConformanceReader(t, rawOSFixture(t, mem))
			}
			if backend == physicalBackend {
				r = mem
				prefix = "/scope/"
			}
			if backend == "physical-os" {
				r = platform.NewOSPlatformReader()
				prefix = rawOSFixture(t, mem) + "/"
			}
			got, err := r.ReadFile(t.Context(), prefix+"complex")
			if err != nil || string(got) != resolvedValue {
				t.Errorf("complex target = %q, %v", got, err)
			}
			for _, path := range []string{"second/./leaf", "link//leaf", `link/a\b`} {
				if _, err := r.ReadFile(t.Context(), prefix+path); err != nil {
					t.Errorf("ReadFile(%q): %v", path, err)
				}
			}
			for _, path := range []string{"link/", "link/.", "second/"} {
				info, err := r.Stat(prefix + path)
				if err != nil || !info.IsDir() {
					t.Errorf("Stat(%q) = %v, %v", path, info, err)
				}
				entries, err := r.ReadDir(t.Context(), prefix+path)
				if err != nil || len(entries) == 0 {
					t.Errorf("ReadDir(%q) = %v, %v", path, entries, err)
				}
				if _, err := r.Readlink(prefix + path); !errors.Is(err, syscall.EINVAL) {
					t.Errorf("Readlink(%q) = %v", path, err)
				}
			}
			info, err := r.Stat(prefix + "alias")
			if err != nil {
				t.Fatal(err)
			}
			if info.Name() != "alias" {
				t.Errorf("Stat alias Name = %q", info.Name())
			}
			for _, path := range []string{"value/", "value/../real"} {
				if _, err := r.Stat(prefix + path); !errors.Is(err, syscall.ENOTDIR) {
					t.Errorf("Stat(%q) = %v", path, err)
				}
				if _, err := r.ReadDir(t.Context(), prefix+path); !errors.Is(err, syscall.ENOTDIR) {
					t.Errorf("ReadDir(%q) = %v", path, err)
				}
				if _, err := r.Readlink(prefix + path); !errors.Is(err, syscall.ENOTDIR) {
					t.Errorf("Readlink(%q) = %v", path, err)
				}
			}
		})
	}
}

func TestDirectorySnapshotsAfterClose(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"scoped", physicalBackend} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()
			root := rawOSFixture(t, pathFixture())
			var r platform.PlatformReader = platform.NewOSPlatformReader()
			path := root + "/real/nested"
			if backend == "scoped" {
				r = openConformanceReader(t, root)
				path = "real/nested"
			}
			entries, err := r.ReadDir(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if closer, ok := r.(platform.ScopedReader); ok {
				if err := closer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			previous := ""
			for _, entry := range entries {
				if entry.Name() <= previous {
					t.Fatalf("unsorted entries: %v", entries)
				}
				previous = entry.Name()
				info, err := entry.Info()
				if err != nil {
					t.Fatal(err)
				}
				if entry.Name() == "leaf" && info.Size() != int64(len("leaf")) {
					t.Errorf("size = %d", info.Size())
				}
				if entry.Name() == "raw" && info.Mode()&os.ModeSymlink == 0 {
					t.Error("child symlink was followed")
				}
			}
		})
	}
}

func TestMemoryRelativeNamespace(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddDir("..", 0o755)
	mem.AddDir("../..", 0o755)
	mem.AddFile("../../value", []byte("parent"), 0o644)
	mem.AddFile("value", []byte("local"), 0o644)
	mem.AddSymlink("link", "../../value")
	got, err := mem.ReadFile(t.Context(), "link")
	if err != nil || string(got) != "parent" {
		t.Fatalf("relative target = %q, %v", got, err)
	}
	scoped := platform.NewScopedMemReader(".", mem)
	if _, err := scoped.ReadFile(t.Context(), "link"); !errors.Is(err, platform.ErrSubpathEscape) {
		t.Fatalf("relative root escaped: %v", err)
	}
}

func TestMemoryScopeBoundaries(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"../outside", "/scope-other/value", "/scope/../scope/value"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			mem := pathFixture()
			mem.AddSymlink("/scope/escape", target)
			r := platform.NewScopedMemReader("/scope", mem)
			if _, err := r.ReadFile(t.Context(), "escape"); !errors.Is(err, platform.ErrSubpathEscape) {
				t.Fatalf("escape = %v", err)
			}
		})
	}
	for _, failure := range []error{syscall.EACCES, syscall.EIO, syscall.ENOTDIR} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			mem := pathFixture()
			mem.AddError("/scope/real", failure)
			r := platform.NewScopedMemReader("/scope", mem)
			if _, err := r.ReadFile(t.Context(), "link/leaf"); !errors.Is(err, failure) {
				t.Fatalf("intermediate error = %v", err)
			}
		})
	}
}

func TestMemoryDirectoryLinkSnapshot(t *testing.T) {
	t.Parallel()
	mem := pathFixture()
	r := platform.NewScopedMemReader("/scope", mem)
	entries, err := r.ReadDir(t.Context(), "real/nested")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "raw" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != int64(len("../missing")) {
			t.Fatalf("link size = %d", info.Size())
		}
		return
	}
	t.Fatal("missing raw link")
}

func TestMemorySymlinkHopBoundary(t *testing.T) {
	t.Parallel()
	const hopLimit = 16
	for _, hops := range []int{hopLimit, hopLimit + 1} {
		t.Run(strconv.Itoa(hops), func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			mem.AddDir("/scope", 0o755)
			for i := 0; i < hops; i++ {
				mem.AddSymlink("/scope/"+strconv.Itoa(i), strconv.Itoa(i+1))
			}
			mem.AddFile("/scope/"+strconv.Itoa(hops), []byte("end"), 0o644)
			for _, scoped := range []bool{false, true} {
				var r platform.PlatformReader = mem
				path := "/scope/0"
				if scoped {
					r = platform.NewScopedMemReader("/scope", mem)
					path = "0"
				}
				got, err := r.ReadFile(t.Context(), path)
				if hops > hopLimit {
					if !errors.Is(err, syscall.ELOOP) {
						t.Fatalf("error = %v, want ELOOP", err)
					}
				} else if err != nil || string(got) != "end" {
					t.Fatalf("at hop limit = %q, %v", got, err)
				}
			}
		})
	}
}
