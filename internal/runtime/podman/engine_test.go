package podman_test

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const engineSystemPath = "/etc/containers/containers.conf"

func TestEngineSourceProfiles(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"4.9.3", testVersion, "9.0.0", ""} {
		for _, uid := range []uint32{0, 1001} {
			t.Run(version+"/"+strconv.FormatUint(uint64(uid), 10), func(t *testing.T) {
				t.Parallel()
				mem := platform.NewMemPlatformReader()
				paths := []string{"/usr/share/containers/containers.conf", engineSystemPath,
					engineSystemPath + ".d/10-a.conf", engineSystemPath + ".d/90-z.conf",
					"/etc/containers/containers.rootless.conf", "/etc/containers/containers.rootless.conf.d/20-all.conf",
					"/etc/containers/containers.rootless.conf.d/1001/30-target.conf", "/srv/config/containers/containers.conf",
					"/srv/config/containers/containers.conf.d/40-user.conf"}
				for i, name := range paths {
					addEngineFile(mem, name, "[engine]\nconmon_path=['/p"+string(rune('0'+i))+"',{append=true}]\n")
				}
				// Neither vendor drop-ins, modules, another UID, nor nested/other suffixes are selected.
				for _, name := range []string{"/usr/share/containers/containers.conf.d/99.conf",
					"/etc/containers/containers.rootless.conf.d/1002/99.conf", engineSystemPath + ".d/nested/99.conf",
					engineSystemPath + ".d/50.txt", "/srv/config/containers/containers.conf.modules/custom"} {
					addEngineFile(mem, name, "malformed = [")
				}
				p := engineProbe(t, version, uid)
				obs := runEngine(t, mem, p)
				c := obs.Configuration
				if version == "" || version == "9.0.0" {
					if c.SelectionComplete || c.Engine != nil || obs.Completeness != model.Partial {
						t.Fatal("unknown version must not claim source selection", c)
					}
					return
				}
				want := append([]string{}, paths[:4]...)
				if version == testVersion && uid != 0 {
					want = append(want, paths[4:7]...)
				}
				if version == testVersion || uid != 0 {
					want = append(want, paths[7:]...)
				}
				var selected []string
				for _, source := range c.Sources {
					if source.Kind == "file" && source.Selected && source.Status == "parsed" {
						selected = append(selected, source.Path)
						if len(source.SHA256) != 64 || source.ID == "" || source.Engine == nil {
							t.Fatal("missing source identity/projection", source)
						}
					}
				}
				if !reflect.DeepEqual(selected, want) || !c.SelectionComplete || !c.ParseComplete || obs.Completeness != model.Complete {
					t.Fatalf("selected=%v want=%v configuration=%+v", selected, want, c)
				}
				if len(c.Engine.ConmonPath.Values) != len(want) || !c.Engine.ConmonPath.InheritedDefault {
					t.Fatal("ordered merge did not preserve appended unknown default", c.Engine)
				}
			})
		}
	}
}

func TestEngineSourceFailures(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"all sources absent", "denied source", "malformed source", "limit",
		"directory denied", "cancelled configuration", "unbound version"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			p := engineProbe(t, testVersion, 1001)
			switch scenario {
			case "denied source":
				addEngineFile(mem, engineSystemPath, "")
				mem.AddError(engineSystemPath, fs.ErrPermission)
			case "malformed source":
				addEngineFile(mem, engineSystemPath, "password = 'secret")
			case "limit":
				addEngineFile(mem, engineSystemPath, strings.Repeat(" ", 65537))
			case "directory denied":
				addEngineFile(mem, engineSystemPath+".d/one.conf", "")
				mem.AddError(engineSystemPath+".d", fs.ErrPermission)
			case "unbound version":
				p.Version.Scope.ContextID = "other"
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			ctx := t.Context()
			if scenario == "cancelled configuration" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			obs, _ := p.Run(ctx, platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
			if (obs.Completeness == model.Complete) != (scenario == "all sources absent" || scenario == "malformed source") {
				t.Fatal("failure completeness", obs)
			}
			for _, diagnostic := range obs.Diagnostics {
				if strings.Contains(diagnostic.Message, "secret") {
					t.Fatal("raw configuration leaked")
				}
			}
		})
	}
}

