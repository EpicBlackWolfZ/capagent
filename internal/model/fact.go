package model

import (
	"time"
)

// Fact represents a raw, uninterpreted measurement directly retrieved from the host environment.
//
// Examples include raw bytes read from /proc or /sys, file stat structures, or command stdout.
type Fact struct {
	ID           string          `json:"id"`
	Scope        EvaluationScope `json:"scope"`
	Completeness Completeness    `json:"completeness"`
	Source       string          `json:"source"`
	RawData      []byte          `json:"raw_data,omitempty"`
	Text         string          `json:"text,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
}

// ObservationRef uniquely references an observation produced by a probe.
//
// At the domain model layer, ObservationRef is treated as an opaque reference identifier.
// Relational graph resolution and verification against collected observations are explicitly
// handled by the evidence and capability graph validators.
type ObservationRef struct {
	ID      string `json:"id"`
	ProbeID string `json:"probe_id"`
}

// Observation represents a structured datum produced by an individual probe.
//
// Concrete payloads retain field presence and accompany scoped source facts.
// Completeness and diagnostics survive failure; no generic attribute bag or
// capability evaluation is part of the model.
type Observation struct {
	ID           string                    `json:"id"`
	ProbeID      string                    `json:"probe_id"`
	Timestamp    time.Time                 `json:"timestamp"`
	Summary      string                    `json:"summary"`
	Scope        EvaluationScope           `json:"scope"`
	Completeness Completeness              `json:"completeness"`
	Facts        []Fact                    `json:"facts,omitempty"`
	Diagnostics  []Diagnostic              `json:"diagnostics,omitempty"`
	Podman       *PodmanInfo               `json:"podman,omitempty"`
	Discovery    *RuntimeDiscovery         `json:"discovery,omitempty"`
	Version      *PodmanVersionObservation `json:"version,omitempty"`
}
