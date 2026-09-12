package capability_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const feature model.CapabilityID = "runtime.podman.netavark"

func scope() model.EvaluationScope {
	return model.EvaluationScope{RunID: "run", ContextID: "user", Runtime: "podman", Endpoint: "local"}
}
func claim(id string, rank model.EvidencePrecedence, state model.CapabilityState) model.Evidence {
	return model.Evidence{ID: id, Source: "fixture", Claim: string(feature), Scope: scope(), State: state, Precedence: rank,
		Confidence: model.ConfidenceVerified, Completeness: model.Complete, Timestamp: time.Unix(1, 0),
		Observations: []model.ObservationRef{{ID: "obs", ProbeID: "probe"}}}
}

func TestScopedPrecedence(t *testing.T) {
	t.Parallel()
	live := claim("live", model.PrecedenceLive, model.StateUnsupported)
	runtime := claim("runtime", model.PrecedenceRuntime, model.StateSupported)
	config := claim("config", model.PrecedenceConfig, model.StateUnsupported)
	knowledge := claim("knowledge", model.PrecedenceKnowledge, model.StateSupported)
	partial := live
	partial.Completeness = model.Partial
	stale := live
	stale.Scope.RunID = "old"
	future := live
	future.Timestamp = time.Unix(3, 0)
	remote := live
	remote.Scope.Endpoint = "remote"
	unrelated := live
	unrelated.Claim = "storage.overlay"
	conflict := runtime
	conflict.ID = "conflict"
	conflict.State = model.StateUnsupported
	tests := []struct {
		name       string
		input      []model.Evidence
		state      model.CapabilityState
		confidence model.ConfidenceLevel
		winner     []string
	}{
		{"live overrides", []model.Evidence{knowledge, runtime, live}, model.StateUnsupported, model.ConfidenceVerified, []string{"live"}},
		{"runtime overrides config", []model.Evidence{config, runtime}, model.StateSupported, model.ConfidenceVerified, []string{"runtime"}},
		{"config ceiling", []model.Evidence{config, knowledge}, model.StateUnsupported, model.ConfidenceDerived, []string{"config"}},
		{"knowledge ceiling", []model.Evidence{knowledge}, model.StateSupported, model.ConfidenceHeuristic, []string{"knowledge"}},
		{"equal conflict", []model.Evidence{conflict, runtime}, model.StateUnknown, model.ConfidenceUnknown, []string{"conflict", "runtime"}},
		{"partial ignored", []model.Evidence{partial, runtime}, model.StateSupported, model.ConfidenceVerified, []string{"runtime"}},
		{"stale ignored", []model.Evidence{stale}, model.StateUnknown, model.ConfidenceUnknown, nil},
		{"future ignored", []model.Evidence{future}, model.StateUnknown, model.ConfidenceUnknown, nil},
		{"remote ignored", []model.Evidence{remote}, model.StateUnknown, model.ConfidenceUnknown, nil},
		{"unrelated ignored", []model.Evidence{unrelated, runtime}, model.StateSupported, model.ConfidenceVerified, []string{"runtime"}},
		{"missing", nil, model.StateUnknown, model.ConfidenceUnknown, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := capability.Resolve(scope(), feature, time.Unix(2, 0), tt.input)
			var winners []string
			for _, ref := range got.Capability.Evidence {
				winners = append(winners, ref.ID)
			}
			if got.Capability.State != tt.state || got.Capability.Confidence != tt.confidence || !reflect.DeepEqual(winners, tt.winner) {
				t.Fatalf("resolution: %+v", got)
			}
			reverse := append([]model.Evidence(nil), tt.input...)
			for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
				reverse[i], reverse[j] = reverse[j], reverse[i]
			}
			if other := capability.Resolve(scope(), feature, time.Unix(2, 0), reverse); !reflect.DeepEqual(got, other) {
				t.Fatal("resolution depends on input order")
			}
		})
	}
}

