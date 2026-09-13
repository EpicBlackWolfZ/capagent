package capability

import (
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const configuredPodmanPath = "/podman"
const configuredRuntimePath = "/configured"

const (
	configuredFirstPath = "/first"
	configuredOCIRole   = "oci_runtime"
)

func TestEngineConfigurationEvidence(t *testing.T) {
	t.Parallel()
	yes := true
	at := time.Unix(1, 0)
	scope := model.EvaluationScope{RunID: networkTestEngineID, ContextID: storageTestTarget, Runtime: storageTestRuntime,
		Endpoint: storageTestEndpoint}
	for _, scenario := range []string{"configured", "invalid selected value", "malformed source", "denied source",
		"unqualified profile", "wrong version target",
		"wrong runtime path", "runtime overrides configuration", "helper missing", "helper wrong source"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			version := model.Observation{ID: storageVersionID, ProbeID: storageVersionID, Scope: scope, Timestamp: at, Completeness: model.Complete,
				Version: &model.PodmanVersionObservation{Path: configuredPodmanPath, Runnable: &yes,
					Version: &model.PodmanVersion{Canonical: networkTestVersion}}}
			configuration := &model.ConfigurationObservation{Family: networkTestEngineID, RuntimePath: configuredPodmanPath, Profile: "podman-5.8.4",
				VersionSourceID: version.ID, SelectionComplete: true, ParseComplete: true,
				Sources: []model.ConfigurationSource{{ID: "source", Selected: true, Applied: true, Status: "parsed"}},
				Engine: &model.EngineConfiguration{Runtime: &model.ConfigString{Value: configuredRuntimePath, SourceID: "source"},
					CgroupManager: &model.ConfigString{Value: "systemd", SourceID: "source"}}}
			source := model.Observation{ID: networkTestEngineID, ProbeID: networkTestEngineID, Scope: scope, Timestamp: at,
				Completeness:  model.Complete,
				Configuration: configuration}
			helper := model.Observation{ID: networkTestHelperID, ProbeID: networkTestHelperID, Scope: scope, Timestamp: at,
				Completeness: model.Complete,
				Executable: &model.ExecutableObservation{Source: configurationSourceName, Role: configuredOCIRole, SourceID: source.ID,
					RuntimePath: configuredPodmanPath, SelectedPath: configuredRuntimePath, Candidates: []model.ExecutableCandidate{
						{Path: configuredRuntimePath, Present: &yes, Executable: &yes,
							File: &model.ExecutableMetadata{Regular: true, ExecutableBits: true}}}}}
			wantParsed, wantMode, wantHelper := model.StateSupported, model.StateSupported, model.StateSupported
			switch scenario {
			case "invalid selected value":
				configuration.Engine.CgroupManager = &model.ConfigString{Invalid: true, SourceID: "source"}
				wantParsed, wantMode, wantHelper = model.StateMisconfigured, model.StateUnknown, model.StateUnknown
			case "malformed source":
				configuration.Sources[0].Status, configuration.ParseComplete = "malformed", false
				wantParsed, wantMode, wantHelper = model.StateMisconfigured, model.StateUnknown, model.StateUnknown
			case "denied source":
				configuration.Sources[0].Status, configuration.ParseComplete = networkTestDenied, false
				source.Completeness = model.Partial
				wantParsed, wantMode, wantHelper = model.StateUnknown, model.StateUnknown, model.StateUnknown
			case "unqualified profile":
				configuration.SelectionComplete, configuration.Profile = false, "unqualified"
				wantParsed, wantMode, wantHelper = model.StateUnknown, model.StateUnknown, model.StateUnknown
			case "wrong version target":
				version.Scope.ContextID = storageOtherContext
				wantParsed, wantMode, wantHelper = model.StateUnknown, model.StateUnknown, model.StateUnknown
			case "wrong runtime path":
				version.Version.Path = "/other"
				wantParsed, wantMode, wantHelper = model.StateUnknown, model.StateUnknown, model.StateUnknown
			case "helper missing":
				helper.Executable.SelectedPath = ""
				helper.Executable.Candidates = []model.ExecutableCandidate{
					{Path: configuredRuntimePath, Present: new(bool)}, {Path: configuredRuntimePath, Present: new(bool)}}
				wantHelper = model.StateMisconfigured
			case "helper wrong source":
				helper.Executable.SourceID = storageOtherContext
				wantHelper = model.StateUnknown
			}
			observations := []model.Observation{version, source, helper}
			if scenario == "runtime overrides configuration" {
				mode := "cgroupfs"
				info := model.Observation{ID: "engine-runtime-info", ProbeID: "engine-runtime-info",
					Scope: scope, Timestamp: at, Completeness: model.Complete,
					Podman: &model.PodmanInfo{Path: configuredPodmanPath, Available: &yes, CgroupManager: &mode,
						OCIRuntime: &model.SelectedOCIRuntime{Path: "/effective"}}}
				selected := helper
				selected.ID, selected.ProbeID = "effective", "effective"
				selected.Executable = &model.ExecutableObservation{Source: "runtime", Role: configuredOCIRole, SourceID: info.ID,
					RuntimePath: configuredPodmanPath, SelectedPath: "/effective",
					Candidates: []model.ExecutableCandidate{{Path: "/effective", Present: new(bool)}}}
				observations = append(observations, info, selected)
				wantMode, wantHelper = model.StateUnsupported, model.StateMisconfigured
			}
			definitions := append(EngineDefinitions(), selectedExecutableDefinition(configuredOCIRole))
			for i, want := range []model.CapabilityState{wantParsed, wantMode, wantHelper} {
				d := definitions[i]
				resolution := Resolve(scope, d.ID, at, d.Evaluate(scope, observations))
				if resolution.Capability.State != want {
					t.Fatalf("%s = %s; want %s", d.ID, resolution.Capability.State, want)
				}
			}
		})
	}
}

