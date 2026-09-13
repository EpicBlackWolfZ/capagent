package podman_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const storageTestSystemPath = "/etc/containers/storage.conf"

func TestStorageIncompleteSourcesRetainUnknownAndRedactPartialBytes(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{storageCancelledCase, "environment error", "invalid environment", "no filesystem", "syntax", "limit",
		"unsupported", "partial read", "denied read", "missing runtime directory", "parent traversal"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			addEngineFile(mem, storageTestSystemPath, "[storage]\ndriver='vfs'")
			p := engineProbe(t, testVersion, 0)
			switch scenario {
			case "syntax":
				addEngineFile(mem, storageTestSystemPath, "secret = '")
			case "limit":
				addEngineFile(mem, storageTestSystemPath, strings.Repeat(" ", 65537))
			case "unsupported":
				addEngineFile(mem, storageTestSystemPath, "[storage]\ngraphroot='relative'")
			case "missing runtime directory":
				p.Target.UID = 1001
			case "parent traversal":
				addEngineFile(mem, storageTestSystemPath, "[storage]\ngraphroot='/a/../b'")
			}
			engine := runEngine(t, mem, p)
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			borrowed := files
			switch scenario {
			case "environment error":
				p.EnvironmentError = platform.ErrIncomplete
			case "invalid environment":
				p.Target.HomeDir = storageRelativePath
			case "partial read":
				borrowed = storageReadFailure{ScopedReader: files, failure: platform.ErrIncomplete}
			case "denied read":
				borrowed = storageReadFailure{ScopedReader: files, failure: fs.ErrPermission}
			case "no filesystem":
				borrowed = nil
			}
			ctx := t.Context()
			if scenario == storageCancelledCase {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			probe := podman.StorageProbe{Selection: p, Engine: engine}
			if len(probe.Dependencies()) != 0 {
				t.Fatal("configuration unexpectedly scheduled before sources")
			}
			obs, _ := probe.Run(ctx, platform.NewEnvironment(nil, nil, nil, nil).WithFiles(borrowed).WithScope(versionScope()))
			if obs.Configuration.ParseComplete || obs.Configuration.Storage != nil {
				t.Fatal("failed selection supplied complete storage")
			}
			if (obs.Completeness == model.Complete) != (scenario == "syntax") {
				t.Fatal("malformed and incomplete errors collapsed", obs.Completeness)
			}
			for _, source := range obs.Configuration.Sources {
				if source.SHA256 != "" && (scenario == "partial read" || scenario == "denied read") {
					t.Fatal("partial bytes hashed")
				}
				if strings.Contains(source.Problem, "secret") {
					t.Fatal("unsafe source diagnostic")
				}
			}
		})
	}
}

type storageReadFailure struct {
	platform.ScopedReader
	failure error
}

func (f storageReadFailure) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == strings.TrimPrefix(storageTestSystemPath, "/") {
		return []byte("[storage]\ndriver='secret'"), f.failure
	}
	return f.ScopedReader.ReadFile(ctx, name)
}

func TestRootlessStorageInheritsOnlyQualifiedSubset(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, config, driver, ignore string }{
		{"overlay subset",
			"[storage]\ndriver='overlay2'\n[storage.options]\nignore_chown_errors='true'\nmount_program='/discarded'",
			storageOverlayDriver,
			"true"},
		{"driver-specific subset",
			"[storage]\ndriver='overlay'\n[storage.options.overlay]\nignore_chown_errors='false'",
			storageOverlayDriver,
			"false"},
		{"non-rootless baseline", "[storage]\ndriver='zfs'\ndriver_priority=['vfs']", "", ""},
	} {
		for _, version := range []string{storageLegacyVersion, testVersion} {
			t.Run(test.name+version, func(t *testing.T) {
				t.Parallel()
				mem := platform.NewMemPlatformReader()
				addEngineFile(mem, storageTestSystemPath, test.config)
				p := engineProbe(t, version, 1001)
				var err error
				p.Environment, err = platform.NewEnvPolicy(nil, map[string]string{"XDG_DATA_HOME": "/data", "XDG_RUNTIME_DIR": "/runtime"})
				if err != nil {
					t.Fatal(err)
				}
				engine := runEngine(t, mem, p)
				files := platform.NewScopedMemReader("/", mem)
				defer files.Close()
				obs, err := (podman.StorageProbe{Selection: p, Engine: engine}).Run(t.Context(),
					platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
				s := obs.Configuration.Storage
				if err != nil || s == nil || s.GraphRoot.Value != "/data/containers/storage" {
					t.Fatal("rootless defaults lost", err)
				}
				if s.Driver != nil && s.Driver.Value != test.driver || s.Driver == nil && test.driver != "" {
					t.Fatal("inherited unsafe driver")
				}
				if s.MountProgram != nil || s.Options["overlay.ignore_chown_errors"].Value != test.ignore {
					t.Fatal("inherited wrong option subset")
				}
				if test.driver == "" && (s.DriverPriority == nil || s.DriverPriority.Values[0] != "vfs") {
					t.Fatal("lost configured auto-selection priority")
				}
			})
		}
	}
}
