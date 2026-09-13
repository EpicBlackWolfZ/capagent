package capability

import (
	"strings"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestStoragePathMappingsRequireSelectedCompletePaths(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"accessible roots",
		"missing target",
		"denied access",
		"read-only filesystem",
		"wrong selected path",
		"wrong target scope",
		"missing record"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			yes, no := true, false
			at := time.Unix(1, 0)
			scope := model.EvaluationScope{RunID: "storage-paths", ContextID: storageTestTarget, Runtime: storageTestRuntime,
				Endpoint: storageTestEndpoint}
			root, run := "/graph", "/run"
			source := model.Observation{ID: "reported", ProbeID: "reported", Scope: scope, Timestamp: at, Completeness: model.Complete,
				Podman: &model.PodmanInfo{Path: configuredPodmanPath, Available: &yes, GraphRoot: &root, RunRoot: &run}}
			paths := model.StoragePathObservation{Source: storageRuntimeSource,
				SourceID:    source.ID,
				RuntimePath: configuredPodmanPath,
				Writable:    true,
				Paths: []model.StoragePathMetadata{
					{Role: "graphroot",
						Path:        root,
						CheckedPath: root,
						Present:     &yes,
						Directory:   &yes,
						Accessible:  &yes,
						Filesystem:  &model.StorageFilesystem{ReadOnly: &no}},
					{Role: "runroot",
						Path:        run,
						CheckedPath: run,
						Present:     &yes,
						Directory:   &yes,
						Accessible:  &yes,
						Filesystem:  &model.StorageFilesystem{ReadOnly: &no}},
				}}
			measured := model.Observation{ID: "metadata",
				ProbeID:      "metadata",
				Scope:        scope,
				Timestamp:    at,
				Completeness: model.Complete,
				StoragePaths: &paths}
			want := model.StateSupported
			switch scenario {
			case "missing target":
				paths.Paths[0].Present = &no
				paths.Paths[0].Ancestor = true
				want = model.StateUnknown
			case "denied access":
				paths.Paths[0].Accessible = &no
				want = model.StateMisconfigured
			case "read-only filesystem":
				paths.Paths[0].Filesystem.ReadOnly = &yes
				want = model.StateMisconfigured
			case "wrong selected path":
				paths.Paths[0].Path = storageOtherPath
				want = model.StateUnknown
			case "wrong target scope":
				measured.Scope.ContextID = storageOtherContext
				want = model.StateUnknown
			case "missing record":
				paths.Paths = paths.Paths[:1]
				want = model.StateUnknown
			}
			for _, definition := range StorageDefinitions() {
				if definition.ID != StorageRootsID {
					continue
				}
				result := Resolve(scope, definition.ID, at, definition.Evaluate(scope, []model.Observation{source, measured}))
				if result.Capability.State != want {
					t.Fatalf("state=%s want=%s", result.Capability.State, want)
				}
			}
		})
	}
}

func TestStorageAdditionalSelectionHasBoundedEvidence(t *testing.T) {
	t.Parallel()
	const beyondBound = 33
	source := model.Observation{Configuration: &model.ConfigurationObservation{Family: "storage", Profile: "podman-5.8.4",
		SelectionComplete: true, ParseComplete: true, Storage: &model.StorageConfiguration{
			AdditionalImageStores: &model.ConfigList{Values: []string{strings.Repeat("/images,", beyondBound) + "/images"}},
		}}}
	paths, known := expectedStoragePaths(source, StorageAdditionalID)
	if known || len(paths) != 0 {
		t.Fatal("over-budget selected paths must remain unknown")
	}
}

