package output_test

import (
	"bytes"
	"reflect"
	"sync"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestNewReport(t *testing.T) {
	t.Parallel()

	r := output.NewReport()
	if r == nil {
		t.Fatal("NewReport returned nil")
	}
	if r.SchemaVersion != output.CurrentSchemaVersion {
		t.Errorf("expected schema version %d, got %d", output.CurrentSchemaVersion, r.SchemaVersion)
	}
	if r.Runtimes == nil {
		t.Error("expected initialized Runtimes map, got nil")
	}
	if r.Capabilities == nil {
		t.Error("expected initialized Capabilities map, got nil")
	}
}

func TestNewReportFromModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		evalCtx      model.EvaluationContext
		runtimes     map[string]output.RuntimeInfo
		caps         []model.Capability
		wantUID      uint32
		wantGID      uint32
		wantUser     string
		wantSystemd  bool
		wantRuntimes int
		wantCaps     int
	}{
		{
			name: "target identity preferred when populated",
			evalCtx: model.EvaluationContext{
				Identity: model.IdentityContext{
					Current: model.UserIdentity{UID: 0, GID: 0, Username: "root"},
					Target:  model.UserIdentity{UID: 1000, GID: 1000, Username: "ansible"},
				},
				Host: model.HostContext{
					OS:            "rhel",
					OSVersion:     "9.4",
					SystemdActive: true,
				},
			},
			runtimes: map[string]output.RuntimeInfo{
				"podman": {Installed: true, Version: "5.0.0"},
			},
			caps: []model.Capability{
				{
					ID:         model.CapabilityID("container.network.dns"),
					State:      model.StateSupported,
					Confidence: model.ConfidenceVerified,
					Evidence: []model.EvidenceRef{
						{ID: "netavark=1.0"},
						{ID: "aardvark=1.0"},
					},
				},
			},
			wantUID:      1000,
			wantGID:      1000,
			wantUser:     "ansible",
			wantSystemd:  true,
			wantRuntimes: 1,
			wantCaps:     1,
		},
		{
			name: "fallback to current identity when target is empty",
			evalCtx: model.EvaluationContext{
				Identity: model.IdentityContext{
					Current: model.UserIdentity{UID: 1001, GID: 1001, Username: "developer"},
				},
				Host: model.HostContext{
					OS:            "debian",
					OSVersion:     "12",
					SystemdActive: false,
				},
			},
			runtimes:     nil,
			caps:         nil,
			wantUID:      1001,
			wantGID:      1001,
			wantUser:     "developer",
			wantSystemd:  false,
			wantRuntimes: 0,
			wantCaps:     0,
		},
		{
			name: "target identity with only UID is considered populated",
			evalCtx: model.EvaluationContext{
				Identity: model.IdentityContext{
					Current: model.UserIdentity{UID: 2000, GID: 2000, Username: "foo"},
					Target:  model.UserIdentity{UID: 1000},
				},
				Host: model.HostContext{
					OS: "fedora",
				},
			},
			wantUID:  1000,
			wantGID:  0,
			wantUser: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := output.NewReportFromModel(tt.evalCtx, tt.runtimes, tt.caps)
			if r.Context.UID != tt.wantUID {
				t.Errorf("UID: got %d, want %d", r.Context.UID, tt.wantUID)
			}
			if r.Context.GID != tt.wantGID {
				t.Errorf("GID: got %d, want %d", r.Context.GID, tt.wantGID)
			}
			if r.Context.TargetUser != tt.wantUser {
				t.Errorf("TargetUser: got %q, want %q", r.Context.TargetUser, tt.wantUser)
			}
			if r.Host.Systemd != tt.wantSystemd {
				t.Errorf("Host.Systemd: got %v, want %v", r.Host.Systemd, tt.wantSystemd)
			}
			if len(r.Runtimes) != tt.wantRuntimes {
				t.Errorf("Runtimes length: got %d, want %d", len(r.Runtimes), tt.wantRuntimes)
			}
			if len(r.Capabilities) != tt.wantCaps {
				t.Errorf("Capabilities length: got %d, want %d", len(r.Capabilities), tt.wantCaps)
			}

			if tt.wantCaps > 0 {
				c := r.Capabilities["container.network.dns"]
				wantEvidence := []string{"netavark=1.0", "aardvark=1.0"}
				if !reflect.DeepEqual(c.Evidence, wantEvidence) {
					t.Errorf("evidence flattening: got %v, want %v", c.Evidence, wantEvidence)
				}
			}
		})
	}
}

