package output

import (
	json "encoding/json/v2"
	"encoding/json/jsontext"
	"errors"
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
	UID         uint32 `json:"uid"`
	GID         uint32 `json:"gid"`
	TargetUser  string `json:"target_user"`
	IsRootless  bool   `json:"is_rootless"`
	InContainer bool   `json:"in_container"`
}

// Host encapsulates observed kernel, distribution, and init system facts.
type Host struct {
	OS            string `json:"os"`
	OSVersion     string `json:"os_version"`
	Kernel        string `json:"kernel"`
	Architecture  string `json:"architecture"`
	CgroupVersion string `json:"cgroup_version"`
	Systemd       bool   `json:"systemd"`
}

// RuntimeInfo models discovered container runtime state.
type RuntimeInfo struct {
	Installed      bool   `json:"installed"`
	Version        string `json:"version,omitempty"`
	Accessible     bool   `json:"accessible"`
	NetworkBackend string `json:"network_backend,omitempty"`
	StorageDriver  string `json:"storage_driver,omitempty"`
}

// CapabilityReport models canonical capability status, confidence, and supporting evidence.
type CapabilityReport struct {
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
}

// NewReport constructs an empty Schema v1 report with initialized non-nil maps.
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
//   - Target user identity is favored; falls back to current identity if target is zero-valued.
//   - Host SystemdActive is projected onto external contract field 'systemd'.
//   - Structured model.EvidenceRef.ID is flattened into []string.
//   - Non-null containers: runtimes and capabilities maps and evidence slices are guaranteed non-nil.
func NewReportFromModel(evalCtx model.EvaluationContext, runtimes map[string]RuntimeInfo, caps []model.Capability) *Report {
	report := NewReport()

	// Identity projection: Target identity takes precedence, falling back to Current if unpopulated.
	uid := evalCtx.Identity.Target.UID
	gid := evalCtx.Identity.Target.GID
	username := evalCtx.Identity.Target.Username
	if uid == 0 && username == "" && (evalCtx.Identity.Current.UID != 0 || evalCtx.Identity.Current.Username != "") {
		uid = evalCtx.Identity.Current.UID
		gid = evalCtx.Identity.Current.GID
		username = evalCtx.Identity.Current.Username
	}

	report.Context = Context{
		UID:         uid,
		GID:         gid,
		TargetUser:  username,
		IsRootless:  evalCtx.Identity.IsRootless,
		InContainer: evalCtx.Identity.InContainer,
	}

	report.Host = Host{
		OS:            evalCtx.Host.OS,
		OSVersion:     evalCtx.Host.OSVersion,
		Kernel:        evalCtx.Host.Kernel,
		Architecture:  evalCtx.Host.Architecture,
		CgroupVersion: evalCtx.Host.CgroupVersion,
		Systemd:       evalCtx.Host.SystemdActive,
	}

	for k, v := range runtimes {
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
// Invariant: Marshal is non-mutating and safe for concurrent execution.
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
// Invariant: MarshalCompact is non-mutating and safe for concurrent execution.
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

// Unmarshal decodes JSON data into a Report, ensuring collections are never nil (Policy A).
func Unmarshal(data []byte) (*Report, error) {
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}

	if r.Runtimes == nil {
		r.Runtimes = make(map[string]RuntimeInfo)
	}
	if r.Capabilities == nil {
		r.Capabilities = make(map[string]CapabilityReport)
	}

	for k, v := range r.Capabilities {
		if v.Evidence == nil {
			v.Evidence = []string{}
			r.Capabilities[k] = v
		}
	}

	return &r, nil
}
