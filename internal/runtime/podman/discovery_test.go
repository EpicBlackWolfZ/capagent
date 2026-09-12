package podman_test

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestDiscoveryCancellationAndMissingService(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"cancelled", "invalid path", "missing service", "symlink", "denied then found"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			for _, dir := range []string{"/usr", "/usr/bin", "/usr/local", "/usr/local/bin"} {
				mem.AddDir(dir, 0o755)
			}
			mem.AddFile("/usr/local/bin/podman", nil, 0o755)
			if scenario == "symlink" {
				mem.AddSymlink("/usr/bin/podman", "/usr/local/bin/podman")
			}
			if scenario == "denied then found" {
				mem.AddError("/usr/bin/podman", fs.ErrPermission)
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithScope(versionScope())
			if scenario != "missing service" {
				env = env.WithFiles(files)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			p := podman.DiscoveryProbe{}
			if scenario == "invalid path" {
				p.Path = "relative"
			}
			obs, err := p.Run(ctx, env)
			found := scenario == "symlink" || scenario == "denied then found"
			if (err == nil) != found {
				t.Fatal("wrong failure outcome", err)
			}
			if found && (obs.Discovery.Installed == nil || !*obs.Discovery.Installed) {
				t.Fatal("lost discovered file")
			}
			if scenario == "denied then found" && (obs.Completeness != model.Partial || len(obs.Diagnostics) == 0) {
				t.Fatal("lost prior lookup uncertainty")
			}
		})
	}
}

func TestDiscovery(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"missing", "present", "permission", "not executable", "directory", "override", "ordered"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			for _, dir := range []string{"/usr", "/usr/bin", "/usr/local", "/usr/local/bin", "/bin"} {
				mem.AddDir(dir, 0o755)
			}
			p := podman.DiscoveryProbe{Now: func() time.Time { return time.Unix(1, 0) }}
			switch scenario {
			case "present", "override", "ordered":
				mem.AddFile("/usr/bin/podman", nil, 0o755)
			case "permission":
				mem.AddError("/usr/bin/podman", fs.ErrPermission)
			case "not executable":
				mem.AddFile("/usr/bin/podman", nil, 0o644)
			case "directory":
				mem.AddDir("/usr/bin/podman", 0o755)
			}
			if scenario == "override" {
				p.Path = "/bin/podman"
			}
			if scenario == "ordered" {
				mem.AddFile("/usr/local/bin/podman", nil, 0o755)
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope())
			obs, err := p.Run(t.Context(), env)
			if err != nil {
				t.Fatal(err)
			}
			if obs.Discovery == nil || len(obs.Facts) == 0 {
				t.Fatal("missing discovery observation")
			}
			d := obs.Discovery
			switch scenario {
			case "missing", "override":
				if d.Installed == nil || *d.Installed {
					t.Fatal("missing not definitive")
				}
			case "present", "ordered":
				if d.Installed == nil || !*d.Installed || d.Path != "/usr/bin/podman" {
					t.Fatal("wrong selection")
				}
			case "permission", "directory":
				if d.Installed != nil || len(obs.Diagnostics) == 0 {
					t.Fatal("uncertainty collapsed")
				}
			case "not executable":
				if d.File == nil || d.File.ExecutableBits {
					t.Fatal("wrong file metadata")
				}
			}
		})
	}
}

func versionScope() model.EvaluationScope {
	return model.EvaluationScope{RunID: "run", ContextID: "current", Runtime: "podman", Endpoint: "local"}
}

func TestDiscoveryPathValidation(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"podman", "/tmp/../podman", "/tmp/podman\n", "/usr/bin/podmansh", "/usr/bin/-podmansh", "//podman"} {
		if podman.ValidateExecutablePath(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, good := range []string{"", "/usr/bin/podman", "/opt/podman-custom"} {
		if err := podman.ValidateExecutablePath(good); err != nil {
			t.Errorf("rejected %q: %v", good, err)
		}
	}
}
