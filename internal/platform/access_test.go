package platform_test

import (
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"os"
	"path/filepath"
	"testing"
)

func TestScopedExecutableAccess(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "executable"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := platform.NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, tc := range []struct {
		name string
		want bool
	}{{"executable", true}, {"data", false}} {
		got, err := r.ExecutableAccess(t.Context(), tc.name)
		if err != nil || got != tc.want {
			t.Fatalf("%s %t %v", tc.name, got, err)
		}
	}
	if _, err := r.ExecutableAccess(t.Context(), "../escape"); err == nil {
		t.Fatal("escaped")
	}
	if _, err := r.ExecutableAccess(t.Context(), "missing"); err == nil {
		t.Fatal("missing observed")
	}
}

func TestMemoryExecutableAccessIsExplicit(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/helper", nil, 0o755)
	r := platform.NewScopedMemReader("/", mem)
	if _, err := r.ExecutableAccess(t.Context(), "helper"); err == nil {
		t.Fatal("bits fabricated access")
	}
	if err := mem.SetExecutableAccess("/helper", true, nil); err != nil {
		t.Fatal(err)
	}
	got, err := r.ExecutableAccess(t.Context(), "helper")
	if err != nil || !got {
		t.Fatal(got, err)
	}
	if err := mem.SetExecutableAccess("/helper", false, os.ErrPermission); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ExecutableAccess(t.Context(), "helper"); err == nil {
		t.Fatal("access error lost")
	}
	if _, err := r.ExecutableAccess(t.Context(), "missing"); err == nil {
		t.Fatal("missing observed")
	}
	r.Close()
	if _, err := r.ExecutableAccess(t.Context(), "helper"); err == nil {
		t.Fatal("closed reader")
	}
}