func TestEngineLegacySymlinkDirectorySelection(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"4.9.3", testVersion} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			addEngineFile(mem, "/srv/dropins/one.conf", "[engine]\nruntime='custom'")
			addEngineFile(mem, engineSystemPath, "[engine]\nruntime='crun'")
			mem.AddSymlink(engineSystemPath+".d", "/srv/dropins")
			obs := runEngine(t, mem, engineProbe(t, version, 0))
			want := "custom"
			if version == "4.9.3" {
				want = "crun" // filepath.WalkDir does not follow its root symlink.
			}
			if obs.Configuration.Engine.Runtime.Value != want {
				t.Fatal("wrong version-specific directory semantics")
			}
		})
	}
}

func TestEngineUnsafeDropinNameIsRedactedAndIncomplete(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	addEngineFile(mem, engineSystemPath+".d/secret\n.conf", "[engine]\nruntime='crun'")
	obs := runEngine(t, mem, engineProbe(t, testVersion, 1001))
	if obs.Completeness != model.Partial || obs.Configuration.SelectionComplete || obs.Configuration.ParseComplete {
		t.Fatal("uninspectable selected name lost configuration uncertainty")
	}
	found := false
	for _, diagnostic := range obs.Diagnostics {
		if strings.Contains(diagnostic.Message, "secret") {
			t.Fatal("unsafe filename leaked")
		}
		found = found || diagnostic.Code == "config_source_name_unsupported"
	}
	if !found {
		t.Fatal("missing source-selection explanation")
	}
}

func TestEngineSourceBoundsAndPartialReads(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"file count", "total bytes", "partial file", "disappeared legacy file", "disappeared modern file"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			addEngineFile(mem, engineSystemPath, "[engine]\nruntime='crun'")
			version := testVersion
			if scenario == "disappeared legacy file" {
				version = "4.9.3"
			}
			for i := range 70 {
				if scenario == "file count" {
					addEngineFile(mem, engineSystemPath+fmt.Sprintf(".d/%03d.conf", i), "")
				}
			}
			for i := range 20 {
				if scenario == "total bytes" {
					addEngineFile(mem, engineSystemPath+fmt.Sprintf(".d/%03d.conf", i), strings.Repeat(" ", 65536))
				}
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			borrowed := files
			if strings.Contains(scenario, "file") && scenario != "file count" {
				failure := platform.ErrIncomplete
				if strings.HasPrefix(scenario, "disappeared") {
					failure = fs.ErrNotExist
				}
				borrowed = engineReadFailure{ScopedReader: files, failure: failure}
			}
			p := engineProbe(t, version, 0)
			obs, _ := p.Run(t.Context(), platform.NewEnvironment(nil, nil, nil, nil).WithFiles(borrowed).WithScope(versionScope()))
			if (obs.Completeness == model.Complete) != (scenario == "disappeared modern file") {
				t.Fatal("lost incomplete source", obs.Configuration)
			}
			for _, source := range obs.Configuration.Sources {
				if source.Path == engineSystemPath && strings.Contains(scenario, "file") && scenario != "file count" &&
					(source.SHA256 != "" || source.Engine != nil) {
					t.Fatal("partial bytes were parsed or hashed")
				}
			}
		})
	}
}

type engineReadFailure struct {
	platform.ScopedReader
	failure error
}

func (f engineReadFailure) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == strings.TrimPrefix(engineSystemPath, "/") {
		return []byte("[engine]\nruntime='must-not-use'"), f.failure
	}
	return f.ScopedReader.ReadFile(ctx, name)
}

func addEngineFile(mem *platform.MemPlatformReader, name, content string) {
	for dir := path.Dir(name); dir != "/"; dir = path.Dir(dir) {
		mem.AddDir(dir, 0o755)
	}
	mem.AddFile(name, []byte(content), 0o644)
}

func engineProbe(t *testing.T, version string, uid uint32) podman.EngineProbe {
	t.Helper()
	policy, err := platform.NewEnvPolicy(nil, map[string]string{"XDG_CONFIG_HOME": "/srv/config"})
	if err != nil {
		t.Fatal(err)
	}
	p := podman.EngineProbe{Target: model.UserIdentity{UID: uid, HomeDir: "/home/target"}, Environment: policy, Path: "/usr/bin/podman"}
	if version != "" {
		parts, parseErr := podman.ParseVersion([]byte("podman version " + version))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		runnable := true
		p.Version = model.Observation{ID: "podman.version", Scope: versionScope(), Completeness: model.Complete,
			Version: &model.PodmanVersionObservation{Path: p.Path, Runnable: &runnable, Version: &parts}}
	}
	return p
}

func runEngine(t *testing.T, mem *platform.MemPlatformReader, p podman.EngineProbe) model.Observation {
	t.Helper()
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	obs, _ := p.Run(t.Context(), platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
	return obs
}
