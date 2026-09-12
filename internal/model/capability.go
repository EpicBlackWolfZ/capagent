package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	segmentRegex = regexp.MustCompile(`^[a-z0-9_]+$`)

	// CanonicalNamespaces defines the canonical top-level namespaces.
	CanonicalNamespaces = map[string]struct{}{
		"host":      {},
		"runtime":   {},
		"container": {},
		"image":     {},
		"network":   {},
		"storage":   {},
		"systemd":   {},
		"context":   {},
	}
)

const (
	// minCapabilitySegments is the minimum number of dot-separated segments required in a CapabilityID.
	minCapabilitySegments = 2
)

// CapabilityID represents a canonical, dot-separated capability identifier.
//
// Form: <namespace>.[<subsystem...>.]<name>
// A minimum of two dot-separated segments is required. Valid forms include:
//   - Two-segment IDs (e.g. "runtime.docker"): Namespace="runtime", Subsystem="", Name="docker".
//   - Three-segment IDs (e.g. "container.lifecycle.systemd_native"): Namespace="container", Subsystem="lifecycle", Name="systemd_native".
//   - Multi-segment IDs (e.g. "runtime.podman.network.netavark"): Namespace="runtime", Subsystem="podman.network", Name="netavark".
type CapabilityID string

// String returns the string representation of the CapabilityID.
func (id CapabilityID) String() string {
	return string(id)
}

// Namespace returns the first segment of the CapabilityID.
func (id CapabilityID) Namespace() string {
	segments := strings.Split(string(id), ".")
	return segments[0]
}

// Name returns the final segment of the CapabilityID.
func (id CapabilityID) Name() string {
	segments := strings.Split(string(id), ".")
	return segments[len(segments)-1]
}

// Subsystem returns the intermediate segments between Namespace and Name.
// If the ID has only two segments (e.g. "runtime.docker"), Subsystem returns "".
func (id CapabilityID) Subsystem() string {
	segments := strings.Split(string(id), ".")
	if len(segments) <= minCapabilitySegments {
		return ""
	}
	return strings.Join(segments[1:len(segments)-1], ".")
}

// Validate ensures the CapabilityID adheres to canonical namespace and formatting rules.
func (id CapabilityID) Validate() error {
	raw := string(id)
	if raw == "" {
		return errors.New("capability ID cannot be empty")
	}

	segments := strings.Split(raw, ".")
	if len(segments) < minCapabilitySegments {
		return fmt.Errorf("capability ID %q must contain at least two dot-separated segments", raw)
	}

	ns := segments[0]
	if _, ok := CanonicalNamespaces[ns]; !ok {
		return fmt.Errorf("unknown canonical namespace %q in capability ID %q", ns, raw)
	}

	for _, seg := range segments {
		if !segmentRegex.MatchString(seg) {
			return fmt.Errorf("invalid segment %q in capability ID %q: must match ^[a-z0-9_]+$", seg, raw)
		}
	}

	return nil
}

// EvidenceRef identifies an authoritative evidence item supporting a capability.
//
// At the domain model layer, EvidenceRef is an opaque reference identifier. Relational
// graph integrity, acyclicity, and dangling reference checks belong to
// internal/capability, which validates the evaluation graph before resolution.
type EvidenceRef struct {
	ID string `json:"id"`
}

// Capability represents an authoritative feature determination.
type Capability struct {
	Scope      EvaluationScope `json:"scope"`
	ID         CapabilityID    `json:"id"`
	State      CapabilityState `json:"state"`
	Confidence ConfidenceLevel `json:"confidence"`
	Reason     string          `json:"reason,omitempty"`
	Evidence   []EvidenceRef   `json:"evidence,omitempty"`
}

// Candidate is one independently evaluated deployment. Requirements evaluate
// each candidate as a whole; its capabilities cannot be pooled with another.
type Candidate struct {
	Scope        EvaluationScope `json:"scope"`
	Capabilities []Capability    `json:"capabilities"`
	Evidence     []Evidence      `json:"evidence"`
}

// Validate verifies that the Capability fields are structurally valid.
func (c Capability) Validate() error {
	if err := c.ID.Validate(); err != nil {
		return fmt.Errorf("invalid capability ID: %w", err)
	}
	if err := c.State.IsValid(); err != nil {
		return fmt.Errorf("invalid capability state: %w", err)
	}
	if err := c.Confidence.IsValid(); err != nil {
		return fmt.Errorf("invalid capability confidence: %w", err)
	}
	return nil
}
