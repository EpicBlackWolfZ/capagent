package model

import (
	"fmt"
)

// CapabilityState represents the operational state of a capability in the target environment.
//
// These represent operational realities, not Boolean truth values.
type CapabilityState string

const (
	// StateSupported indicates the environment and runtime fully provide and permit the capability.
	StateSupported CapabilityState = "supported"
	// StateUnsupported indicates the runtime or platform fundamentally does not provide the capability.
	StateUnsupported CapabilityState = "unsupported"
	// StateMisconfigured indicates the capability exists in software, but configuration prevents its use.
	StateMisconfigured CapabilityState = "misconfigured"
	// StateUnavailable indicates the capability exists in software, but a dependency or service is inactive.
	StateUnavailable CapabilityState = "unavailable"
	// StateUnknown indicates the engine was unable to determine the state reliably.
	StateUnknown CapabilityState = "unknown"
)

// String returns the string representation of the CapabilityState.
func (s CapabilityState) String() string {
	return string(s)
}

// IsValid validates whether the capability state is one of the five canonical states.
func (s CapabilityState) IsValid() error {
	switch s {
	case StateSupported, StateUnsupported, StateMisconfigured, StateUnavailable, StateUnknown:
		return nil
	default:
		return fmt.Errorf("invalid capability state: %q", s)
	}
}

// ConfidenceLevel represents the degree of certainty in a capability determination.
type ConfidenceLevel string

const (
	// ConfidenceVerified indicates direct verification via live probing or effective runtime inspection.
	ConfidenceVerified ConfidenceLevel = "verified"
	// ConfidenceDerived indicates inference from authoritative configuration or system dependencies.
	ConfidenceDerived ConfidenceLevel = "derived"
	// ConfidenceHeuristic indicates inference based on OS distribution conventions or historical patterns.
	ConfidenceHeuristic ConfidenceLevel = "heuristic"
	// ConfidenceUnknown indicates lack of verifiable evidence.
	ConfidenceUnknown ConfidenceLevel = "unknown"
)

// String returns the string representation of the ConfidenceLevel.
func (c ConfidenceLevel) String() string {
	return string(c)
}

// IsValid validates whether the confidence level is one of the four canonical levels.
func (c ConfidenceLevel) IsValid() error {
	switch c {
	case ConfidenceVerified, ConfidenceDerived, ConfidenceHeuristic, ConfidenceUnknown:
		return nil
	default:
		return fmt.Errorf("invalid confidence level: %q", c)
	}
}
