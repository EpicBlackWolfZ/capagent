package podman_test

import (
	"io/fs"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestConfiguredHelperSelection(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"paths", "special fallback", "directory candidate skipped",
		networkTestNonExecutable, "missing then fallback", "denied OCI",
		"denied conmon", "default unknown", "append default", "environment effect", "absolute runtime"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			text := "[engine]\nruntime='custom'\nconmon_path=['/selected','/next']\n[engine.runtimes]\ncustom=['/selected','/next']"
			for _, name := range []string{"/selected", "/next", "/usr/bin/custom", "/usr/bin/conmon"} {
				addEngineFile(mem, name, "executable")
				mem.SetExecutableAccess(name, true, nil)
			}
			switch scenario {
			case "special fallback":
				text = "[engine]\nruntime='custom'\nconmon_path=['/absent']\n[engine.runtimes]\ncustom=['/absent']"
				for _, name := range []string{"custom", "conmon"} {
					if err := mem.AddSpecial("/usr/bin/"+name, fs.ModeNamedPipe|0o755); err != nil {
						t.Fatal(err)
					}
					addEngineFile(mem, "/bin/"+name, "executable")
					if err := mem.SetExecutableAccess("/bin/"+name, true, nil); err != nil {
						t.Fatal(err)
					}
				}
			case "directory candidate skipped":
				mem.AddDir("/selected", 0o755)
			case networkTestNonExecutable:
				mem.SetExecutableAccess("/selected", false, nil)
			case "missing then fallback":
				text = "[engine]\nruntime='custom'\nconmon_path=['/absent']\n[engine.runtimes]\ncustom=['/absent']"
			case "denied OCI", "denied conmon":
				mem.AddError("/selected", fs.ErrPermission)
			case "default unknown":
				text = ""
			case "append default":
				text = "[engine]\nconmon_path=['/selected',{append=true}]"
			case "environment effect":
				text = "[engine]\nruntime='/selected'\nconmon_path=['/selected']\nenv=['CONTAINERS_HELPER_BINARY_DIR=/secret']"
			case "absolute runtime":
				text = "[engine]\nruntime='/selected'\nconmon_path=['/selected']"
			}
			addEngineFile(mem, engineSystemPath, text)
			source := runEngine(t, mem, engineProbe(t, testVersion, 1001))
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			for _, p := range podman.ConfiguredHelperProbes(source, nil) {
				obs, _ := p.Run(t.Context(), platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
				x := obs.Executable
				if x.Source != "configuration" || x.SourceID != source.ID || x.RuntimePath != "/usr/bin/podman" {
					t.Fatal("helper selection lost source binding")
				}
				want := "/selected"
				partial := false
				switch scenario {
				case "directory candidate skipped":
					want = "/next"
				case "missing then fallback":
					want = "/usr/bin/conmon"
					if x.Role == "oci_runtime" {
						want = "/usr/bin/custom"
					}
				case "denied OCI", "denied conmon":
					partial, want = true, "/next"
					if x.Role == "oci_runtime" {
						want = ""
					}
				case "special fallback", "default unknown", "append default", "environment effect":
					partial, want = true, ""
				}
				if x.SelectedPath != want || (obs.Completeness == model.Partial) != partial {
					t.Fatalf("role=%s selected=%s complete=%s; want %s partial=%v", x.Role, x.SelectedPath, obs.Completeness, want, partial)
				}
				if scenario == networkTestNonExecutable && (x.Candidates[0].Executable == nil || *x.Candidates[0].Executable) {
					t.Fatal("selection skipped a present non-executable configured helper")
				}
			}
		})
	}
}
