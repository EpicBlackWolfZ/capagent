package app

import "github.com/EpicBlackWolfZ/capagent/internal/output"

// Aggregates are repeated at every ancestor by the domain result. Keep ordinary
// reports unchanged, but prevent a bounded document from expanding by its depth
// across the worker transport. Leaf diagnostics and the root aggregate remain
// complete; only redundant intermediate aggregates are omitted.
const maxRepeatedRequirementDiagnosticBytes = 128 * 1024
const diagnosticJSONOverhead = 64

func compactRequirementDiagnostics(trace *output.EvaluationTrace) {
	if requirementDiagnosticBytes(trace.Requirement) <= maxRepeatedRequirementDiagnosticBytes {
		return
	}
	for i := range trace.Requirement.Children {
		omitIntermediateDiagnostics(&trace.Requirement.Children[i])
	}
	trace.Diagnostics = append(trace.Diagnostics, output.Diagnostic{Code: "requirement_diagnostics_compacted",
		Message: "Repeated ancestor diagnostics omitted; root aggregate and leaf diagnostics retained"})
}

func requirementDiagnosticBytes(result output.RequirementResult) int {
	size := 0
	for _, diagnostic := range result.Diagnostics {
		size += len(diagnostic.Code) + len(diagnostic.Message) + len(diagnostic.Reference) + diagnosticJSONOverhead
	}
	for _, child := range result.Children {
		size += requirementDiagnosticBytes(child)
	}
	return size
}

func omitIntermediateDiagnostics(result *output.RequirementResult) {
	if len(result.Children) == 0 {
		return
	}
	result.Diagnostics = []output.Diagnostic{}
	for i := range result.Children {
		omitIntermediateDiagnostics(&result.Children[i])
	}
}
