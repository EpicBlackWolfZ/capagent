package model

import (
	"errors"
	"fmt"
	"time"
)

// EvidencePrecedence represents the strict authoritative ranking of evidence sources.
//
// Lower numerical values indicate higher precedence:
// Live (1) > Runtime (2) > Config (3) > Knowledge (4) > Heuristic (5).
type EvidencePrecedence uint8

const (
	// PrecedenceLive represents direct kernel syscalls, /proc, /sys, or live filesystem checks.
	PrecedenceLive EvidencePrecedence = 1
	// PrecedenceRuntime represents runtime-reported effective state (e.g. podman info --format json).
	PrecedenceRuntime EvidencePrecedence = 2
	// PrecedenceConfig represents parsed configuration files (registries.conf, storage.conf).
	PrecedenceConfig EvidencePrecedence = 3
	// PrecedenceKnowledge represents historical version release matrices and deprecation tables.
	PrecedenceKnowledge EvidencePrecedence = 4
	// PrecedenceHeuristic represents OS release conventions and fallback assumptions.
	PrecedenceHeuristic EvidencePrecedence = 5
)

// Overrides returns true if p strictly overrides other in precedence.
// Live evidence always overrides runtime, which overrides config, etc.
func (p EvidencePrecedence) Overrides(other EvidencePrecedence) bool {
	if p.IsValid() != nil || other.IsValid() != nil {
		return false
	}
	return p < other
}

// IsValid checks whether the precedence level is within the valid 1..5 range.
func (p EvidencePrecedence) IsValid() error {
	switch p {
	case PrecedenceLive, PrecedenceRuntime, PrecedenceConfig, PrecedenceKnowledge, PrecedenceHeuristic:
		return nil
	default:
		return fmt.Errorf("invalid evidence precedence: %d", p)
	}
}

// String returns the canonical name of the precedence tier.
func (p EvidencePrecedence) String() string {
	switch p {
	case PrecedenceLive:
		return "live"
	case PrecedenceRuntime:
		return "runtime"
	case PrecedenceConfig:
		return "config"
	case PrecedenceKnowledge:
		return "knowledge"
	case PrecedenceHeuristic:
		return "heuristic"
	default:
		return "unknown"
	}
}

// Evidence represents an authoritative claim backed by observations.
type Evidence struct {
	ID           string             `json:"id"`
	Source       string             `json:"source"`
	Claim        string             `json:"claim"`
	Precedence   EvidencePrecedence `json:"precedence"`
	Observations []ObservationRef   `json:"observations,omitempty"`
	Timestamp    time.Time          `json:"timestamp"`
}

// Validate verifies that the Evidence struct contains required fields and valid precedence.
func (e Evidence) Validate() error {
	if e.ID == "" {
		return errors.New("evidence ID cannot be empty")
	}
	if e.Source == "" {
		return errors.New("evidence Source cannot be empty")
	}
	if e.Claim == "" {
		return errors.New("evidence Claim cannot be empty")
	}
	if err := e.Precedence.IsValid(); err != nil {
		return fmt.Errorf("invalid evidence precedence: %w", err)
	}
	if e.Timestamp.IsZero() {
		return errors.New("evidence Timestamp cannot be zero")
	}
	return nil
}
