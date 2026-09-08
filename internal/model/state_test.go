package model_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestCapabilityState_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		state   model.CapabilityState
		wantErr bool
	}{
		{
			name:    "valid supported",
			state:   model.StateSupported,
			wantErr: false,
		},
		{
			name:    "valid unsupported",
			state:   model.StateUnsupported,
			wantErr: false,
		},
		{
			name:    "valid misconfigured",
			state:   model.StateMisconfigured,
			wantErr: false,
		},
		{
			name:    "valid unavailable",
			state:   model.StateUnavailable,
			wantErr: false,
		},
		{
			name:    "valid unknown",
			state:   model.StateUnknown,
			wantErr: false,
		},
		{
			name:    "invalid empty",
			state:   model.CapabilityState(""),
			wantErr: true,
		},
		{
			name:    "invalid uppercase",
			state:   model.CapabilityState("SUPPORTED"),
			wantErr: true,
		},
		{
			name:    "invalid random",
			state:   model.CapabilityState("disabled"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.state.IsValid()
			if (err != nil) != tt.wantErr {
				t.Fatalf("CapabilityState.IsValid() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestCapabilityState_String(t *testing.T) {
	t.Parallel()

	if got := model.StateSupported.String(); got != "supported" {
		t.Errorf("CapabilityState.String() = %q, want %q", got, "supported")
	}
}

func TestConfidenceLevel_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		confidence model.ConfidenceLevel
		wantErr    bool
	}{
		{
			name:       "valid verified",
			confidence: model.ConfidenceVerified,
			wantErr:    false,
		},
		{
			name:       "valid derived",
			confidence: model.ConfidenceDerived,
			wantErr:    false,
		},
		{
			name:       "valid heuristic",
			confidence: model.ConfidenceHeuristic,
			wantErr:    false,
		},
		{
			name:       "valid unknown",
			confidence: model.ConfidenceUnknown,
			wantErr:    false,
		},
		{
			name:       "invalid empty",
			confidence: model.ConfidenceLevel(""),
			wantErr:    true,
		},
		{
			name:       "invalid case",
			confidence: model.ConfidenceLevel("Verified"),
			wantErr:    true,
		},
		{
			name:       "invalid arbitrary",
			confidence: model.ConfidenceLevel("guessed"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.confidence.IsValid()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ConfidenceLevel.IsValid() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfidenceLevel_String(t *testing.T) {
	t.Parallel()

	if got := model.ConfidenceVerified.String(); got != "verified" {
		t.Errorf("ConfidenceLevel.String() = %q, want %q", got, "verified")
	}
}
