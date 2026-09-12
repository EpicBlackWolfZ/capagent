package model

import "fmt"

// EvaluationScope identifies one candidate deployment within a run. ContextID
// resolves to exactly one immutable EvaluationContext containing the execution
// and target identities and namespace observations. Empty IDs never match a
// usable evaluation. A runtime endpoint is explicit, including the value local.
type EvaluationScope struct {
	RunID     string `json:"run_id"`
	ContextID string `json:"context_id"`
	Runtime   string `json:"runtime"`
	Endpoint  string `json:"endpoint"`
}

func (s EvaluationScope) IsValid() error {
	if s.RunID == "" || s.ContextID == "" || s.Runtime == "" || s.Endpoint == "" {
		return fmt.Errorf("evaluation scope requires run, context, runtime and endpoint")
	}
	return nil
}

// Completeness describes a measurement, independently of capability state.
type Completeness string

const (
	Complete   Completeness = "complete"
	Partial    Completeness = "partial"
	Unobserved Completeness = "unobserved"
)

func (s Completeness) IsValid() error {
	switch s {
	case Complete, Partial, Unobserved:
		return nil
	default:
		return fmt.Errorf("invalid completeness: %q", s)
	}
}

// Diagnostic contains an adapter-selected public code and sanitized message.
// Raw command output, paths and OS error strings must not be copied here.
type Diagnostic struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Reference string `json:"reference,omitempty"`
}

// Namespace identifies an observed namespace. Absence is not a host namespace.
type Namespace struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PodmanInfo is the initial concrete observation payload. Pointer fields retain
// missing versus explicit empty/false values. HelperPresent is a separate direct
// metadata observation, not inferred from networkBackend.
type PodmanInfo struct {
	Version        string  `json:"version,omitempty"`
	NetworkBackend *string `json:"network_backend,omitempty"`
	StorageDriver  *string `json:"storage_driver,omitempty"`
	CgroupVersion  *string `json:"cgroup_version,omitempty"`
	CgroupManager  *string `json:"cgroup_manager,omitempty"`
	Rootless       *bool   `json:"rootless,omitempty"`
	HelperPath     string  `json:"helper_path,omitempty"`
	HelperPresent  *bool   `json:"helper_present,omitempty"`
	Available      *bool   `json:"available,omitempty"`
}
