package capability_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestGraphMetadataAndFreshnessBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*capability.Dataset)
	}{
		{"run", func(d *capability.Dataset) { d.RunID = "" }},
		{"graph limit", func(d *capability.Dataset) {
			d.Contexts = make([]model.EvaluationContext, capability.MaxGraphEntries+1)
		}},
		{"namespace kind", func(d *capability.Dataset) { d.Contexts[0].Namespaces[0].Kind = "" }},
		{"namespace ID", func(d *capability.Dataset) { d.Contexts[0].Namespaces[0].ID = "" }},
		{"duplicate namespace", func(d *capability.Dataset) {
			d.Contexts[0].Namespaces = append(d.Contexts[0].Namespaces, d.Contexts[0].Namespaces[0])
		}},
		{"observation metadata", func(d *capability.Dataset) { d.Observations[0].Timestamp = time.Time{} }},
		{"no facts", func(d *capability.Dataset) { d.Observations[0].Facts = nil }},
		{"partial fact", func(d *capability.Dataset) { d.Observations[0].Facts[0].Completeness = model.Partial }},
		{"future fact", func(d *capability.Dataset) { d.Observations[0].Facts[0].Timestamp = d.At }},
		{"future observation", func(d *capability.Dataset) { d.Observations[0].Timestamp = d.At }},
		{"invalid evidence", func(d *capability.Dataset) { d.Evidence[0].Confidence = "invalid" }},
		{"missing evidence context", func(d *capability.Dataset) { d.Evidence[0].Scope.ContextID = "missing" }},
		{"no observation references", func(d *capability.Dataset) { d.Evidence[0].Observations = nil }},
		{"duplicate observation references", func(d *capability.Dataset) {
			d.Evidence[0].Observations = append(d.Evidence[0].Observations, d.Evidence[0].Observations[0])
		}},
		{"partial observation", func(d *capability.Dataset) { d.Observations[0].Completeness = model.Partial }},
		{"reference limit", func(d *capability.Dataset) {
			d.Evidence[0].Observations = make([]model.ObservationRef, capability.MaxGraphReferences+1)
		}},
		{"fact limit", func(d *capability.Dataset) {
			fact := d.Observations[0].Facts[0]
			d.Observations[0].Facts = nil
			for i := range capability.MaxGraphEntries + 1 {
				fact.ID = fmt.Sprint(i)
				d.Observations[0].Facts = append(d.Observations[0].Facts, fact)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := dataset()
			tt.change(&d)
			if err := capability.ValidateDataset(d); err == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
}

func TestResolverValidationAndExplanation(t *testing.T) {
	t.Parallel()
	now := time.Unix(2, 0)
	invalid := claim("invalid", model.PrecedenceLive, model.StateSupported)
	invalid.Confidence = "invalid"
	if got := capability.Resolve(scope(), feature, now, []model.Evidence{invalid}); len(got.Diagnostics) != 1 {
		t.Fatal(got)
	}
	if got := capability.Resolve(model.EvaluationScope{}, feature, now, nil); len(got.Diagnostics) != 1 {
		t.Fatal(got)
	}
	live := claim("live", model.PrecedenceLive, model.StateSupported)
	weaker := claim("weaker", model.PrecedenceHeuristic, model.StateUnsupported)
	equal := live
	equal.ID = "equal"
	equal.Confidence = model.ConfidenceDerived
	got := capability.Resolve(scope(), feature, now, []model.Evidence{weaker, live, equal})
	if got.Capability.Confidence != model.ConfidenceDerived || len(got.Capability.Evidence) != 2 || len(got.Superseded) != 1 ||
		got.Superseded[0].ID != "weaker" {
		t.Fatal("lost precedence explanation", got)
	}
}

func TestEvidenceDependencyCompletenessAndTime(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"partial dependency", "future dependency"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			d := dataset()
			dependency := d.Evidence[0]
			dependency.ID = "dependency"
			if kind == "partial dependency" {
				dependency.Completeness = model.Partial
			} else {
				dependency.Timestamp = d.At
			}
			d.Evidence[0].DependsOn = []model.EvidenceRef{{ID: dependency.ID}}
			d.Evidence = append(d.Evidence, dependency)
			if err := capability.ValidateDataset(d); err == nil {
				t.Fatal("claim upgraded an incomplete or future dependency")
			}
		})
	}
}

func TestRegistryPrerequisitesAndInvalidEvaluators(t *testing.T) {
	t.Parallel()
	noop := func(model.EvaluationScope, []model.Observation) []model.Evidence { return nil }
	dependency := model.CapabilityID("runtime.prerequisite")
	definitions := []capability.Definition{
		{ID: feature, Description: "dependent", Dependencies: []model.CapabilityID{dependency}, Evaluate: noop},
		{ID: dependency, Description: "prerequisite", Evaluate: noop},
	}
	registry, err := capability.NewRegistry(definitions)
	if err != nil {
		t.Fatal(err)
	}
	definitions[0].Dependencies[0] = "runtime.mutated"
	metadata := registry.Definitions()
	metadata[1].Dependencies[0] = "runtime.mutated"
	if registry.Definitions()[1].Dependencies[0] != dependency {
		t.Fatal("dependency metadata aliases caller")
	}
	d := dataset()
	d.Evidence = nil
	got, err := registry.Evaluate(scope(), d)
	if err != nil || got.Candidate.Capabilities[1].State != model.StateUnknown || len(got.Diagnostics) == 0 {
		t.Fatal(got, err)
	}
	// A fully supported prerequisite unlocks the dependent evaluator.
	ev := claim("prerequisite", model.PrecedenceRuntime, model.StateSupported)
	ev.Claim = string(dependency)
	d.Evidence = []model.Evidence{ev}
	if _, err := registry.Evaluate(scope(), d); err != nil {
		t.Fatal(err)
	}
	d.Contexts[0].Identity.Target = nil
	if got, err := registry.Evaluate(scope(), d); err != nil || got.Candidate.Capabilities[0].State != model.StateUnknown {
		t.Fatal(got, err)
	}
	for _, change := range []func(*capability.Dataset){func(d *capability.Dataset) { d.RunID = "wrong" },
		func(d *capability.Dataset) { d.At = time.Time{} },
		func(d *capability.Dataset) { d.Contexts = nil; d.Observations = nil; d.Evidence = nil },
	} {
		d := dataset()
		change(&d)
		if _, err := registry.Evaluate(scope(), d); err == nil {
			t.Fatal("invalid evaluation accepted")
		}
	}
	for _, kind := range []string{"invalid definition", "wrong claim", "wrong scope", "missing reference"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			def := capability.Definition{ID: feature, Description: "test",
				Evaluate: func(model.EvaluationScope, []model.Observation) []model.Evidence {
					ev := claim("generated", model.PrecedenceRuntime, model.StateSupported)
					switch kind {
					case "wrong claim":
						ev.Claim = "runtime.other"
					case "wrong scope":
						ev.Scope.Endpoint = "remote"
					case "missing reference":
						ev.Observations = nil
					}
					return []model.Evidence{ev}
				}}
			if kind == "invalid definition" {
				def.Evaluate = nil
			}
			reg, err := capability.NewRegistry([]capability.Definition{def})
			if err == nil {
				_, err = reg.Evaluate(scope(), dataset())
			}
			if err == nil {
				t.Fatal("invalid evaluator accepted")
			}
		})
	}
}
