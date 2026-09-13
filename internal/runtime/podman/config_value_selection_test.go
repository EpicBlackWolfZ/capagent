package podman_test

import (
	"slices"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const testStorageVFS = "vfs"

func TestEngineSupersededStringValueDoesNotBlockSelectedConfiguration(t *testing.T) {
	t.Parallel()
	for _, version := range []string{storageLegacyVersion, testVersion} {
		for _, value := range []string{"'synthetic-secret'", "3"} {
			t.Run(version+value, func(t *testing.T) {
				t.Parallel()
				mem := platform.NewMemPlatformReader()
				addEngineFile(mem, "/usr/share/containers/containers.conf", "[engine]\ncgroup_manager="+value)
				addEngineFile(mem, engineSystemPath, "[engine]\ncgroup_manager='systemd'")
				obs := runEngine(t, mem, engineProbe(t, version, 0))
				complete := value != "3"
				if obs.Configuration.ParseComplete != complete {
					t.Fatal("value interpretation preceded source merge")
				}
				if complete && (obs.Configuration.Engine == nil || obs.Configuration.Engine.CgroupManager.Value != "systemd") {
					t.Fatal("later valid engine value was discarded")
				}
			})
		}
	}
}

func TestStorageSupersededOptionValueDoesNotBlockSelection(t *testing.T) {
	t.Parallel()
	for _, version := range []string{storageLegacyVersion, testVersion} {
		for _, scenario := range []string{"user replacement", "rootless conversion", "type failure"} {
			t.Run(version+scenario, func(t *testing.T) {
				t.Parallel()
				mem := platform.NewMemPlatformReader()
				value := "'synthetic-secret'"
				if scenario == "type failure" {
					value = "3"
				}
				addEngineFile(mem, storageTestSystemPath, "[storage]\ndriver='vfs'\n[storage.options.overlay]\nignore_chown_errors="+value)
				if scenario != "rootless conversion" {
					addEngineFile(mem, "/home/target/.config/containers/storage.conf", "[storage]\ndriver='overlay'\n"+
						"[storage.options.overlay]\nignore_chown_errors='false'")
				}
				p := engineProbe(t, version, 1001)
				var err error
				p.Environment, err = platform.NewEnvPolicy(nil, map[string]string{testRuntimeEnv: "/run/user/1001"})
				if err != nil {
					t.Fatal(err)
				}
				engine := runEngine(t, mem, p)
				files := platform.NewScopedMemReader("/", mem)
				defer files.Close()
				obs, _ := (podman.StorageProbe{Selection: p, Engine: engine}).Run(t.Context(),
					platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
				complete := scenario != "type failure"
				if obs.Configuration.ParseComplete != complete || obs.Completeness != model.Complete {
					t.Fatal("storage option interpreted before selection", obs.Diagnostics)
				}
				if complete && obs.Configuration.Storage == nil {
					t.Fatal("lost final selected storage values")
				}
			})
		}
	}
}

func TestStorageSelectedInvalidOptionsAndInactiveDriverFields(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, driver, fields string
		uid                  uint32
		want                 bool
	}{
		{storageOverlayDriver, storageOverlayDriver, "[storage.options.overlay]\nignore_chown_errors='synthetic-secret'", 0, true},
		{"overlay2", "overlay2", "[storage.options.overlay]\nforce_mask='synthetic-secret'", 0, true},
		{testStorageVFS, testStorageVFS, "[storage.options.vfs]\nignore_chown_errors='synthetic-secret'", 0, true},
		{"inactive vfs", storageOverlayDriver, "[storage.options.vfs]\nignore_chown_errors='synthetic-secret'", 0, false},
		{"inactive overlay", testStorageVFS, "[storage.options.overlay]\nignore_chown_errors='synthetic-secret'", 0, false},
		{"other driver", "btrfs", "[storage.options.overlay]\nignore_chown_errors='synthetic-secret'", 0, false},
		{"unselected driver", "", "[storage.options.overlay]\nignore_chown_errors='synthetic-secret'", 0, false},
		{"general option precedes specific", storageOverlayDriver, "[storage.options]\nignore_chown_errors='synthetic-secret'\n" +
			"[storage.options.overlay]\nignore_chown_errors='false'", 0, true},
		{"rootless first inherited option", storageOverlayDriver, "[storage.options]\nignore_chown_errors='synthetic-secret'\n" +
			"[storage.options.overlay]\nignore_chown_errors='false'", 1001, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			addEngineFile(mem, storageTestSystemPath, "[storage]\ndriver='"+test.driver+"'\n"+test.fields)
			p := engineProbe(t, testVersion, test.uid)
			var err error
			p.Environment, err = platform.NewEnvPolicy(nil, map[string]string{testRuntimeEnv: "/run/user/1001"})
			if err != nil {
				t.Fatal(err)
			}
			engine := runEngine(t, mem, p)
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			obs, err := (podman.StorageProbe{Selection: p, Engine: engine}).Run(t.Context(),
				platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
			if err != nil || obs.Configuration.Storage == nil || !obs.Configuration.ParseComplete {
				t.Fatal("selected value was rejected before interpretation", err)
			}
			if slices.Contains(obs.Configuration.Storage.Problems, "storage_option_invalid") != test.want {
				t.Fatal("invalid option did not follow selected driver and rootless subset")
			}
		})
	}
}

func TestEngineDropinReplacesInvalidSelectedValue(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	addEngineFile(mem, engineSystemPath, "[engine]\ncgroup_manager='synthetic-secret'")
	addEngineFile(mem, engineSystemPath+".d/90-valid.conf", "[engine]\ncgroup_manager='systemd'")
	obs := runEngine(t, mem, engineProbe(t, testVersion, 1001))
	value := obs.Configuration.Engine.CgroupManager
	if !obs.Configuration.ParseComplete || value.Invalid || value.Value != "systemd" {
		t.Fatal("drop-in failed to replace invalid value")
	}
}

func TestInvalidSelectedEngineValuePreventsStorageSelection(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	addEngineFile(mem, engineSystemPath, "[engine]\ncgroup_manager='synthetic-secret'")
	p := engineProbe(t, testVersion, 0)
	engine := runEngine(t, mem, p)
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	obs, _ := (podman.StorageProbe{Selection: p, Engine: engine}).Run(t.Context(),
		platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
	if obs.Configuration.SelectionComplete || obs.Configuration.Storage != nil || obs.Completeness != model.Partial {
		t.Fatal("invalid engine supplied qualified storage")
	}
}
