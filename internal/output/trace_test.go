package output_test

import (
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

const satisfiedState = "SATISFIED"

func TestEvaluationTrace(t *testing.T) {
	t.Parallel()
	scope := model.EvaluationScope{RunID: "run", ContextID: "user", Runtime: "podman", Endpoint: "local"}
	trace := output.NewEvaluationTrace(scope, time.Unix(1, 0), "synthetic")
	trace.Requirement = output.RequirementResult{State: "INDETERMINATE", Reason: "no observations"}
	if err := trace.Validate(); err != nil {
		t.Fatal(err)
	}
	trace.Requirement.State = "invalid"
	if err := trace.Validate(); err == nil {
		t.Fatal("invalid requirement accepted")
	}
	trace.Requirement.State = "INDETERMINATE"
	trace.Scope.ContextID = ""
	if err := trace.Validate(); err == nil {
		t.Fatal("invalid scope accepted")
	}
}

func TestTraceResultValidationBounds(t *testing.T) {
	t.Parallel()
	scope := model.EvaluationScope{RunID: "run", ContextID: "user", Runtime: "podman", Endpoint: "local"}
	for _, kind := range []string{"mode", "provenance", "time", "child scope", "child state", "depth", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			trace := output.NewEvaluationTrace(scope, time.Unix(1, 0), "synthetic")
			trace.Requirement = output.RequirementResult{State: satisfiedState, Reason: "fixture"}
			switch kind {
			case "mode":
				trace.Mode = "live"
			case "provenance":
				trace.Provenance = "unknown"
			case "time":
				trace.Timestamp = time.Time{}
			case "child scope":
				trace.Requirement.Scope = &output.Scope{}
			case "child state":
				trace.Requirement.Children = []output.RequirementResult{{State: "invalid"}}
			case "depth":
				const overDepth = 35
				for range overDepth {
					trace.Requirement = output.RequirementResult{State: satisfiedState, Children: []output.RequirementResult{trace.Requirement}}
				}
			case "nodes":
				const overNodes = 66001
				trace.Requirement.Children = make([]output.RequirementResult, overNodes)
				for i := range trace.Requirement.Children {
					trace.Requirement.Children[i].State = satisfiedState
				}
			}
			if err := trace.Validate(); err == nil {
				t.Fatal("invalid trace accepted")
			}
		})
	}
	trace := output.NewEvaluationTrace(scope, time.Unix(1, 0), "captured")
	projected := output.ProjectScope(scope)
	trace.Requirement = output.RequirementResult{State: satisfiedState, Scope: &projected,
		Children: []output.RequirementResult{{State: satisfiedState}}}
	if err := trace.Validate(); err != nil {
		t.Fatal(err)
	}
}
