package model_test

import (
	json "encoding/json/v2"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestEvaluationScopeValidation(t *testing.T) {
	t.Parallel()
	valid := model.EvaluationScope{RunID: "run", ContextID: "target", Runtime: "podman", Endpoint: "local"}
	tests := []struct {
		name  string
		scope model.EvaluationScope
		valid bool
	}{
		{"complete", valid, true},
		{"zero", model.EvaluationScope{}, false},
		{"no run", model.EvaluationScope{ContextID: "target", Runtime: "podman", Endpoint: "local"}, false},
		{"no context", model.EvaluationScope{RunID: "run", Runtime: "podman", Endpoint: "local"}, false},
		{"no runtime", model.EvaluationScope{RunID: "run", ContextID: "target", Endpoint: "local"}, false},
		{"no endpoint", model.EvaluationScope{RunID: "run", ContextID: "target", Runtime: "podman"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if (tt.scope.IsValid() == nil) != tt.valid {
				t.Fatalf("scope validation: %v", tt.scope.IsValid())
			}
		})
	}
}

func TestCompletenessValidation(t *testing.T) {
	t.Parallel()
	for _, state := range []model.Completeness{model.Complete, model.Partial, model.Unobserved} {
		if err := state.IsValid(); err != nil {
			t.Fatal(err)
		}
	}
	if model.Completeness("invalid").IsValid() == nil {
		t.Fatal("accepted invalid completeness")
	}
}

func TestExplicitRootAndUnknownContext(t *testing.T) {
	t.Parallel()
	root := model.EvaluationContext{ID: "root", Identity: model.IdentityContext{Target: &model.UserIdentity{}}}
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip model.EvaluationContext
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.Identity.Target == nil || roundtrip.Identity.Target.UID != 0 {
		t.Fatal("lost explicit UID zero")
	}
	if roundtrip.Identity.Current != nil || roundtrip.Identity.IsRootless != nil || roundtrip.Host.SystemdActive != nil {
		t.Fatal("unobserved context became a negative fact")
	}
	if model.CapabilityID("context.user").Validate() != nil {
		t.Fatal("context catalog namespace is rejected")
	}
}
