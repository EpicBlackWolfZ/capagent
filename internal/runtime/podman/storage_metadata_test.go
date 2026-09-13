package podman_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestStoragePathMetadataPreservesAccessAndAbsence(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"writable", "access denied", "traversal denied", "absent path", "wrong type", "read-only mount"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			mem.AddDir("/", 0o755)
			mem.AddDir(storageTestRoot, 0o700)
			if err := mem.SetDirectoryAccess(storageTestRoot, true, scenario != "access denied", nil); err != nil {
				t.Fatal(err)
			}
			if err := mem.SetDirectoryAccess("/", true, true, nil); err != nil {
				t.Fatal(err)
			}
			targetPath := storageTestRoot
			switch scenario {
			case "traversal denied":
				mem.AddError(storageTestRoot, fs.ErrPermission)
			case "absent path":
				targetPath = "/store/missing/target"
			case "wrong type":
				mem.AddFile(storageTestRoot, nil, 0o600)
			}
			files := platform.NewScopedMemReaderWithFilesystems("/", mem, map[string]platform.FilesystemInfo{
				"/": {Type: 0xef53, FlagsKnown: true, ReadOnly: scenario == "read-only mount"}})
			defer files.Close()
			source := model.Observation{ID: "configuration", Scope: versionScope(), Completeness: model.Complete,
				Configuration: &model.ConfigurationObservation{Family: storageFamily,
					RuntimePath:       testPodmanPath,
					SelectionComplete: true,
					ParseComplete:     true,
					Storage: &model.StorageConfiguration{GraphRoot: &model.ConfigString{Value: targetPath},
						RunRoot: &model.ConfigString{Value: storageTestRoot}}}}
			p := podman.StoragePathProbe{Source: source, Writable: true}
			obs, _ := p.Run(t.Context(), platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
			if obs.StoragePaths == nil || len(obs.StoragePaths.Paths) != 2 {
				t.Fatal("lost selected storage paths")
			}
			first := obs.StoragePaths.Paths[0]
			switch scenario {
			case "writable":
				if first.Accessible == nil || !*first.Accessible || first.Filesystem.ReadOnly == nil || *first.Filesystem.ReadOnly {
					t.Fatal("missing access facts")
				}
			case "access denied":
				if first.Accessible == nil || *first.Accessible {
					t.Fatal("kernel denial lost")
				}
			case "traversal denied":
				if first.Present != nil || obs.Completeness != model.Partial {
					t.Fatal("denial fabricated absence")
				}
			case "absent path":
				if first.Present == nil || *first.Present || !first.Ancestor || first.CheckedPath != storageTestRoot {
					t.Fatal("ancestor confused with existing target")
				}
			case "wrong type":
				if first.Directory == nil || *first.Directory {
					t.Fatal("regular file accepted as a storage directory")
				}
			case "read-only mount":
				if first.Filesystem.ReadOnly == nil || !*first.Filesystem.ReadOnly {
					t.Fatal("mount policy lost")
				}
			}
		})
	}
}

func TestStoragePathCancellationWithoutSelectedPathsIsIncomplete(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	source := model.Observation{ID: "empty", Scope: versionScope(), Completeness: model.Complete,
		Configuration: &model.ConfigurationObservation{Family: storageFamily, SelectionComplete: true, ParseComplete: true,
			Storage: &model.StorageConfiguration{}}}
	p := podman.StoragePathProbe{Source: source}
	obs, err := p.Run(ctx, platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
	if !errors.Is(err, context.Canceled) || obs.Completeness != model.Partial {
		t.Fatalf("cancelled empty selection produced %s: %v", obs.Completeness, err)
	}
}

func TestStorageMetadataBoundsAndAuthority(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"additional layers", "path bound", "ancestor bound", "bad path", "missing source", "wrong scope",
		"incomplete source", "runtime source", "remote source", "unavailable runtime", "no runtime roots", "runtime additional"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			yes := true
			mem := platform.NewMemPlatformReader()
			mem.AddDir("/", 0o755)
			mem.AddDir(storageTestRoot, 0o700)
			if err := mem.SetDirectoryAccess(storageTestRoot, false, true, nil); err != nil {
				t.Fatal(err)
			}
			if err := mem.SetDirectoryAccess(storageTestRoot, true, true, nil); err != nil {
				t.Fatal(err)
			}
			files := platform.NewScopedMemReaderWithFilesystems("/", mem, map[string]platform.FilesystemInfo{"/": {Type: 0xef53, FlagsKnown: true}})
			defer files.Close()
			storage := &model.StorageConfiguration{GraphRoot: &model.ConfigString{Value: storageTestRoot},
				RunRoot:               &model.ConfigString{Value: storageTestRoot},
				AdditionalImageStores: &model.ConfigList{Values: []string{storageTestRoot}},
				AdditionalLayerStores: &model.ConfigList{Values: []string{"/store:ref"}}}
			source := model.Observation{ID: "selected", Scope: versionScope(), Completeness: model.Complete,
				Configuration: &model.ConfigurationObservation{Family: storageFamily, RuntimePath: testPodmanPath, SelectionComplete: true,
					ParseComplete: true, Storage: storage}}
			p := podman.StoragePathProbe{Source: source, Writable: true}
			complete := false
			switch scenario {
			case "additional layers":
				p.Writable = false
				complete = true
			case "path bound":
				p.Writable = false
				storage.AdditionalImageStores.Values = []string{strings.Repeat("/store,", podman.MaxStoragePaths) + storageTestRoot}
			case "ancestor bound":
				storage.GraphRoot.Value = "/store/" + strings.Repeat("a/", 40) + "leaf"
			case "bad path":
				storage.GraphRoot.Value = storageRelativePath
			case "missing source":
				p.Source.Configuration = nil
			case "wrong scope":
				p.Source.Scope.ContextID = "other"
			case "incomplete source":
				p.Source.Completeness = model.Partial
			default:
				root := storageTestRoot
				p.Source.Configuration = nil
				p.Source.Podman = &model.PodmanInfo{Path: testPodmanPath, Available: &yes, GraphRoot: &root, RunRoot: &root}
				switch scenario {
				case "runtime source":
					complete = true
				case "remote source":
					p.Source.Podman.ServiceIsRemote = &yes
				case "unavailable runtime":
					p.Source.Podman.Available = nil
				case "no runtime roots":
					p.Source.Podman.RunRoot = nil
				case "runtime additional":
					p.Writable = false
				}
			}
			if len(p.Dependencies()) != 0 {
				t.Fatal("unexpected metadata dependencies")
			}
			obs, err := p.Run(t.Context(), platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
			if err != nil && !errors.Is(err, platform.ErrIncomplete) {
				t.Fatal(err)
			}
			if (obs.Completeness == model.Complete) != complete {
				t.Fatal("wrong completeness", obs.Diagnostics)
			}
			if len(obs.StoragePaths.Paths) > podman.MaxStoragePaths {
				t.Fatal("path metadata exceeded bound")
			}
			if scenario == "additional layers" && (len(obs.StoragePaths.Paths) != 2 || obs.StoragePaths.Paths[1].Path != storageTestRoot) {
				t.Fatal("layer reference suffix was used as filesystem path")
			}
		})
	}
}
