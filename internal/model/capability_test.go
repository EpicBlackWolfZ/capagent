package model_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestCapabilityID_ParsingAndValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		id            model.CapabilityID
		wantErr       bool
		wantNamespace string
		wantSubsystem string
		wantName      string
	}{
		{
			name:          "four segment ID",
			id:            model.CapabilityID("runtime.podman.network.netavark"),
			wantErr:       false,
			wantNamespace: "runtime",
			wantSubsystem: "podman.network",
			wantName:      "netavark",
		},
		{
			name:          "three segment ID",
			id:            model.CapabilityID("container.lifecycle.systemd_native"),
			wantErr:       false,
			wantNamespace: "container",
			wantSubsystem: "lifecycle",
			wantName:      "systemd_native",
		},
		{
			name:          "two segment ID",
			id:            model.CapabilityID("runtime.docker"),
			wantErr:       false,
			wantNamespace: "runtime",
			wantSubsystem: "",
			wantName:      "docker",
		},
		{
			name:          "all known namespaces valid",
			id:            model.CapabilityID("host.cgroups.v2"),
			wantErr:       false,
			wantNamespace: "host",
			wantSubsystem: "cgroups",
			wantName:      "v2",
		},
		{
			name:          "storage namespace",
			id:            model.CapabilityID("storage.driver.overlay2"),
			wantErr:       false,
			wantNamespace: "storage",
			wantSubsystem: "driver",
			wantName:      "overlay2",
		},
		{
			name:          "network namespace",
			id:            model.CapabilityID("network.backend.cni"),
			wantErr:       false,
			wantNamespace: "network",
			wantSubsystem: "backend",
			wantName:      "cni",
		},
		{
			name:          "image namespace",
			id:            model.CapabilityID("image.signature.cosign"),
			wantErr:       false,
			wantNamespace: "image",
			wantSubsystem: "signature",
			wantName:      "cosign",
		},
		{
			name:          "systemd namespace",
			id:            model.CapabilityID("systemd.native.unit"),
			wantErr:       false,
			wantNamespace: "systemd",
			wantSubsystem: "native",
			wantName:      "unit",
		},
		{
			name:    "invalid single segment",
			id:      model.CapabilityID("container"),
			wantErr: true,
		},
		{
			name:    "invalid unknown namespace",
			id:      model.CapabilityID("cloud.k8s.pod"),
			wantErr: true,
		},
		{
			name:    "invalid uppercase characters",
			id:      model.CapabilityID("container.Lifecycle.systemd"),
			wantErr: true,
		},
		{
			name:    "invalid dashes in name",
			id:      model.CapabilityID("container.network.custom-dns"),
			wantErr: true,
		},
		{
			name:    "invalid empty segment",
			id:      model.CapabilityID("container..dns"),
			wantErr: true,
		},
		{
			name:    "invalid trailing dot",
			id:      model.CapabilityID("container.network."),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.id.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("CapabilityID.Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if got := tt.id.String(); got != string(tt.id) {
					t.Errorf("CapabilityID.String() = %q, want %q", got, string(tt.id))
				}
				if got := tt.id.Namespace(); got != tt.wantNamespace {
					t.Errorf("CapabilityID.Namespace() = %q, want %q", got, tt.wantNamespace)
				}
				if got := tt.id.Subsystem(); got != tt.wantSubsystem {
					t.Errorf("CapabilityID.Subsystem() = %q, want %q", got, tt.wantSubsystem)
				}
				if got := tt.id.Name(); got != tt.wantName {
					t.Errorf("CapabilityID.Name() = %q, want %q", got, tt.wantName)
				}
			}
		})
	}
}

func TestCapabilityID_EmptyEdgeCases(t *testing.T) {
	t.Parallel()

	empty := model.CapabilityID("")
	if err := empty.Validate(); err == nil {
		t.Error("expected error for empty CapabilityID.Validate(), got nil")
	}
	if got := empty.Namespace(); got != "" {
		t.Errorf("empty.Namespace() = %q, want empty string", got)
	}
	if got := empty.Name(); got != "" {
		t.Errorf("empty.Name() = %q, want empty string", got)
	}
	if got := empty.Subsystem(); got != "" {
		t.Errorf("empty.Subsystem() = %q, want empty string", got)
	}
}

func TestCapability_Validate(t *testing.T) {
	t.Parallel()

	validCap := model.Capability{
		ID:         model.CapabilityID("container.lifecycle.systemd_native"),
		State:      model.StateSupported,
		Confidence: model.ConfidenceVerified,
		Reason:     "cgroup v2 and quadlet present",
		Evidence: []model.EvidenceRef{
			{ID: "ev-1"},
			{ID: "ev-2"},
		},
	}

	if err := validCap.Validate(); err != nil {
		t.Fatalf("valid capability failed validation: %v", err)
	}

	invalidID := validCap
	invalidID.ID = model.CapabilityID("invalid")
	if err := invalidID.Validate(); err == nil {
		t.Error("expected error for invalid ID, got nil")
	}

	invalidState := validCap
	invalidState.State = model.CapabilityState("bad")
	if err := invalidState.Validate(); err == nil {
		t.Error("expected error for invalid State, got nil")
	}

	invalidConfidence := validCap
	invalidConfidence.Confidence = model.ConfidenceLevel("bad")
	if err := invalidConfidence.Validate(); err == nil {
		t.Error("expected error for invalid Confidence, got nil")
	}
}