func TestMarshal_Errors(t *testing.T) {
	t.Parallel()

	if _, err := output.Marshal(nil); err == nil {
		t.Error("expected error when marshaling nil report, got nil")
	}

	if _, err := output.MarshalCompact(nil); err == nil {
		t.Error("expected error when compact marshaling nil report, got nil")
	}
}

func TestMarshal_NonMutatingAndConcurrent(t *testing.T) {
	t.Parallel()

	r := output.NewReport()
	r.Capabilities["test.unordered"] = output.CapabilityReport{
		State:      "supported",
		Confidence: "verified",
		Evidence:   []string{"z=3", "a=1", "m=2"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := output.Marshal(r)
			if err != nil {
				t.Errorf("concurrent Marshal error: %v", err)
			}
			if !bytes.Contains(data, []byte(`"schema_version": 1`)) {
				t.Errorf("missing schema_version in concurrent marshal")
			}
		}()
	}
	wg.Wait()

	// Verify original evidence slice in r is unmodified
	orig := r.Capabilities["test.unordered"].Evidence
	want := []string{"z=3", "a=1", "m=2"}
	if !reflect.DeepEqual(orig, want) {
		t.Errorf("caller's Report evidence slice was mutated: got %v, want %v", orig, want)
	}
}

func TestMarshalCompact(t *testing.T) {
	t.Parallel()

	r := output.NewReport()
	r.Host.OS = "linux"

	compact, err := output.MarshalCompact(r)
	if err != nil {
		t.Fatalf("MarshalCompact failed: %v", err)
	}

	if bytes.Contains(compact, []byte("\n")) {
		t.Errorf("compact output contains unexpected newlines: %s", string(compact))
	}
}

func TestMarshal_NilEvidence(t *testing.T) {
	t.Parallel()

	r := output.NewReport()
	r.Capabilities["test.nil"] = output.CapabilityReport{
		State:      "supported",
		Confidence: "verified",
		Evidence:   nil,
	}

	data, err := output.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	if !bytes.Contains(data, []byte(`"evidence": []`)) {
		t.Errorf("expected empty array for nil evidence, got: %s", string(data))
	}
}

func TestUnmarshal(t *testing.T) {
	t.Parallel()

	validJSON := []byte(`{
		"schema_version": 1,
		"context": {"uid": 1000, "gid": 1000, "target_user": "u", "is_rootless": true, "in_container": false},
		"host": {"os": "linux", "os_version": "1", "kernel": "6", "architecture": "x86_64", "cgroup_version": "v2", "systemd": true},
		"runtimes": {},
		"capabilities": {
			"c1": {"state": "supported", "confidence": "verified", "evidence": ["e1"]}
		}
	}`)

	r, err := output.Unmarshal(validJSON)
	if err != nil {
		t.Fatalf("Unmarshal failed on valid JSON: %v", err)
	}

	if r.SchemaVersion != 1 {
		t.Errorf("expected schema version 1, got %d", r.SchemaVersion)
	}
	if len(r.Capabilities) != 1 {
		t.Errorf("expected 1 capability, got %d", len(r.Capabilities))
	}

	invalidTests := []struct {
		name    string
		rawJSON string
	}{
		{
			name:    "malformed JSON syntax",
			rawJSON: `{invalid json`,
		},
		{
			name: "missing schema_version",
			rawJSON: `{
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {}
			}`,
		},
		{
			name: "schema_version mismatch",
			rawJSON: `{
				"schema_version": 99,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {}
			}`,
		},
		{
			name: "null runtimes",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": null,
				"capabilities": {}
			}`,
		},
		{
			name: "missing runtimes",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"capabilities": {}
			}`,
		},
		{
			name: "null capabilities",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": null
			}`,
		},
		{
			name: "missing capabilities",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": {}
			}`,
		},
		{
			name: "capability missing state",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {
					"c1": {"confidence": "verified", "evidence": []}
				}
			}`,
		},
		{
			name: "capability missing confidence",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {
					"c1": {"state": "supported", "evidence": []}
				}
			}`,
		},
		{
			name: "capability null evidence",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {
					"c1": {"state": "supported", "confidence": "verified", "evidence": null}
				}
			}`,
		},
		{
			name: "capability missing evidence",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "u", "is_rootless": false, "in_container": false},
				"host": {"os": "l", "os_version": "1", "kernel": "6", "architecture": "x", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {
					"c1": {"state": "supported", "confidence": "verified"}
				}
			}`,
		},
	}

	for _, tt := range invalidTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := output.Unmarshal([]byte(tt.rawJSON)); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}
