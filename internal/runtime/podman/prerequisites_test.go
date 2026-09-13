package podman_test

import (
	"io/fs"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const testUsrDirectory = "/usr"

func TestHelperMetadataPreservesTargetAccessAndSearchFailures(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"accessible candidate", "absent", networkTestDenied, "no access", "directory candidate",
		"unknown access", "linked candidate"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			mem.AddDir(testUsrDirectory, 0o755)
			mem.AddDir("/usr/bin", 0o755)
			name := "/usr/bin/crun"
			if scenario != "absent" {
				mem.AddFile(name, []byte("executable"), 0o755)
				if err := mem.SetOwnership(name, platform.FileOwnership{}); err != nil {
					t.Fatal(err)
				}
				if scenario != "unknown access" {
					if err := mem.SetExecutableAccess(name, scenario != "no access", nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			switch scenario {
			case networkTestDenied:
				mem.AddError(name, fs.ErrPermission)
			case "directory candidate":
				mem.AddDir(name, 0o755)
			case "linked candidate":
				mem.AddSymlink("/usr/bin/runtime", name)
				name = "/usr/bin/runtime"
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope())
			p := podman.ExecutableProbe{Role: "crun", Source: "trusted_candidates", Paths: []string{name}}
			obs, _ := p.Run(t.Context(), env)
			if obs.Scope != versionScope() || obs.Executable == nil || len(obs.Executable.Candidates) != 1 {
				t.Fatal("lost scoped metadata")
			}
			c := obs.Executable.Candidates[0]
			if (obs.Completeness == model.Partial) != (scenario == networkTestDenied || scenario == "unknown access") {
				t.Fatal("lost uncertainty", obs)
			}
			if scenario == "absent" && (c.Present == nil || *c.Present) {
				t.Fatal("absence unknown")
			}
			if scenario == networkTestDenied && c.Present != nil {
				t.Fatal("denial became absence")
			}
			if scenario == "no access" && (c.Executable == nil || *c.Executable) {
				t.Fatal("mode bits replaced target access")
			}
			if scenario == "directory candidate" && (c.File == nil || c.File.Regular) {
				t.Fatal("directory accepted")
			}
			if scenario == "accessible candidate" || scenario == "linked candidate" {
				if c.File == nil || c.File.UID == nil || !c.File.Regular || c.Executable == nil || !*c.Executable {
					t.Fatal("missing identity/access")
				}
			}
		})
	}
}

func TestInfoSelectedExecutableMetadata(t *testing.T) {
	t.Parallel()
	p, err := podman.ParseInfo([]byte(`{"host":{"ociRuntime":{"name":"crun","path":"/opt/crun"},"conmon":{"path":"/opt/conmon"},
"pasta":{"executable":"/opt/pasta"},"slirp4netns":{"executable":"/opt/slirp4netns"},
"networkBackendInfo":{"dns":{"path":"/opt/aardvark-dns"}}}}`))
	if err != nil || p.OCIRuntime == nil || p.OCIRuntime.Name != "crun" || p.OCIRuntime.Path != "/opt/crun" ||
		p.ConmonPath != "/opt/conmon" || p.PastaPath != "/opt/pasta" || p.SlirpPath != "/opt/slirp4netns" ||
		p.AardvarkPath != "/opt/aardvark-dns" {
		t.Fatal("lost effective selections", p, err)
	}
}

func TestSelectedHelpersRequireCompleteSourceAndRetainSelection(t *testing.T) {
	t.Parallel()
	yes := true
	p := model.PodmanInfo{Available: &yes, Path: testPodmanPath, OCIRuntime: &model.SelectedOCIRuntime{Path: "/opt/selected-runtime"},
		ConmonPath: "/opt/selected-conmon", PastaPath: "/opt/selected-pasta", SlirpPath: "/opt/selected-slirp", AardvarkPath: "/opt/selected-dns"}
	for _, completeness := range []model.Completeness{model.Complete, model.Partial, model.Unobserved} {
		t.Run(string(completeness), func(t *testing.T) {
			t.Parallel()
			info := model.Observation{ID: "effective", Scope: versionScope(), Completeness: completeness, Podman: &p}
			probes := podman.SelectedHelperProbes(info, nil)
			if completeness != model.Complete {
				if len(probes) != 0 {
					t.Fatal("incomplete source selected helpers")
				}
				return
			}
			const selectedCount = 5
			if len(probes) != selectedCount {
				t.Fatal("lost reported helper paths", probes)
			}
			for _, probe := range probes {
				if probe.SourceID != "effective" || probe.RuntimePath != testPodmanPath || probe.Source != "runtime" || len(probe.Paths) != 1 {
					t.Fatal("lost selection authority", probe)
				}
			}
		})
	}
	if got := podman.SelectedHelperProbes(model.Observation{}, nil); len(got) != 0 {
		t.Fatal("invented helpers")
	}
}

func TestHelperInventoryIsBoundedAndIndependentOfAmbientPath(t *testing.T) {
	t.Parallel()
	now := func() time.Time { return time.Unix(1, 0) }
	probes := podman.HelperProbes(now)
	const helperCount = 8
	if len(probes) != helperCount {
		t.Fatal("unexpected helper inventory")
	}
	for _, p := range probes {
		if len(p.Dependencies()) != 0 || p.Source != "trusted_candidates" || p.Generator || p.Now() != now() {
			t.Fatal("unexpected command dependency")
		}
		if len(p.Paths) == 0 || len(p.Paths) > 16 {
			t.Fatal("unbounded inventory")
		}
		for _, path := range p.Paths {
			if podman.ValidateExecutablePath(path) != nil {
				t.Fatal("unsafe candidate", path)
			}
		}
	}
}
