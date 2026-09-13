package contract_test

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
	"os"
	"path/filepath"
	"testing"
)

func TestHelperMetadataKernelConfinement(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	root := filepath.Join(directory, "root")
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "crun"), []byte("outside executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../crun", filepath.Join(root, "usr", "bin", "crun")); err != nil {
		t.Fatal(err)
	}
	files, err := platform.NewScopedOSReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(metadataTestScope())
	p := podman.ExecutableProbe{Role: "crun", Source: "trusted_candidates", Paths: []string{"/usr/bin/crun"}}
	obs, err := p.Run(t.Context(), env)
	if err != nil || obs.Completeness != model.Complete ||
		obs.Executable.Candidates[0].Present == nil || *obs.Executable.Candidates[0].Present {
		t.Fatal("helper metadata escaped kernel root", obs, err)
	}
}

func TestQuadletNullMaskThroughSymlinkChain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mask := filepath.Join(dir, "mask")
	name := filepath.Join(dir, "generator")
	if err := os.Symlink("/dev/null", mask); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("mask", name); err != nil {
		t.Fatal(err)
	}
	files, err := platform.NewScopedOSReader("/")
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(metadataTestScope())
	p := podman.ExecutableProbe{Role: "quadlet", Source: "systemd_user_generators", Paths: []string{name}, Generator: true}
	obs, err := p.Run(t.Context(), env)
	if err != nil || !obs.Executable.Candidates[0].Masked {
		t.Fatal("symlink chain lost /dev/null mask", obs, err)
	}
}

func metadataTestScope() model.EvaluationScope {
	return model.EvaluationScope{RunID: "run", ContextID: "current", Runtime: "podman", Endpoint: "local"}
}
