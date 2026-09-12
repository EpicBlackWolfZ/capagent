package output

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const (
	// CurrentSchemaVersion defines the canonical major schema version for Schema v1.
	CurrentSchemaVersion = 1

	// jsonIndent specifies the standard 2-space indentation prefix for pretty-printed output.
	jsonIndent = "  "
)

// Context encapsulates execution context details projected from model.EvaluationContext.
type Context struct {
	Completeness string  `json:"completeness,omitempty"`
	UID          *uint32 `json:"uid"`
	GID          *uint32 `json:"gid"`
	TargetUser   string  `json:"target_user"`
	IsRootless   *bool   `json:"is_rootless"`
	InContainer  *bool   `json:"in_container"`
}

// Host encapsulates observed kernel, distribution, and init system facts.
type Host struct {
	Completeness  string `json:"completeness,omitempty"`
	OS            string `json:"os"`
	OSVersion     string `json:"os_version"`
	Kernel        string `json:"kernel"`
	Architecture  string `json:"architecture"`
	CgroupVersion string `json:"cgroup_version"`
	Systemd       *bool  `json:"systemd"`
}

// RuntimeInfo models discovered container runtime state.
type RuntimeInfo struct {
	Completeness   string `json:"completeness,omitempty"`
	Installed      *bool  `json:"installed"`
	Version        string `json:"version,omitempty"`
	Accessible     *bool  `json:"accessible"`
	NetworkBackend string `json:"network_backend,omitempty"`
	StorageDriver  string `json:"storage_driver,omitempty"`
}

// CapabilityReport models canonical capability status, confidence, and supporting evidence.
type CapabilityReport struct {
	Superseded []string `json:"superseded,omitempty"`
	State      string   `json:"state"`
	Confidence string   `json:"confidence"`
	Reason     string   `json:"reason,omitempty"`
	Evidence   []string `json:"evidence"`
}

// Report represents the root Schema v1 evaluation report.
type Report struct {
	SchemaVersion int                         `json:"schema_version"`
	Context       Context                     `json:"context"`
	Host          Host                        `json:"host"`
	Runtimes      map[string]RuntimeInfo      `json:"runtimes"`
	Capabilities  map[string]CapabilityReport `json:"capabilities"`
	Evaluation    *EvaluationTrace            `json:"evaluation,omitempty"`
}

// NewReport constructs an empty Schema v1 report with initialized non-nil maps and
// SchemaVersion set to CurrentSchemaVersion.
//
// Note: NewReport serves as a mutable builder/skeleton. Zero-valued fields (such as
// Host.CgroupVersion) will fail Validate() until properly populated.
func NewReport() *Report {
	return &Report{
		SchemaVersion: CurrentSchemaVersion,
		Runtimes:      make(map[string]RuntimeInfo),
		Capabilities:  make(map[string]CapabilityReport),
	}
}

// NewReportFromModel projects internal domain types into a canonical Schema v1 Report.
//
// Semantics:
//   - Target user identity is favored; falls back to current identity only if target is nil.
//   - Host SystemdActive is projected onto external contract field 'systemd'.
//   - Structured model.EvidenceRef.ID is flattened into []string.
//   - Non-null containers: runtimes and capabilities maps and evidence slices are guaranteed non-nil.
func NewReportFromModel(evalCtx model.EvaluationContext, runtimes map[string]RuntimeInfo, caps []model.Capability) *Report {
	report := NewReport()

	// Only nil means no explicit target. A target with UID zero remains root.
	target := evalCtx.Identity.Target
	if target == nil {
		target = evalCtx.Identity.Current
	}

	report.Context = Context{
		IsRootless:  copyValue(evalCtx.Identity.IsRootless),
		InContainer: copyValue(evalCtx.Identity.InContainer),
	}
	if target != nil {
		report.Context.UID = copyValue(&target.UID)
		report.Context.GID = copyValue(&target.GID)
		report.Context.TargetUser = target.Username
	}

	report.Host = Host{
		OS:            evalCtx.Host.OS,
		OSVersion:     evalCtx.Host.OSVersion,
		Kernel:        evalCtx.Host.Kernel,
		Architecture:  evalCtx.Host.Architecture,
		CgroupVersion: evalCtx.Host.CgroupVersion,
		Systemd:       copyValue(evalCtx.Host.SystemdActive),
	}

	for k, v := range runtimes {
		v.Installed, v.Accessible = copyValue(v.Installed), copyValue(v.Accessible)
		report.Runtimes[k] = v
	}

	for _, c := range caps {
		evidence := make([]string, 0, len(c.Evidence))
		for _, e := range c.Evidence {
			evidence = append(evidence, e.ID)
		}

		report.Capabilities[c.ID.String()] = CapabilityReport{
			State:      c.State.String(),
			Confidence: c.Confidence.String(),
			Reason:     c.Reason,
			Evidence:   evidence,
		}
	}

	return report
}

