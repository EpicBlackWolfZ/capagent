package platform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestScopedDirectoryAccessIsConfinedAndPassive(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "regular-node"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/data", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	reader, err := platform.NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, write := range []bool{false, true} {
		for _, name := range []string{"data", "link"} {
			allowed, err := reader.DirectoryAccess(t.Context(), name, write)
			if err != nil || !allowed {
				t.Fatalf("%s write=%t allowed=%t error=%v", name, write, allowed, err)
			}
		}
	}
	for _, name := range []string{"../escape", "unobserved-directory", "regular-node"} {
		if _, err := reader.DirectoryAccess(t.Context(), name, true); err == nil {
			t.Fatal("invalid directory accepted", name)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reader.DirectoryAccess(ctx, "data", true); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation")
	}
	entries, err := os.ReadDir(filepath.Join(root, "data"))
	if err != nil || len(entries) != 0 {
		t.Fatal("access query changed directory")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.DirectoryAccess(t.Context(), "data", true); !errors.Is(err, platform.ErrClosed) {
		t.Fatal("lost closed state")
	}
}

func TestMemoryDirectoryAccessRequiresExplicitCredentialResult(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/data", 0o777)
	mem.AddFile("/regular-node", nil, 0o777)
	reader := platform.NewScopedMemReader("/", mem)
	defer reader.Close()
	if _, err := reader.DirectoryAccess(t.Context(), "data", true); !errors.Is(err, platform.ErrIncomplete) {
		t.Fatal("mode bits fabricated ACL result")
	}
	if err := mem.SetDirectoryAccess("/data", true, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := mem.SetDirectoryAccess("/data", false, true, nil); err != nil {
		t.Fatal(err)
	}
	for _, write := range []bool{false, true} {
		allowed, err := reader.DirectoryAccess(t.Context(), "data", write)
		if err != nil || allowed == write {
			t.Fatal("read/write decisions collapsed")
		}
	}
	if err := mem.SetDirectoryAccess("/data", true, false, syscall.ENOSYS); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.DirectoryAccess(t.Context(), "data", true); !errors.Is(err, syscall.ENOSYS) {
		t.Fatal("unsupported query became denied access")
	}
	for _, name := range []string{"unobserved-directory", "regular-node", "../escape"} {
		if _, err := reader.DirectoryAccess(t.Context(), name, true); err == nil {
			t.Fatal("invalid directory accepted")
		}
	}
}
