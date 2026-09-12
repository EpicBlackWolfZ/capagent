package requirement

import (
	"errors"
	"fmt"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const (
	MaxDepth            = 32
	MaxNodes            = 512
	MaxCandidates       = 128
	MaxCandidateEntries = 4096
)

// Node has exactly one operator. Non-nil empty All/Any lists retain the existing
// algebra's identity elements. Parsing belongs to internal/config.
type Node struct {
	Capability model.CapabilityID `json:"capability,omitempty"`
	All        []*Node            `json:"all,omitempty"`
	Any        []*Node            `json:"any,omitempty"`
	Not        *Node              `json:"not,omitempty"`
}

type Result struct {
	State       RequirementState       `json:"state"`
	Reason      string                 `json:"reason"`
	Scope       *model.EvaluationScope `json:"scope,omitempty"`
	Diagnostics []model.Diagnostic     `json:"diagnostics,omitempty"`
	Children    []Result               `json:"children,omitempty"`
}

// Validate checks every branch before evaluation, including branches whose
// value could be short-circuited. A malformed document is never a valid verdict.
func Validate(root *Node) error {
	count := 0
	return validateNode(root, 1, &count, make(map[*Node]bool))
}

func validateNode(node *Node, depth int, count *int, active map[*Node]bool) error {
	if node == nil {
		return errors.New("nil requirement node")
	}
	*count++
	if depth > MaxDepth || *count > MaxNodes {
		return errors.New("requirement size limit exceeded")
	}
	if active[node] {
		return errors.New("cyclic requirement document")
	}
	active[node] = true
	defer delete(active, node)
	operators := 0
	for _, present := range []bool{node.Capability != "", node.All != nil, node.Any != nil, node.Not != nil} {
		if present {
			operators++
		}
	}
	if operators != 1 {
		return errors.New("requirement node needs exactly one operator")
	}
	if node.Capability != "" {
		return node.Capability.Validate()
	}
	children := node.All
	if node.Any != nil {
		children = node.Any
	}
	if node.Not != nil {
		children = []*Node{node.Not}
	}
	for _, child := range children {
		if err := validateNode(child, depth+1, count, active); err != nil {
			return err
		}
	}
	return nil
}

// Evaluate evaluates the whole document independently for each candidate, then
// folds candidate outcomes with Or. Inputs are borrowed for this call only and
// must not be mutated concurrently. Results retain no caller-owned slices/maps.
func Evaluate(root *Node, candidates []model.Candidate) Result {
	if err := Validate(root); err != nil {
		return uncertain("invalid_requirement", err.Error())
	}
	if len(candidates) == 0 || len(candidates) > MaxCandidates {
		return uncertain("invalid_candidates", "missing or excessive deployment candidates")
	}
	result := Result{State: RequirementUnsatisfied, Reason: "any complete deployment candidate"}
	for _, candidate := range candidates {
		index, err := indexCandidate(candidate)
		var child Result
		if err != nil {
			child = uncertain("invalid_candidate", err.Error())
		} else {
			child = evaluateNode(root, index)
		}
		scope := candidate.Scope
		child.Scope = &scope
		result.State = Or(result.State, child.State)
		result.Diagnostics = append(result.Diagnostics, child.Diagnostics...)
		result.Children = append(result.Children, child)
	}
	return result
}

type candidateIndex struct {
	scope    model.EvaluationScope
	caps     map[model.CapabilityID]model.Capability
	evidence map[string]model.Evidence
}

func indexCandidate(c model.Candidate) (candidateIndex, error) {
	index := candidateIndex{scope: c.Scope, caps: make(map[model.CapabilityID]model.Capability), evidence: make(map[string]model.Evidence)}
	if err := c.Scope.IsValid(); err != nil {
		return index, err
	}
	if len(c.Capabilities) > MaxCandidateEntries || len(c.Evidence) > MaxCandidateEntries {
		return index, errors.New("candidate size limit exceeded")
	}
	for _, capability := range c.Capabilities {
		if err := capability.Validate(); err != nil {
			return index, err
		}
		if capability.Scope != c.Scope {
			return index, errors.New("capability scope differs from deployment candidate")
		}
		if _, ok := index.caps[capability.ID]; ok {
			return index, errors.New("duplicate capability in deployment candidate")
		}
		index.caps[capability.ID] = capability
	}
	for _, evidence := range c.Evidence {
		if _, ok := index.evidence[evidence.ID]; ok {
			return index, errors.New("duplicate evidence in deployment candidate")
		}
		index.evidence[evidence.ID] = evidence
	}
	return index, nil
}

func evaluateNode(node *Node, index candidateIndex) Result {
	if node.Capability != "" {
		return predicate(node.Capability, index)
	}
	if node.Not != nil {
		child := evaluateNode(node.Not, index)
		return Result{State: Not(child.State), Reason: "not", Diagnostics: child.Diagnostics, Children: []Result{child}}
	}
	children, fold, initial, reason := node.All, And, RequirementSatisfied, "all"
	if node.Any != nil {
		children, fold, initial, reason = node.Any, Or, RequirementUnsatisfied, "any"
	}
	result := Result{State: initial, Reason: reason}
	for _, node := range children {
		child := evaluateNode(node, index)
		result.State = fold(result.State, child.State)
		result.Diagnostics = append(result.Diagnostics, child.Diagnostics...)
		result.Children = append(result.Children, child)
	}
	return result
}

func predicate(id model.CapabilityID, index candidateIndex) Result {
	capability, ok := index.caps[id]
	if !ok {
		return uncertain("missing_capability", fmt.Sprintf("capability %s was not evaluated", id))
	}
	if capability.State == model.StateUnknown || capability.State == model.StateUnavailable {
		return uncertain("indeterminate_capability", fmt.Sprintf("%s: %s", id, capability.State))
	}
	if len(capability.Evidence) == 0 {
		return uncertain("missing_evidence", fmt.Sprintf("capability %s has no evidence", id))
	}
	for _, ref := range capability.Evidence {
		ev, ok := index.evidence[ref.ID]
		if !ok || ev.Validate() != nil || ev.Scope != index.scope || ev.Completeness != model.Complete ||
			ev.State != capability.State || ev.Claim != string(id) || ev.Confidence.IsValid() != nil {
			return uncertain("invalid_evidence", fmt.Sprintf("capability %s has invalid supporting evidence", id))
		}
	}
	state := RequirementUnsatisfied
	if capability.State == model.StateSupported {
		state = RequirementSatisfied
	}
	return Result{State: state, Reason: fmt.Sprintf("%s: %s", id, capability.State)}
}

func uncertain(code, reason string) Result {
	return Result{State: RequirementIndeterminate, Reason: reason, Diagnostics: []model.Diagnostic{{Code: code, Message: reason}}}
}
