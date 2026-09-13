package capability

import (
	"slices"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const networkTestHelperPath = "/helpers/netavark"

func TestConfiguredNetworkHelperBindingAndConflictingEvidence(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"selected", "missing", networkTestDenied, "wrong planned path", "changed parent directories",
		"wrong parent scope", networkTestWrongVersion, "wrong helper scope", "empty plan", "conflicting helpers", "non executable metadata"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			observations := networkMappingObservations()
			scope, at := observations[0].Scope, observations[0].Timestamp
			engine, network, x := observations[1].Configuration.Engine, observations[2].Configuration.Network, observations[3].Executable
			want := model.StateSupported
			switch scenario {
			case "missing":
				x.SelectedPath = ""
				x.Candidates = []model.ExecutableCandidate{{Path: networkTestHelperPath, Present: new(bool)}}
				want = model.StateMisconfigured
			case networkTestDenied:
				observations[3].Completeness = model.Partial
				x.Candidates[0].Executable = nil
				want = model.StateUnknown
			case "wrong planned path":
				network.HelperPaths[netavarkName] = model.ConfigList{Values: []string{"/unrelated"}}
				x.Candidates[0].Path, x.SelectedPath = "/unrelated", "/unrelated"
				want = model.StateUnknown
			case "changed parent directories":
				engine.HelperBinariesDir.Values[0] = "/changed"
				want = model.StateUnknown
			case "wrong parent scope":
				observations[1].Scope.ContextID = storageOtherContext
				want = model.StateUnknown
			case networkTestWrongVersion:
				observations[0].Version.Path = "/other"
				want = model.StateUnknown
			case "wrong helper scope":
				observations[3].Scope.ContextID = storageOtherContext
				want = model.StateUnknown
			case "empty plan":
				engine.HelperBinariesDir.Values = []string{}
				network.HelperPaths[netavarkName] = model.ConfigList{Values: []string{}}
				x.Candidates = []model.ExecutableCandidate{}
				x.SelectedPath = ""
				want = model.StateMisconfigured
			case "conflicting helpers":
				other := observations[3]
				other.ID = "conflicting-helper"
				copy := *x
				copy.SelectedPath = ""
				copy.Candidates = []model.ExecutableCandidate{{Path: networkTestHelperPath, Present: new(bool)}}
				other.Executable = &copy
				observations = append(observations, other)
				want = model.StateUnknown
			case "non executable metadata":
				x.Candidates[0].File.ExecutableBits = false
				want = model.StateMisconfigured
			}
			definitions := []Definition{NetavarkDefinition(), selectedExecutableDefinition(netavarkName)}
			for _, definition := range definitions {
				for range 2 {
					resolved := Resolve(scope, definition.ID, at, definition.Evaluate(scope, observations))
					if resolved.Capability.State != want {
						t.Fatalf("%s=%s want=%s", definition.ID, resolved.Capability.State, want)
					}
					slices.Reverse(observations)
				}
			}
		})
	}
}

func networkMappingObservations() []model.Observation {
	at := time.Unix(1, 0)
	yes := true
	scope := model.EvaluationScope{RunID: "network-helper", ContextID: storageTestTarget,
		Runtime: storageTestRuntime, Endpoint: storageTestEndpoint}
	version := model.Observation{ID: storageVersionID, ProbeID: storageVersionID, Scope: scope, Timestamp: at, Completeness: model.Complete,
		Version: &model.PodmanVersionObservation{Path: configuredPodmanPath, Runnable: &yes,
			Version: &model.PodmanVersion{Canonical: networkTestVersion}}}
	engine := model.Observation{ID: networkTestEngineID, ProbeID: networkTestEngineID, Scope: scope, Timestamp: at,
		Completeness: model.Complete,
		Configuration: &model.ConfigurationObservation{Family: networkTestEngineID, Profile: networkTestProfile,
			RuntimePath:     configuredPodmanPath,
			VersionSourceID: version.ID, SelectionComplete: true, ParseComplete: true,
			Engine: &model.EngineConfiguration{HelperBinariesDir: &model.ConfigList{Values: []string{"/helpers"}}}}}
	network := model.Observation{ID: networkTestID, ProbeID: networkTestID, Scope: scope, Timestamp: at, Completeness: model.Complete,
		Configuration: &model.ConfigurationObservation{Family: networkTestID, Profile: networkTestProfile, RuntimePath: configuredPodmanPath,
			VersionSourceID: version.ID, SelectionComplete: true, ParseComplete: true, Network: &model.NetworkConfiguration{
				EngineSourceID: engine.ID, Strings: map[string]model.ConfigString{networkBackendKey: {Value: netavarkName}},
				HelperPaths: map[string]model.ConfigList{netavarkName: {Values: []string{networkTestHelperPath}}}}}}
	helper := model.Observation{ID: networkTestHelperID, ProbeID: networkTestHelperID, Scope: scope, Timestamp: at,
		Completeness: model.Complete,
		Executable: &model.ExecutableObservation{Role: netavarkName, Source: configurationSourceName, SourceID: network.ID,
			RuntimePath: configuredPodmanPath, SelectedPath: networkTestHelperPath, Candidates: []model.ExecutableCandidate{
				{Path: networkTestHelperPath, Present: &yes, Executable: &yes, File: &model.ExecutableMetadata{Regular: true, ExecutableBits: true}}}}}
	return []model.Observation{version, engine, network, helper}
}