func TestConfiguredHelperEvidenceRequiresDeclaredCompletePathSequence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		role     string
		paths    []string
		selected string
		want     bool
	}{
		{"named runtime", configuredOCIRole, []string{configuredFirstPath, "/usr/bin/custom"}, "/usr/bin/custom", true},
		{"conmon fallback", conmonName, []string{configuredFirstPath, "/usr/bin/conmon", "/bin/conmon"}, "", true},
		{"missing tail is unknown", conmonName, []string{configuredFirstPath}, "", false},
		{"wrong candidate", configuredOCIRole, []string{"/different"}, "/different", false},
		{"wrong selection", configuredOCIRole, []string{configuredFirstPath}, "/different", false},
		{"extra candidates", configuredOCIRole, []string{configuredFirstPath, "/usr/bin/custom", "/bin/custom", "/extra"}, "", false},
		{"unknown role", "different", []string{configuredFirstPath}, configuredFirstPath, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			engine := &model.EngineConfiguration{Runtime: &model.ConfigString{Value: "custom"},
				Runtimes:   map[string]model.ConfigList{"custom": {Values: []string{configuredFirstPath}}},
				ConmonPath: &model.ConfigList{Values: []string{configuredFirstPath}}}
			x := &model.ExecutableObservation{Role: test.role, SelectedPath: test.selected}
			for _, name := range test.paths {
				x.Candidates = append(x.Candidates, model.ExecutableCandidate{Path: name})
			}
			if configuredPathsMatch(engine, x) != test.want {
				t.Fatal("configuration source binding accepted an incomplete or unrelated path sequence")
			}
		})
	}
}

func TestConfiguredHelperDefaultsAndUnsupportedEffectsRemainUnknown(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		engine model.EngineConfiguration
		role   string
	}{
		{"missing runtime", model.EngineConfiguration{}, configuredOCIRole},
		{"unmeasured runtime map", model.EngineConfiguration{Runtime: &model.ConfigString{Value: "crun"}}, configuredOCIRole},
		{"inherited runtime paths", model.EngineConfiguration{Runtime: &model.ConfigString{Value: "custom"},
			Runtimes: map[string]model.ConfigList{"custom": {InheritedDefault: true}}}, configuredOCIRole},
		{"missing conmon", model.EngineConfiguration{}, conmonName},
		{"inherited conmon", model.EngineConfiguration{ConmonPath: &model.ConfigList{InheritedDefault: true}}, conmonName},
		{"engine environment", model.EngineConfiguration{Environment: &model.ConfigRedactedList{Count: 1}}, conmonName},
		{"platform override", model.EngineConfiguration{SelectionOverrides: true}, configuredOCIRole},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			x := &model.ExecutableObservation{Role: test.role, Candidates: []model.ExecutableCandidate{{Path: configuredFirstPath}}}
			if configuredPathsMatch(&test.engine, x) {
				t.Fatal("unmeasured default or unmodeled effect became selection evidence")
			}
		})
	}
	for _, d := range EngineDefinitions() {
		if len(d.Evaluate(model.EvaluationScope{Runtime: "docker", Endpoint: "remote"}, nil)) != 0 {
			t.Fatal("local Podman definition evaluated outside its runtime/endpoint")
		}
	}
	if cgroupManagerState("") != model.StateUnknown {
		t.Fatal("missing manager choice is not a negative observation")
	}
}
