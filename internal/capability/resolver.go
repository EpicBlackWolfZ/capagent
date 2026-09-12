// Package capability resolves scoped evidence and evaluates a small static catalog.
package capability

import (
	"slices"
	"sort"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

type Resolution struct {
	Capability  model.Capability
	Superseded  []model.EvidenceRef
	Diagnostics []model.Diagnostic
}

// Resolve ranks only matching claims. Callers must first validate reference
// integrity with ValidateDataset. Freshness means the same run plus a timestamp
// no later than its evaluation time; there is no cross-run evidence cache.
func Resolve(scope model.EvaluationScope, id model.CapabilityID, at time.Time, input []model.Evidence) Resolution {
	result := Resolution{Capability: model.Capability{ID: id, Scope: scope, State: model.StateUnknown,
		Confidence: model.ConfidenceUnknown, Reason: "no complete evidence for this candidate"}}
	if scope.IsValid() != nil || id.Validate() != nil || at.IsZero() {
		result.Diagnostics = append(result.Diagnostics, diagnostic("invalid_scope", "invalid evaluation scope or claim", ""))
		return result
	}
	evidence := slices.Clone(input)
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].ID < evidence[j].ID })
	var selected []model.Evidence
	best := model.PrecedenceHeuristic + 1
	for _, ev := range evidence {
		if ev.Claim != string(id) {
			continue
		}
		code := unusable(ev, scope, at)
		if code != "" {
			result.Diagnostics = append(result.Diagnostics, diagnostic(code, "evidence excluded from resolution", ev.ID))
			continue
		}
		selected = append(selected, ev)
		best = min(best, ev.Precedence)
	}
	first := true
	for _, ev := range selected {
		ref := model.EvidenceRef{ID: ev.ID}
		if ev.Precedence != best {
			result.Superseded = append(result.Superseded, ref)
			continue
		}
		result.Capability.Evidence = append(result.Capability.Evidence, ref)
		if first {
			result.Capability.State = ev.State
			result.Capability.Confidence = confidence(ev)
			first = false
			result.Capability.Reason = "selected complete " + best.String() + " evidence"
		} else {
			if ev.State != result.Capability.State {
				result.Capability.State = model.StateUnknown
				result.Capability.Reason = "conflicting evidence at equal precedence"
			}
			result.Capability.Confidence = weaker(result.Capability.Confidence, confidence(ev))
		}
	}
	if result.Capability.State == model.StateUnknown {
		result.Capability.Confidence = model.ConfidenceUnknown
	}
	return result
}

func unusable(ev model.Evidence, scope model.EvaluationScope, at time.Time) string {
	if ev.Scope.RunID != scope.RunID || ev.Timestamp.After(at) {
		return "stale_evidence"
	}
	if ev.Scope != scope {
		return "incompatible_scope"
	}
	if ev.Validate() != nil || ev.State.IsValid() != nil || ev.Confidence.IsValid() != nil {
		return "invalid_evidence"
	}
	if ev.Completeness != model.Complete {
		return "incomplete_evidence"
	}
	return ""
}

func confidence(ev model.Evidence) model.ConfidenceLevel {
	ceiling := model.ConfidenceHeuristic
	switch ev.Precedence {
	case model.PrecedenceLive, model.PrecedenceRuntime:
		ceiling = model.ConfidenceVerified
	case model.PrecedenceConfig:
		ceiling = model.ConfidenceDerived
	}
	return weaker(ceiling, ev.Confidence)
}

func weaker(a, b model.ConfidenceLevel) model.ConfidenceLevel {
	levels := []model.ConfidenceLevel{model.ConfidenceUnknown, model.ConfidenceHeuristic, model.ConfidenceDerived, model.ConfidenceVerified}
	return levels[min(slices.Index(levels, a), slices.Index(levels, b))]
}

func diagnostic(code, message, reference string) model.Diagnostic {
	return model.Diagnostic{Code: code, Message: message, Reference: reference}
}