// prepareForSerialization returns a deep clone of the report suitable for deterministic serialization.
//
// Crucially, prepareForSerialization ensures the caller's Report and its evidence slices are NEVER mutated.
func prepareForSerialization(r *Report) *Report {
	if r == nil {
		return nil
	}

	clone := *r

	// Clone runtimes map
	clone.Runtimes = make(map[string]RuntimeInfo, len(r.Runtimes))
	for k, v := range r.Runtimes {
		clone.Runtimes[k] = v
	}

	// Clone capabilities map and sort cloned evidence slices lexicographically
	clone.Capabilities = make(map[string]CapabilityReport, len(r.Capabilities))
	for k, v := range r.Capabilities {
		capCopy := v
		if v.Evidence == nil {
			capCopy.Evidence = []string{}
		} else {
			capCopy.Evidence = make([]string, len(v.Evidence))
			copy(capCopy.Evidence, v.Evidence)
			slices.Sort(capCopy.Evidence)
		}
		clone.Capabilities[k] = capCopy
	}

	return &clone
}

// Marshal encodes a Report into canonically sorted, 2-space indented JSON with a trailing newline.
//
// Identical inputs produce canonical byte-identical output for a given build.
//
// Invariant: Marshal is non-mutating and safe for concurrent execution. It serializes the report
// without calling Validate(); callers desiring contract validation should invoke r.Validate() first.
func Marshal(r *Report) ([]byte, error) {
	if r == nil {
		return nil, errors.New("cannot marshal nil report")
	}

	clone := prepareForSerialization(r)

	data, err := json.Marshal(clone,
		jsontext.WithIndent(jsonIndent),
		jsontext.EscapeForHTML(false),
		json.Deterministic(true),
	)
	if err != nil {
		return nil, err
	}

	return append(data, '\n'), nil
}

// MarshalCompact encodes a Report into compact JSON without indentation or trailing newline.
//
// Identical inputs produce canonical byte-identical output for a given build.
//
// Invariant: MarshalCompact is non-mutating and safe for concurrent execution. It serializes the report
// without calling Validate(); callers desiring contract validation should invoke r.Validate() first.
func MarshalCompact(r *Report) ([]byte, error) {
	if r == nil {
		return nil, errors.New("cannot marshal nil report")
	}

	clone := prepareForSerialization(r)

	return json.Marshal(clone,
		jsontext.EscapeForHTML(false),
		json.Deterministic(true),
	)
}

// Unmarshal decodes JSON data into a Report DTO without validating Schema v1 contract rules.
// To validate report invariants, call (*Report).Validate().
func Unmarshal(data []byte) (*Report, error) {
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Validate checks report-level invariants not guaranteed by Go's type system.
//
// It verifies schema version alignment, non-nil collections, valid cgroup enum values,
// valid capability state/confidence enums, and non-nil capability evidence slices.
// Note: Validate is not a full JSON Schema validator; authoritative wire-contract
// validation is performed by the JSON Schema test suite.
func (r *Report) Validate() error {
	if r == nil {
		return errors.New("report is nil")
	}
	if r.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("invalid or missing schema_version: %d (expected %d)", r.SchemaVersion, CurrentSchemaVersion)
	}
	if r.Runtimes == nil {
		return errors.New("invalid report: 'runtimes' field cannot be nil")
	}
	if r.Capabilities == nil {
		return errors.New("invalid report: 'capabilities' field cannot be nil")
	}
	if r.Host.OS == "" {
		return errors.New("invalid report: 'host.os' cannot be empty")
	}
	if err := validateCgroupVersion(r.Host.CgroupVersion); err != nil {
		return fmt.Errorf("invalid report: %w", err)
	}

	for name, c := range r.Capabilities {
		if err := c.Validate(name); err != nil {
			return err
		}
	}
	if r.Evaluation != nil {
		return r.Evaluation.Validate()
	}

	return nil
}

// Validate checks whether the CapabilityReport satisfies state, confidence, and evidence invariants.
func (c CapabilityReport) Validate(name string) error {
	if err := model.CapabilityID(name).Validate(); err != nil {
		return err
	}
	if err := model.CapabilityState(c.State).IsValid(); err != nil {
		return fmt.Errorf("invalid capability %q: %w", name, err)
	}
	if err := model.ConfidenceLevel(c.Confidence).IsValid(); err != nil {
		return fmt.Errorf("invalid capability %q: %w", name, err)
	}
	if c.Evidence == nil {
		return fmt.Errorf("invalid capability %q: 'evidence' cannot be nil", name)
	}
	return nil
}

func copyValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// validateCgroupVersion checks that the cgroup_version string is a valid enum value.
func validateCgroupVersion(v string) error {
	switch v {
	case "v1", "v2", "mixed", "unavailable", "unknown":
		return nil
	default:
		return fmt.Errorf("invalid host.cgroup_version: %q", v)
	}
}