func dataset() capability.Dataset {
	return capability.Dataset{RunID: "run", At: time.Unix(2, 0), Contexts: []model.EvaluationContext{{ID: "user",
		Identity:   model.IdentityContext{Current: &model.UserIdentity{UID: 1000}, Target: &model.UserIdentity{UID: 1000}},
		Namespaces: []model.Namespace{{Kind: "user", ID: "user:[1]"}}}},
		Observations: []model.Observation{{ID: "obs", ProbeID: "probe", Scope: scope(), Timestamp: time.Unix(1, 0), Completeness: model.Complete,
			Facts: []model.Fact{{ID: "fact", Source: "fixture", Scope: scope(), Timestamp: time.Unix(1, 0), Completeness: model.Complete}}}},
		Evidence: []model.Evidence{claim("ev", model.PrecedenceRuntime, model.StateSupported)}}
}

func TestGraphReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*capability.Dataset)
	}{
		{"missing observation", func(d *capability.Dataset) { d.Observations = nil }},
		{"wrong probe", func(d *capability.Dataset) { d.Evidence[0].Observations[0].ProbeID = "another" }},
		{"missing context", func(d *capability.Dataset) { d.Contexts = nil }},
		{"duplicate context", func(d *capability.Dataset) { d.Contexts = append(d.Contexts, d.Contexts[0]) }},
		{"duplicate observation", func(d *capability.Dataset) { d.Observations = append(d.Observations, d.Observations[0]) }},
		{"duplicate fact", func(d *capability.Dataset) {
			d.Observations[0].Facts = append(d.Observations[0].Facts, d.Observations[0].Facts[0])
		}},
		{"duplicate evidence", func(d *capability.Dataset) { d.Evidence = append(d.Evidence, d.Evidence[0]) }},
		{"different observation scope", func(d *capability.Dataset) { d.Evidence[0].Scope.Endpoint = "remote" }},
		{"missing evidence dependency", func(d *capability.Dataset) { d.Evidence[0].DependsOn = []model.EvidenceRef{{ID: "missing"}} }},
		{"self cycle", func(d *capability.Dataset) { d.Evidence[0].DependsOn = []model.EvidenceRef{{ID: "ev"}} }},
	}
	if err := capability.ValidateDataset(dataset()); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := dataset()
			tt.change(&d)
			if capability.ValidateDataset(d) == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
}

func TestRegistryDependenciesAndOwnership(t *testing.T) {
	t.Parallel()
	evaluate := func(model.EvaluationScope, []model.Observation) []model.Evidence { return nil }
	definitions := []capability.Definition{{ID: feature, Description: "Netavark configured", Evaluate: evaluate}}
	registry, err := capability.NewRegistry(definitions)
	if err != nil {
		t.Fatal(err)
	}
	definitions[0].Description = "mutated"
	metadata := registry.Definitions()
	metadata[0].Description = "changed again"
	if registry.Definitions()[0].Description != "Netavark configured" {
		t.Fatal("registry metadata aliases caller")
	}
	got, err := registry.Evaluate(scope(), dataset())
	if err != nil || len(got.Candidate.Capabilities) != 1 || got.Candidate.Capabilities[0].State != model.StateSupported {
		t.Fatalf("evaluation: %+v %v", got, err)
	}
	for _, defs := range [][]capability.Definition{
		{{ID: feature, Description: "x", Evaluate: evaluate, Dependencies: []model.CapabilityID{feature}}},
		{{ID: feature, Description: "x", Evaluate: evaluate, Dependencies: []model.CapabilityID{"runtime.absent"}}},
		{{ID: feature, Description: "x", Evaluate: evaluate}, {ID: feature, Description: "x", Evaluate: evaluate}},
	} {
		if _, err := capability.NewRegistry(defs); err == nil {
			t.Fatal("invalid registry accepted")
		}
	}
}
