package capability

import (
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const storageTestEndpoint = "local"
const storageTestRuntime = "podman"
const storageTestTarget = "target"

const (
	storageVersionID     = "version"
	storageSourceID      = "source"
	storageOtherContext  = "other"
	storageOtherPath     = "/other"
	storageRuntimeSource = "runtime"
	storageTestHelper    = "/helper"
)

func TestStorageConfigurationMappingAndEffectivePrecedence(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"configured overlay",
		"effective vfs",
		"invalid selected file",
		"denied selected file",
		"legacy root required",
		"invalid selected option",
		"wrong version binding",
		"missing selected helper"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			yes := true
			at := time.Unix(1, 0)
			scope := model.EvaluationScope{RunID: "storage", ContextID: storageTestTarget, Runtime: storageTestRuntime,
				Endpoint: storageTestEndpoint}
			version := model.Observation{ID: storageVersionID, ProbeID: storageVersionID, Scope: scope, Timestamp: at, Completeness: model.Complete,
				Version: &model.PodmanVersionObservation{Path: configuredPodmanPath, Runnable: &yes, Version: &model.PodmanVersion{Canonical: "5.8.4"}}}
			c := &model.ConfigurationObservation{Family: "storage",
				RuntimePath:       configuredPodmanPath,
				Profile:           "podman-5.8.4",
				VersionSourceID:   version.ID,
				SelectionComplete: true, ParseComplete: true, Sources: []model.ConfigurationSource{{ID: "selected", Status: "parsed", Selected: true}},
				Storage: &model.StorageConfiguration{Driver: &model.ConfigString{Value: "overlay"},
					MountProgram: &model.ConfigString{Value: storageTestHelper}}}
			source := model.Observation{ID: "storage-config",
				ProbeID:       "storage-config",
				Scope:         scope,
				Timestamp:     at,
				Completeness:  model.Complete,
				Configuration: c}
			helper := model.Observation{ID: "storage-helper", ProbeID: "storage-helper", Scope: scope, Timestamp: at, Completeness: model.Complete,
				Executable: &model.ExecutableObservation{Role: "storage_mount_program",
					Source:       "configuration",
					SourceID:     source.ID,
					RuntimePath:  configuredPodmanPath,
					SelectedPath: storageTestHelper, Candidates: []model.ExecutableCandidate{{Path: storageTestHelper, Present: &yes, Executable: &yes,
						File: &model.ExecutableMetadata{Regular: true, ExecutableBits: true}}}}}
			wanted := map[model.CapabilityID]model.CapabilityState{StorageParsedID: model.StateSupported, StorageOverlayID: model.StateSupported,
				StorageMountProgramID: model.StateSupported}
			var runtime []model.Observation
			switch scenario {
			case "effective vfs":
				driver := "vfs"
				runtime = []model.Observation{{ID: "effective-storage",
					ProbeID:      "effective-storage",
					Scope:        scope,
					Timestamp:    at,
					Completeness: model.Complete,
					Podman:       &model.PodmanInfo{Path: configuredPodmanPath, Available: &yes, StorageDriver: &driver}}}
				wanted[StorageOverlayID] = model.StateUnsupported
			case "invalid selected file":
				c.ParseComplete = false
				c.Sources[0].Status = "invalid"
				wanted[StorageParsedID],
					wanted[StorageOverlayID],
					wanted[StorageMountProgramID] = model.StateMisconfigured,
					model.StateUnknown,
					model.StateUnknown
			case "denied selected file":
				c.ParseComplete = false
				c.Sources[0].Status = "denied"
				source.Completeness = model.Partial
				wanted[StorageParsedID],
					wanted[StorageOverlayID],
					wanted[StorageMountProgramID] = model.StateUnknown,
					model.StateUnknown,
					model.StateUnknown
			case "legacy root required", "invalid selected option":
				c.Storage.Problems = []string{"runroot_required"}
				if scenario == "invalid selected option" {
					c.Storage.Problems = []string{"storage_option_invalid"}
				}
				wanted[StorageParsedID],
					wanted[StorageOverlayID],
					wanted[StorageMountProgramID] = model.StateMisconfigured,
					model.StateUnknown,
					model.StateUnknown
			case "wrong version binding":
				version.Version.Path = storageOtherPath
				wanted[StorageParsedID],
					wanted[StorageOverlayID],
					wanted[StorageMountProgramID] = model.StateUnknown,
					model.StateUnknown,
					model.StateUnknown
			case "missing selected helper":
				helper.Executable.Candidates[0].Present = new(bool)
				wanted[StorageMountProgramID] = model.StateMisconfigured
			}
			observations := append([]model.Observation{version, source, helper}, runtime...)
			for _, definition := range StorageDefinitions() {
				want, ok := wanted[definition.ID]
				if !ok {
					continue
				}
				result := Resolve(scope, definition.ID, at, definition.Evaluate(scope, observations))
				if result.Capability.State != want {
					t.Fatalf("%s=%s want=%s", definition.ID, result.Capability.State, want)
				}
			}
		})
	}
}

func TestStorageRuntimeHelperBindingAndMissingDriver(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"runtime helper",
		"runtime mismatch",
		"runtime remote",
		"incomplete runtime",
		"unknown driver",
		"wrong scope",
		"nonlocal"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			yes := true
			driver := ""
			at := time.Unix(1, 0)
			scope := model.EvaluationScope{RunID: "helper-binding",
				ContextID: storageTestTarget,
				Runtime:   storageTestRuntime,
				Endpoint:  storageTestEndpoint}
			source := model.Observation{ID: storageSourceID, ProbeID: storageSourceID, Scope: scope, Timestamp: at, Completeness: model.Complete,
				Podman: &model.PodmanInfo{Path: configuredPodmanPath, Available: &yes, StorageDriver: &driver, StorageMountProgram: storageTestHelper}}
			helper := model.Observation{ID: "helper", ProbeID: "helper", Scope: scope, Timestamp: at, Completeness: model.Complete,
				Executable: &model.ExecutableObservation{Role: "storage_mount_program", Source: storageRuntimeSource, SourceID: source.ID,
					RuntimePath:  configuredPodmanPath,
					SelectedPath: storageTestHelper,
					Candidates: []model.ExecutableCandidate{{Path: storageTestHelper,
						Present:    &yes,
						Executable: &yes, File: &model.ExecutableMetadata{Regular: true, ExecutableBits: true}}}}}
			want := model.StateUnknown
			switch scenario {
			case "runtime helper":
				want = model.StateSupported
			case "runtime mismatch":
				source.Podman.Path = storageOtherPath
			case "runtime remote":
				source.Podman.ServiceIsRemote = &yes
			case "incomplete runtime":
				source.Completeness = model.Partial
			case "unknown driver":
				helper.Executable = nil
			case "wrong scope":
				source.Scope.ContextID = storageOtherContext
			case "nonlocal":
				scope.Endpoint = "remote"
			}
			observations := []model.Observation{source, helper}
			for _, definition := range StorageDefinitions() {
				expected := model.StateUnknown
				if definition.ID == StorageMountProgramID {
					expected = want
				}
				result := Resolve(scope, definition.ID, at, definition.Evaluate(scope, observations))
				if result.Capability.State != expected {
					t.Fatalf("%s=%s want=%s", definition.ID, result.Capability.State, expected)
				}
			}
		})
	}
}
