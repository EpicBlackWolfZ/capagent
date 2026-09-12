package requirement_test

import (
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

const featureID model.CapabilityID = "runtime.podman.netavark"

func candidate(state model.CapabilityState) model.Candidate {
	scope := model.EvaluationScope{RunID: "run", ContextID: "user", Runtime: "podman", Endpoint: "local"}
	return model.Candidate{Scope: scope, Capabilities: []model.Capability{{ID: featureID, Scope: scope, State: state,
		Confidence: model.ConfidenceDerived, Evidence: []model.EvidenceRef{{ID: "evidence"}}}},
		Evidence: []model.Evidence{{ID: "evidence", Source: "fixture", Claim: string(featureID), Scope: scope, State: state,
			Confidence: model.ConfidenceDerived, Completeness: model.Complete, Precedence: model.PrecedenceRuntime, Timestamp: time.Unix(1, 0)}}}
}

func TestPredicateStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state model.CapabilityState
		want  requirement.RequirementState
	}{
		{model.StateSupported, requirement.RequirementSatisfied},
		{model.StateUnsupported, requirement.RequirementUnsatisfied},
		{model.StateMisconfigured, requirement.RequirementUnsatisfied},
		{model.StateUnavailable, requirement.RequirementIndeterminate},
		{model.StateUnknown, requirement.RequirementIndeterminate},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()
			node := &requirement.Node{Capability: featureID}
			got := requirement.Evaluate(node, []model.Candidate{candidate(tt.state)})
			if got.State != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			not := requirement.Evaluate(&requirement.Node{Not: node}, []model.Candidate{candidate(tt.state)})
			if not.State != requirement.Not(tt.want) {
				t.Fatal("predicate NOT changed unknown semantics")
			}
		})
	}
}

func TestRequirementCannotPoolCandidates(t *testing.T) {
	t.Parallel()
	a, b := candidate(model.StateSupported), candidate(model.StateSupported)
	other := model.CapabilityID("container.storage.overlay")
	b.Scope.Runtime = "docker"
	b.Capabilities[0].Scope = b.Scope
	b.Capabilities[0].ID = other
	b.Evidence[0].Scope = b.Scope
	b.Evidence[0].Claim = string(other)
	node := &requirement.Node{All: []*requirement.Node{{Capability: featureID}, {Capability: other}}}
	if got := requirement.Evaluate(node, []model.Candidate{a, b}); got.State != requirement.RequirementIndeterminate {
		t.Fatalf("borrowed capabilities from different runtimes: %+v", got)
	}
	a.Capabilities = append(a.Capabilities, model.Capability{ID: other, Scope: a.Scope, State: model.StateSupported,
		Confidence: model.ConfidenceDerived, Evidence: []model.EvidenceRef{{ID: "other"}}})
	ev := a.Evidence[0]
	ev.ID = "other"
	ev.Claim = string(other)
	a.Evidence = append(a.Evidence, ev)
	if got := requirement.Evaluate(node, []model.Candidate{a, b}); got.State != requirement.RequirementSatisfied {
		t.Fatal(got)
	}
}

func TestRequirementInvalidInputs(t *testing.T) {
	t.Parallel()
	cycle := &requirement.Node{}
	cycle.Not = cycle
	tests := []struct {
		name   string
		node   *requirement.Node
		change func(*model.Candidate)
	}{
		{"nil", nil, nil}, {"empty", &requirement.Node{}, nil}, {"cycle", cycle, nil},
		{"multiple", &requirement.Node{Capability: featureID, Any: []*requirement.Node{}}, nil},
		{"invalid ID", &requirement.Node{Capability: "typo.feature"}, nil},
		{"missing", &requirement.Node{Capability: "runtime.missing"}, nil},
		{"wrong scope", &requirement.Node{Capability: featureID}, func(c *model.Candidate) { c.Capabilities[0].Scope.Endpoint = "remote" }},
		{"missing evidence", &requirement.Node{Capability: featureID}, func(c *model.Candidate) { c.Evidence = nil }},
		{"invalid state", &requirement.Node{Capability: featureID}, func(c *model.Candidate) { c.Capabilities[0].State = "invalid" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := candidate(model.StateSupported)
			if tt.change != nil {
				tt.change(&c)
			}
			got := requirement.Evaluate(tt.node, []model.Candidate{c})
			if got.State != requirement.RequirementIndeterminate || len(got.Diagnostics) == 0 {
				t.Fatal(got)
			}
		})
	}
}

