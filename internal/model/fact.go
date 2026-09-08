package model

import (
	"time"
)

// Fact represents a raw, uninterpreted measurement directly retrieved from the host environment.
//
// Examples include raw bytes read from /proc or /sys, file stat structures, or command stdout.
type Fact struct {
	Source    string    `json:"source"`
	RawData   []byte    `json:"raw_data,omitempty"`
	Text      string    `json:"text,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ObservationRef uniquely references an observation produced by a probe.
type ObservationRef struct {
	ID      string `json:"id"`
	ProbeID string `json:"probe_id"`
}

// Observation represents a structured datum produced by an individual probe.
//
// In accordance with Milestone 0 design, Observation is minimal and unencumbered
// by premature generic attribute bags. Concrete payload types are introduced
// with specific probes in subsequent milestones.
type Observation struct {
	ID        string    `json:"id"`
	ProbeID   string    `json:"probe_id"`
	Timestamp time.Time `json:"timestamp"`
	Summary   string    `json:"summary"`
}