func TestStoragePathEvidenceDoesNotCrossSelectionOrProofLevels(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"optional unselected", "additional selected", "image selected", "missing additional", "ancestor only",
		"unknown presence", "unknown type", "wrong type", "unknown access", "unknown mount policy", "denied followed by unknown",
		"wrong path source", "wrong runtime binding", "incomplete configuration", networkTestWrongVersion, "nonlocal"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			yes, no := true, false
			at := time.Unix(1, 0)
			scope := model.EvaluationScope{RunID: "path-selection",
				ContextID: storageTestTarget,
				Runtime:   storageTestRuntime,
				Endpoint:  storageTestEndpoint}
			version := model.Observation{ID: storageVersionID, ProbeID: storageVersionID, Scope: scope, Timestamp: at, Completeness: model.Complete,
				Version: &model.PodmanVersionObservation{Path: configuredPodmanPath, Runnable: &yes,
					Version: &model.PodmanVersion{Canonical: networkTestVersion}}}
			c := &model.ConfigurationObservation{Family: "storage", Profile: "podman-5.8.4", RuntimePath: configuredPodmanPath,
				VersionSourceID: version.ID, SelectionComplete: true, ParseComplete: true, Storage: &model.StorageConfiguration{
					AdditionalImageStores: &model.ConfigList{Values: []string{"/images"}},
					AdditionalLayerStores: &model.ConfigList{Values: []string{"/layers:ref"}}}}
			source := model.Observation{ID: "selection",
				ProbeID:       "selection",
				Scope:         scope,
				Timestamp:     at,
				Completeness:  model.Complete,
				Configuration: c}
			paths := &model.StoragePathObservation{Source: configurationSourceName, SourceID: source.ID, RuntimePath: configuredPodmanPath,
				Paths: []model.StoragePathMetadata{
					{Role: "additionalimagestore", Path: "/images", CheckedPath: "/images", Present: &yes, Directory: &yes, Accessible: &yes},
					{Role: "additionallayerstore", Path: "/layers", CheckedPath: "/layers", Present: &yes, Directory: &yes, Accessible: &yes}}}
			measured := model.Observation{ID: "paths",
				ProbeID:      "paths",
				Scope:        scope,
				Timestamp:    at,
				Completeness: model.Complete,
				StoragePaths: paths}
			id, want := StorageAdditionalID, model.StateUnknown
			switch scenario {
			case "optional unselected":
				c.Storage.AdditionalImageStores = nil
				c.Storage.AdditionalLayerStores = nil
				want = model.StateSupported
			case "additional selected":
				want = model.StateSupported
			case "image selected", "unknown mount policy":
				id = StorageImageStoreID
				c.Storage.ImageStore = &model.ConfigString{Value: "/images"}
				paths.Writable = true
				paths.Paths = paths.Paths[:1]
				paths.Paths[0].Role = "imagestore"
				if scenario == "image selected" {
					paths.Paths[0].Filesystem = &model.StorageFilesystem{ReadOnly: &no}
					want = model.StateSupported
				}
			case "missing additional":
				paths.Paths[0].Present = &no
				want = model.StateMisconfigured
			case "ancestor only":
				paths.Paths[0].Ancestor = true
			case "unknown presence":
				paths.Paths[0].Present = nil
			case "unknown type":
				paths.Paths[0].Directory = nil
			case "wrong type":
				paths.Paths[0].Directory = &no
				want = model.StateMisconfigured
			case "unknown access":
				paths.Paths[0].Accessible = nil
			case "denied followed by unknown":
				paths.Paths[0].Accessible = &no
				paths.Paths[1].Accessible = nil
				want = model.StateMisconfigured
			case "wrong path source":
				paths.Source = storageRuntimeSource
			case "wrong runtime binding":
				paths.RuntimePath = storageOtherPath
			case "incomplete configuration":
				c.ParseComplete = false
				source.Completeness = model.Partial
			case networkTestWrongVersion:
				version.Version.Path = storageOtherPath
			case "nonlocal":
				scope.Endpoint = "remote"
			}
			evidence := storagePathEvidence(scope, []model.Observation{version, source, measured}, id)
			result := Resolve(scope, id, at, evidence)
			if result.Capability.State != want {
				t.Fatalf("%s want %s", result.Capability.State, want)
			}
		})
	}
}