func TestRequirementResourceAndEvidenceBoundaries(t *testing.T) {
	t.Parallel()
	node := &requirement.Node{Capability: featureID}
	for _, count := range []int{0, requirement.MaxCandidates + 1} {
		got := requirement.Evaluate(node, make([]model.Candidate, count))
		if got.State != requirement.RequirementIndeterminate || len(got.Diagnostics) == 0 {
			t.Fatal(got)
		}
	}
	deep := node
	for range requirement.MaxDepth {
		deep = &requirement.Node{Not: deep}
	}
	if err := requirement.Validate(deep); err == nil {
		t.Fatal("unbounded depth")
	}
	wide := &requirement.Node{All: make([]*requirement.Node, requirement.MaxNodes)}
	for i := range wide.All {
		wide.All[i] = node
	}
	if err := requirement.Validate(wide); err == nil {
		t.Fatal("unbounded node count")
	}
	mutations := []struct {
		name   string
		change func(*model.Candidate)
	}{
		{"scope", func(c *model.Candidate) { c.Scope.ContextID = "" }},
		{"cap limit", func(c *model.Candidate) { c.Capabilities = make([]model.Capability, requirement.MaxCandidateEntries+1) }},
		{"evidence limit", func(c *model.Candidate) { c.Evidence = make([]model.Evidence, requirement.MaxCandidateEntries+1) }},
		{"duplicate capability", func(c *model.Candidate) { c.Capabilities = append(c.Capabilities, c.Capabilities[0]) }},
		{"duplicate evidence", func(c *model.Candidate) { c.Evidence = append(c.Evidence, c.Evidence[0]) }},
		{"missing references", func(c *model.Candidate) { c.Capabilities[0].Evidence = nil }},
		{"invalid evidence", func(c *model.Candidate) { c.Evidence[0].Source = "" }},
		{"evidence scope", func(c *model.Candidate) { c.Evidence[0].Scope.Endpoint = "remote" }},
		{"partial evidence", func(c *model.Candidate) { c.Evidence[0].Completeness = model.Partial }},
		{"contradictory evidence", func(c *model.Candidate) { c.Evidence[0].State = model.StateUnsupported }},
		{"evidence claim", func(c *model.Candidate) { c.Evidence[0].Claim = "runtime.other" }},
		{"evidence confidence", func(c *model.Candidate) { c.Evidence[0].Confidence = "invalid" }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := candidate(model.StateSupported)
			tt.change(&c)
			result := requirement.Evaluate(node, []model.Candidate{c})
			if result.State != requirement.RequirementIndeterminate || len(result.Diagnostics) == 0 {
				t.Fatal(result)
			}
		})
	}
}

func TestASTFoldsAndCandidateIsolation(t *testing.T) {
	t.Parallel()
	known := &requirement.Node{Capability: featureID}
	missing := &requirement.Node{Capability: "runtime.missing"}
	c := candidate(model.StateSupported)
	tests := []struct {
		node *requirement.Node
		want requirement.RequirementState
	}{
		{&requirement.Node{All: []*requirement.Node{}}, requirement.RequirementSatisfied},
		{&requirement.Node{Any: []*requirement.Node{}}, requirement.RequirementUnsatisfied},
		{&requirement.Node{Any: []*requirement.Node{missing, known}}, requirement.RequirementSatisfied},
		{&requirement.Node{All: []*requirement.Node{known, missing}}, requirement.RequirementIndeterminate},
		// Malformed branches cannot be hidden behind a satisfied OR.
		{&requirement.Node{Any: []*requirement.Node{known, nil}}, requirement.RequirementIndeterminate},
	}
	for _, tt := range tests {
		if got := requirement.Evaluate(tt.node, []model.Candidate{c}); got.State != tt.want {
			t.Fatal(got)
		}
	}
	for _, dimension := range []string{"target", "endpoint", "runtime", "run"} {
		t.Run(dimension, func(t *testing.T) {
			t.Parallel()
			a, b := candidate(model.StateSupported), candidate(model.StateSupported)
			switch dimension {
			case "target":
				b.Scope.ContextID = "other-user"
			case "endpoint":
				b.Scope.Endpoint = "remote"
			case "runtime":
				b.Scope.Runtime = "docker"
			case "run":
				b.Scope.RunID = "other-run"
			}
			b.Capabilities[0].ID = missing.Capability
			b.Capabilities[0].Scope = b.Scope
			b.Evidence[0].Claim = string(missing.Capability)
			b.Evidence[0].Scope = b.Scope
			got := requirement.Evaluate(&requirement.Node{All: []*requirement.Node{known, missing}}, []model.Candidate{a, b})
			if got.State != requirement.RequirementIndeterminate {
				t.Fatal("pooled deployment candidates", got)
			}
		})
	}
}
