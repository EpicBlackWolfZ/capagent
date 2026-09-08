package model_test

import (
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestEvidencePrecedence_HierarchyAndOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		p1            model.EvidencePrecedence
		p2            model.EvidencePrecedence
		wantOverrides bool
	}{
		{
			name:          "live overrides runtime",
			p1:            model.PrecedenceLive,
			p2:            model.PrecedenceRuntime,
			wantOverrides: true,
		},
		{
			name:          "live overrides config",
			p1:            model.PrecedenceLive,
			p2:            model.PrecedenceConfig,
			wantOverrides: true,
		},
		{
			name:          "runtime overrides config",
			p1:            model.PrecedenceRuntime,
			p2:            model.PrecedenceConfig,
			wantOverrides: true,
		},
		{
			name:          "config overrides knowledge",
			p1:            model.PrecedenceConfig,
			p2:            model.PrecedenceKnowledge,
			wantOverrides: true,
		},
		{
			name:          "knowledge overrides heuristic",
			p1:            model.PrecedenceKnowledge,
			p2:            model.PrecedenceHeuristic,
			wantOverrides: true,
		},
		{
			name:          "heuristic does not override knowledge",
			p1:            model.PrecedenceHeuristic,
			p2:            model.PrecedenceKnowledge,
			wantOverrides: false,
		},
		{
			name:          "config does not override live",
			p1:            model.PrecedenceConfig,
			p2:            model.PrecedenceLive,
			wantOverrides: false,
		},
		{
			name:          "equal precedence does not override",
			p1:            model.PrecedenceLive,
			p2:            model.PrecedenceLive,
			wantOverrides: false,
		},
		{
			name:          "invalid p1 does not override",
			p1:            model.EvidencePrecedence(0),
			p2:            model.PrecedenceLive,
			wantOverrides: false,
		},
		{
			name:          "invalid p2 is not overridden",
			p1:            model.PrecedenceLive,
			p2:            model.EvidencePrecedence(99),
			wantOverrides: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.p1.Overrides(tt.p2); got != tt.wantOverrides {
				t.Errorf("%v.Overrides(%v) = %v, want %v", tt.p1, tt.p2, got, tt.wantOverrides)
			}
		})
	}
}

func TestEvidencePrecedence_IsValidAndString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		precedence model.EvidencePrecedence
		wantStr    string
		wantErr    bool
	}{
		{
			name:       "live",
			precedence: model.PrecedenceLive,
			wantStr:    "live",
			wantErr:    false,
		},
		{
			name:       "runtime",
			precedence: model.PrecedenceRuntime,
			wantStr:    "runtime",
			wantErr:    false,
		},
		{
			name:       "config",
			precedence: model.PrecedenceConfig,
			wantStr:    "config",
			wantErr:    false,
		},
		{
			name:       "knowledge",
			precedence: model.PrecedenceKnowledge,
			wantStr:    "knowledge",
			wantErr:    false,
		},
		{
			name:       "heuristic",
			precedence: model.PrecedenceHeuristic,
			wantStr:    "heuristic",
			wantErr:    false,
		},
		{
			name:       "invalid zero",
			precedence: model.EvidencePrecedence(0),
			wantStr:    "unknown",
			wantErr:    true,
		},
		{
			name:       "invalid out of range",
			precedence: model.EvidencePrecedence(99),
			wantStr:    "unknown",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.precedence.IsValid()
			if (err != nil) != tt.wantErr {
				t.Fatalf("EvidencePrecedence.IsValid() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if got := tt.precedence.String(); got != tt.wantStr {
				t.Errorf("EvidencePrecedence.String() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestEvidence_Validation(t *testing.T) {
	t.Parallel()

	validEvidence := model.Evidence{
		ID:         "ev-cgroup-mode",
		Source:     "platform.statfs",
		Claim:      "cgroup_v2_unified",
		Precedence: model.PrecedenceLive,
		Observations: []model.ObservationRef{
			{ID: "obs-1", ProbeID: "probe-cgroup"},
		},
		Timestamp: time.Now(),
	}

	if err := validEvidence.Validate(); err != nil {
		t.Fatalf("valid evidence failed validation: %v", err)
	}

	invID := validEvidence
	invID.ID = ""
	if err := invID.Validate(); err == nil {
		t.Error("expected error for empty ID, got nil")
	}

	invSource := validEvidence
	invSource.Source = ""
	if err := invSource.Validate(); err == nil {
		t.Error("expected error for empty Source, got nil")
	}

	invClaim := validEvidence
	invClaim.Claim = ""
	if err := invClaim.Validate(); err == nil {
		t.Error("expected error for empty Claim, got nil")
	}

	invPrec := validEvidence
	invPrec.Precedence = model.EvidencePrecedence(0)
	if err := invPrec.Validate(); err == nil {
		t.Error("expected error for invalid Precedence, got nil")
	}

	invTime := validEvidence
	invTime.Timestamp = time.Time{}
	if err := invTime.Validate(); err == nil {
		t.Error("expected error for zero Timestamp, got nil")
	}
}
