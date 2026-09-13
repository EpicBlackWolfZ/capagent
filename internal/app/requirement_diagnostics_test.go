package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestRequirementDiagnosticCompactionPreservesInformation(t *testing.T) {
	t.Parallel()
	diagnostics := []output.Diagnostic{{Code: "missing_capability", Message: strings.Repeat("a", 63000), Reference: "original"}}
	leaf := output.RequirementResult{State: "INDETERMINATE", Reason: "leaf predicate", Diagnostics: diagnostics,
		Children: []output.RequirementResult{}}
	scope := &output.Scope{RunID: "same-run", ContextID: "same-target", Runtime: "podman", Endpoint: "local"}
	branch := output.RequirementResult{State: leaf.State, Reason: "not", Scope: scope, Diagnostics: diagnostics,
		Children: []output.RequirementResult{leaf}}
	trace := &output.EvaluationTrace{Requirement: output.RequirementResult{State: leaf.State, Reason: "candidate",
		Diagnostics: diagnostics, Children: []output.RequirementResult{branch}}}
	compactRequirementDiagnostics(trace)
	got := trace.Requirement.Children[0]
	if !reflect.DeepEqual(trace.Requirement.Diagnostics, diagnostics) || !reflect.DeepEqual(got.Children[0], leaf) ||
		got.State != branch.State || got.Reason != branch.Reason || got.Scope != scope || len(got.Diagnostics) != 0 {
		t.Fatal("compaction discarded unique diagnostic information, tree structure or scope")
	}
	if len(trace.Diagnostics) != 1 || trace.Diagnostics[0].Code != "requirement_diagnostics_compacted" {
		t.Fatal("compaction is not explicit in the report")
	}
}
