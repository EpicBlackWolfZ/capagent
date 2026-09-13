package podman_test

import (
	"io/fs"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const storageLegacyVersion = "4.9.3"

const (
	storageRelativePath  = "relative"
	storageCancelledCase = "cancelled"
	storageOverlayDriver = "overlay"
	storageFamily        = "storage"
	storageTestRoot      = "/store"
)

func TestStorageSourceReplacementAndRootlessConversion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                string
		uid                 uint32
		user, system        string
		driver, root, mount string
	}{
		{"rootful system replaces vendor", 0, "", "[storage]\ndriver='vfs'", "vfs", "/var/lib/containers/storage", ""},
		{"rootless system subset", 1001, "", "[storage]\ndriver='overlay'\nrootless_storage_path='/srv/$UID/store'\n" +
			"[storage.options]\nmount_program='/system-helper'", storageOverlayDriver, "/srv/1001/store", ""},
		{"rootless user replaces options", 1001, "[storage]\ndriver='vfs'",
			"[storage]\ndriver='overlay'\n[storage.options]\nmount_program='/system-helper'",
			"vfs", "/home/target/.local/share/containers/storage", ""},
		{"rootful XDG baseline fallback",
			0,
			"[storage]\ngraphroot='/xdg-root'",
			"[storage]\ndriver='overlay'",
			storageOverlayDriver,
			"/xdg-root",
			""},
		{"user driver-specific option", 1001, "[storage]\ndriver='overlay'\n[storage.options]\nmount_program='/general'\n" +
			"[storage.options.overlay]\nmount_program='/driver'", "[storage]\ndriver='vfs'",
			storageOverlayDriver, "/home/target/.local/share/containers/storage", "/driver"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			addEngineFile(mem, "/usr/share/containers/storage.conf",
				"[storage]\ndriver='overlay'\n[storage.options]\nmount_program='/vendor-helper'")
			addEngineFile(mem, "/etc/containers/storage.conf", test.system)
			if test.user != "" {
				addEngineFile(mem, "/srv/config/containers/storage.conf", test.user)
			}
			selection := engineProbe(t, testVersion, test.uid)
			var policyErr error
			selection.Environment, policyErr = platform.NewEnvPolicy(nil,
				map[string]string{"XDG_CONFIG_HOME": "/srv/config", "XDG_RUNTIME_DIR": "/run/user/1001"})
			if policyErr != nil {
				t.Fatal(policyErr)
			}
			source := runEngine(t, mem, selection)
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			p := podman.StorageProbe{Selection: selection, Engine: source}
			obs, err := p.Run(t.Context(), platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
			if err != nil || obs.Completeness != model.Complete {
				t.Fatalf("storage incomplete: %v %+v", err, obs.Diagnostics)
			}
			storage := obs.Configuration.Storage
			if storage.Driver == nil || storage.Driver.Value != test.driver || storage.GraphRoot.Value != test.root {
				t.Fatalf("driver/root mismatch: %+v", storage)
			}
			if storage.MountProgram != nil && storage.MountProgram.Value != test.mount || storage.MountProgram == nil && test.mount != "" {
				t.Fatal("wrong effective mount-program configuration", storage.MountProgram)
			}
		})
	}
}

func TestStorageFailureStatesDoNotPromoteFallback(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"malformed storage file", "denied storage file", "unqualified", "engine environment"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			addEngineFile(mem, "/etc/containers/storage.conf", "[storage]\ndriver='vfs'")
			selection := engineProbe(t, testVersion, 1001)
			switch scenario {
			case "malformed storage file":
				addEngineFile(mem, "/etc/containers/storage.conf", "[storage]\ndriver=3")
			case "denied storage file":
				mem.AddError("/etc/containers/storage.conf", fs.ErrPermission)
			case "unqualified":
				selection.Version.Version.Version.Canonical = "9.0.0"
			case "engine environment":
				addEngineFile(mem, engineSystemPath, "[engine]\nenv=['STORAGE_DRIVER=overlay']")
			}
			engine := runEngine(t, mem, selection)
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope())
			obs, _ := (podman.StorageProbe{Selection: selection, Engine: engine}).Run(t.Context(), env)
			if obs.Configuration.ParseComplete && obs.Configuration.SelectionComplete {
				t.Fatal("failed source became selected configuration")
			}
			if (obs.Completeness == model.Complete) != (scenario == "malformed storage file") {
				t.Fatal("syntax error and incomplete collection collapsed")
			}
		})
	}
}

func TestStorageRootfulFallbackDependsOnQualifiedVersion(t *testing.T) {
	t.Parallel()
	for _, version := range []string{storageLegacyVersion, testVersion} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			addEngineFile(mem, "/etc/containers/storage.conf", "[storage]\ndriver='vfs'")
			selection := engineProbe(t, version, 0)
			engine := runEngine(t, mem, selection)
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope())
			obs, _ := (podman.StorageProbe{Selection: selection, Engine: engine}).Run(t.Context(), env)
			storage := obs.Configuration.Storage
			if storage == nil {
				t.Fatal("missing completed selected storage projection")
			}
			if version == storageLegacyVersion {
				if len(storage.Problems) != 2 || storage.RunRoot != nil || storage.GraphRoot != nil {
					t.Fatal("legacy rootful reload fabricated fallback roots")
				}
			} else if len(storage.Problems) != 0 || storage.RunRoot == nil || storage.GraphRoot == nil {
				t.Fatal("modern rootful reload lost fallback roots")
			}
		})
	}
}

func TestStorageImageStoreAndComposefsVersionSemantics(t *testing.T) {
	t.Parallel()
	for _, version := range []string{storageLegacyVersion, testVersion} {
		for _, scenario := range []string{"same image store", "legacy ignored composefs"} {
			t.Run(version+scenario, func(t *testing.T) {
				t.Parallel()
				mem := platform.NewMemPlatformReader()
				text := "[storage]\ndriver='overlay'\ngraphroot='/graph'\nrunroot='/run/store'\n"
				if scenario == "same image store" {
					text += "imagestore='/graph'\n"
				} else {
					text += "[storage.options.overlay]\nuse_composefs=3\n"
				}
				addEngineFile(mem, storageTestSystemPath, text)
				p := engineProbe(t, version, 0)
				engine := runEngine(t, mem, p)
				files := platform.NewScopedMemReader("/", mem)
				defer files.Close()
				obs, _ := (podman.StorageProbe{Selection: p, Engine: engine}).Run(t.Context(),
					platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
				s := obs.Configuration.Storage
				switch {
				case scenario == "same image store":
					if s == nil || len(s.Problems) != 1 || s.Problems[0] != "imagestore_equals_graphroot" {
						t.Fatal("equal storage roots accepted")
					}
				case version == storageLegacyVersion:
					if s == nil || s.UnprojectedFieldCount != 1 {
						t.Fatal("legacy unknown option was validated as a modern field")
					}
				case obs.Configuration.ParseComplete || s != nil:
					t.Fatal("modern invalid recognized field accepted")
				}
			})
		}
	}
}
