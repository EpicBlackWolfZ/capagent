package podman_test

import (
	"io/fs"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestQuadletPrecedenceAndMasks(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"vendor", "priority generator", "empty mask", "null mask", "denied override", "non executable"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			vendor := "/usr/lib/systemd/user-generators/podman-user-generator"
			override := "/etc/systemd/user-generators/podman-user-generator"
			for _, dir := range []string{testUsrDirectory, "/usr/lib", "/usr/lib/systemd", "/usr/lib/systemd/user-generators",
				"/etc", "/etc/systemd", "/etc/systemd/user-generators"} {
				mem.AddDir(dir, 0o755)
			}
			mem.AddFile(vendor, []byte("generator"), 0o755)
			mem.SetExecutableAccess(vendor, true, nil)
			if scenario == "priority generator" || scenario == "non executable" {
				mem.AddFile(override, []byte("priority generator"), 0o755)
				mem.SetExecutableAccess(override, scenario == "priority generator", nil)
			}
			switch scenario {
			case "empty mask":
				mem.AddFile(override, nil, 0o755)
			case "null mask":
				mem.AddSymlink(override, "/dev/null")
			case "denied override":
				mem.AddError(override, fs.ErrPermission)
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope())
			p := podman.QuadletGenerator(true, nil)
			obs, _ := p.Run(t.Context(), env)
			x := obs.Executable
			want := override
			if scenario == "vendor" {
				want = vendor
			}
			if scenario == "denied override" {
				want = ""
			}
			if x.SelectedPath != want {
				t.Fatal("wrong precedence", x)
			}
			if (obs.Completeness == model.Partial) != (scenario == "denied override") {
				t.Fatal("denial lost")
			}
			last := x.Candidates[len(x.Candidates)-1]
			if last.Masked != (scenario == "empty mask" || scenario == "null mask") {
				t.Fatal("mask lost")
			}
		})
	}
}

func TestQuadletTargetSearchPaths(t *testing.T) {
	t.Parallel()
	policy, err := platform.NewEnvPolicy(nil, map[string]string{
		"HOME": "/home/target", "XDG_CONFIG_HOME": "/srv/config", "XDG_RUNTIME_DIR": "/run/user/1001"})
	if err != nil {
		t.Fatal(err)
	}
	p := podman.QuadletLocations{Target: model.UserIdentity{UID: 1001}, Environment: policy}
	mem := platform.NewMemPlatformReader()
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope())
	obs, err := p.Run(t.Context(), env)
	if err != nil || obs.Quadlet == nil || !obs.Quadlet.Rootless || len(obs.Quadlet.Locations) != 4 {
		t.Fatal(obs, err)
	}
	if obs.Quadlet.Locations[0].Path != "/run/user/1001/containers/systemd" ||
		obs.Quadlet.Locations[1].Path != "/srv/config/containers/systemd" ||
		obs.Quadlet.Locations[3].Path != "/etc/containers/systemd/users/1001" {
		t.Fatal("wrong target/environment", obs.Quadlet)
	}
	p.Target.UID = 0
	obs, err = p.Run(t.Context(), env)
	if err != nil || obs.Quadlet.Rootless || len(obs.Quadlet.Locations) != 3 ||
		obs.Quadlet.Locations[0].Path != "/run/containers/systemd" {
		t.Fatal("rootful search", obs, err)
	}
}
