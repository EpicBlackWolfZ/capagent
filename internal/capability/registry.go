package capability

import (
	"errors"
	"slices"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

// Evaluator is trusted pure code. Inputs are borrowed and must not be mutated
// or retained. Evaluators derive claims from typed observations without I/O.
type Evaluator func(model.EvaluationScope, []model.Observation) []model.Evidence

type Definition struct {
	ID           model.CapabilityID
	Description  string
	Dependencies []model.CapabilityID
	Evaluate     Evaluator
}

type Registry struct{ definitions []Definition }

type Evaluation struct {
	Candidate   model.Candidate
	Resolutions []Resolution
	Diagnostics []model.Diagnostic
}

func NewRegistry(definitions []Definition) (*Registry, error) {
	byID := make(map[string]Definition)
	edges := make(map[string][]string)
	for _, def := range definitions {
		if def.ID.Validate() != nil || def.Description == "" || def.Evaluate == nil {
			return nil, errors.New("invalid capability definition")
		}
		id := string(def.ID)
		if _, ok := byID[id]; ok {
			return nil, errors.New("duplicate capability definition")
		}
		def.Dependencies = slices.Clone(def.Dependencies)
		byID[id] = def
		edges[id] = nil
		for _, dep := range def.Dependencies {
			edges[id] = append(edges[id], string(dep))
		}
	}
	order, err := topological(edges)
	if err != nil {
		return nil, err
	}
	registry := &Registry{}
	for _, id := range order {
		registry.definitions = append(registry.definitions, byID[id])
	}
	return registry, nil
}

func (r *Registry) Definitions() []Definition {
	out := slices.Clone(r.definitions)
	for i := range out {
		out[i].Dependencies = slices.Clone(out[i].Dependencies)
	}
	return out
}

func (r *Registry) Evaluate(scope model.EvaluationScope, data Dataset) (Evaluation, error) {
	result := Evaluation{Candidate: model.Candidate{Scope: scope}}
	if scope.IsValid() != nil || scope.RunID != data.RunID {
		return result, errors.New("candidate does not belong to evaluation run")
	}
	if err := ValidateDataset(data); err != nil {
		return result, err
	}
	var evaluationContext *model.EvaluationContext
	for i := range data.Contexts {
		if data.Contexts[i].ID == scope.ContextID {
			evaluationContext = &data.Contexts[i]
		}
	}
	if evaluationContext == nil {
		return result, errors.New("candidate context is missing")
	}
	data.Evidence = cloneEvidence(data.Evidence)
	resolved := make(map[model.CapabilityID]model.Capability)
	for _, def := range r.definitions {
		reason := missingPrerequisite(def, resolved)
		if evaluationContext.Identity.Current == nil || evaluationContext.Identity.Target == nil {
			reason = "execution or target identity is unobserved"
		}
		var resolution Resolution
		if reason != "" {
			resolution = Resolution{Capability: model.Capability{ID: def.ID, Scope: scope, State: model.StateUnknown,
				Confidence: model.ConfidenceUnknown, Reason: reason},
				Diagnostics: []model.Diagnostic{diagnostic("missing_prerequisite", reason, string(def.ID))}}
		} else {
			generated := def.Evaluate(scope, data.Observations)
			for _, ev := range generated {
				if ev.Claim != string(def.ID) || ev.Scope != scope {
					return result, errors.New("evaluator returned another claim or scope")
				}
			}
			data.Evidence = append(data.Evidence, cloneEvidence(generated)...)
			if err := ValidateDataset(data); err != nil {
				return result, err
			}
			resolution = Resolve(scope, def.ID, data.At, data.Evidence)
		}
		resolved[def.ID] = resolution.Capability
		result.Candidate.Capabilities = append(result.Candidate.Capabilities, resolution.Capability)
		result.Resolutions = append(result.Resolutions, resolution)
		result.Diagnostics = append(result.Diagnostics, resolution.Diagnostics...)
	}
	for _, ev := range data.Evidence {
		if ev.Scope == scope {
			result.Candidate.Evidence = append(result.Candidate.Evidence, ev)
		}
	}
	return result, nil
}

func missingPrerequisite(def Definition, resolved map[model.CapabilityID]model.Capability) string {
	for _, dep := range def.Dependencies {
		if resolved[dep].State != model.StateSupported {
			return "prerequisite " + string(dep) + " is not supported"
		}
	}
	return ""
}

func cloneEvidence(input []model.Evidence) []model.Evidence {
	out := slices.Clone(input)
	for i := range out {
		out[i].Observations = slices.Clone(out[i].Observations)
		out[i].DependsOn = slices.Clone(out[i].DependsOn)
	}
	return out
}
