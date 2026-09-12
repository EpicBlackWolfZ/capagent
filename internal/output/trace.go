package output

import (
	"errors"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

// Wire types deliberately remain separate from domain records. Fact values and
// command output are not serialized: references retain provenance without
// publishing environment values, credentials or arbitrary runtime output.
type Scope struct {
	RunID     string `json:"run_id"`
	ContextID string `json:"context_id"`
	Runtime   string `json:"runtime"`
	Endpoint  string `json:"endpoint"`
}
type Identity struct {
	UID                 uint32   `json:"uid"`
	GID                 uint32   `json:"gid"`
	SupplementaryGroups []uint32 `json:"supplementary_groups"`
	GroupsKnown         bool     `json:"groups_known"`
}
type Namespace struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Diagnostic struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Reference string `json:"reference,omitempty"`
}
type FactRecord struct {
	ID           string    `json:"id"`
	Source       string    `json:"source"`
	Timestamp    time.Time `json:"timestamp"`
	Completeness string    `json:"completeness"`
}
type ObservationRecord struct {
	ID           string       `json:"id"`
	ProbeID      string       `json:"probe_id"`
	Scope        Scope        `json:"scope"`
	Timestamp    time.Time    `json:"timestamp"`
	Completeness string       `json:"completeness"`
	Facts        []FactRecord `json:"facts"`
	Diagnostics  []Diagnostic `json:"diagnostics"`
}
type EvidenceRecord struct {
	ID           string    `json:"id"`
	Source       string    `json:"source"`
	Claim        string    `json:"claim"`
	Scope        Scope     `json:"scope"`
	Timestamp    time.Time `json:"timestamp"`
	Completeness string    `json:"completeness"`
	Precedence   string    `json:"precedence"`
	State        string    `json:"state"`
	Confidence   string    `json:"confidence"`
	Observations []string  `json:"observations"`
	DependsOn    []string  `json:"depends_on"`
}
type RequirementResult struct {
	State       string              `json:"state"`
	Reason      string              `json:"reason"`
	Scope       *Scope              `json:"scope,omitempty"`
	Diagnostics []Diagnostic        `json:"diagnostics"`
	Children    []RequirementResult `json:"children"`
}
type EvaluationTrace struct {
	Mode         string              `json:"mode"`
	Provenance   string              `json:"provenance"`
	Scope        Scope               `json:"scope"`
	Timestamp    time.Time           `json:"timestamp"`
	Current      *Identity           `json:"current"`
	Target       *Identity           `json:"target"`
	Namespaces   []Namespace         `json:"namespaces"`
	Observations []ObservationRecord `json:"observations"`
	Evidence     []EvidenceRecord    `json:"evidence"`
	Requirement  RequirementResult   `json:"requirement"`
	Diagnostics  []Diagnostic        `json:"diagnostics"`
}

func ProjectScope(scope model.EvaluationScope) Scope {
	return Scope{RunID: scope.RunID, ContextID: scope.ContextID, Runtime: scope.Runtime, Endpoint: scope.Endpoint}
}
func NewEvaluationTrace(scope model.EvaluationScope, at time.Time, provenance string) *EvaluationTrace {
	return &EvaluationTrace{Mode: "fixture", Provenance: provenance, Scope: ProjectScope(scope), Timestamp: at,
		Namespaces: []Namespace{}, Observations: []ObservationRecord{}, Evidence: []EvidenceRecord{}, Diagnostics: []Diagnostic{}}
}

func (s Scope) Validate() error {
	return (model.EvaluationScope{RunID: s.RunID, ContextID: s.ContextID, Runtime: s.Runtime, Endpoint: s.Endpoint}).IsValid()
}
func (e *EvaluationTrace) Validate() error {
	fixture := e.Mode == "fixture" && (e.Provenance == "synthetic" || e.Provenance == "captured")
	live := e.Mode == "live" && e.Provenance == "live"
	if (!fixture && !live) || e.Timestamp.IsZero() {
		return errors.New("invalid evaluation mode, provenance or timestamp")
	}
	if err := e.Scope.Validate(); err != nil {
		return err
	}
	count := 0
	return validateRequirement(e.Requirement, 1, &count)
}
func validateRequirement(r RequirementResult, depth int, count *int) error {
	const maxDepth = 34    // Requirement AST plus candidate and aggregate wrappers.
	const maxNodes = 66000 // 128 candidates, each with at most 512 AST nodes.
	*count++
	if depth > maxDepth || *count > maxNodes {
		return errors.New("requirement result limit exceeded")
	}
	switch r.State {
	case "SATISFIED", "UNSATISFIED", "INDETERMINATE":
	default:
		return errors.New("invalid requirement result state")
	}
	if r.Scope != nil {
		if err := r.Scope.Validate(); err != nil {
			return err
		}
	}
	for _, child := range r.Children {
		if err := validateRequirement(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}
