package contract_test

import (
	"bytes"
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/output"
	v1 "github.com/EpicBlackWolfZ/capagent/schema/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// compileSchema compiles the embedded Schema v1 into a reusable Draft 2020-12 validator.
func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	c := jsonschema.NewCompiler()
	schemaURL := "https://github.com/EpicBlackWolfZ/capagent/schema/v1/schema.json"

	schemaJSON, err := jsonschema.UnmarshalJSON(bytes.NewReader(v1.GetSchema()))
	if err != nil {
		t.Fatalf("failed to unmarshal embedded schema JSON: %v", err)
	}

	if err := c.AddResource(schemaURL, schemaJSON); err != nil {
		t.Fatalf("failed to add schema resource: %v", err)
	}

	sch, err := c.Compile(schemaURL)
	if err != nil {
		t.Fatalf("failed to compile Draft 2020-12 schema: %v", err)
	}

	return sch
}

// validateJSON against the compiled schema.
func validateJSON(t *testing.T, sch *jsonschema.Schema, rawJSON []byte) error {
	t.Helper()

	val, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawJSON))
	if err != nil {
		t.Fatalf("failed to unmarshal JSON target for validation: %v", err)
	}

	return sch.Validate(val)
}

// TestSchemaV1_ValidDraft2020_12 asserts that schema/v1/schema.json is a valid Draft 2020-12 schema.
func TestSchemaV1_ValidDraft2020_12(t *testing.T) {
	t.Parallel()

	sch := compileSchema(t)
	if sch == nil {
		t.Fatal("compiled schema is nil")
	}
}

// TestSchemaV1_MetadataAlignment asserts that the embedded schema and package are aligned with Schema v1.
func TestSchemaV1_MetadataAlignment(t *testing.T) {
	t.Parallel()

	var raw map[string]any
	if err := json.Unmarshal(v1.GetSchema(), &raw); err != nil {
		t.Fatalf("failed to unmarshal schema JSON: %v", err)
	}

	id, ok := raw["$id"].(string)
	if !ok || id != "https://github.com/EpicBlackWolfZ/capagent/schema/v1/schema.json" {
		t.Errorf("expected schema $id to be 'https://github.com/EpicBlackWolfZ/capagent/schema/v1/schema.json', got %q", id)
	}

	props, ok := raw["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties")
	}

	ver, ok := props["schema_version"].(map[string]any)
	if !ok {
		t.Fatal("schema has no schema_version property")
	}

	constVal, ok := ver["const"].(float64)
	if !ok || int(constVal) != output.CurrentSchemaVersion {
		t.Errorf("expected schema_version const to match output.CurrentSchemaVersion (%d), got %v", output.CurrentSchemaVersion, ver["const"])
	}
}

// TestSchemaV1_GoldenFixtures validates all golden report fixtures in testdata/expected against Schema v1.
func TestSchemaV1_GoldenFixtures(t *testing.T) {
	t.Parallel()

	sch := compileSchema(t)
	rootDir := findRepoRoot(t)
	expectedDir := filepath.Join(rootDir, "testdata", "expected")

	fixtures := []string{
		"minimal_linux.json",
		"rhel9_podman.json",
		"docker_host.json",
	}

	for _, fixture := range fixtures {
		fPath := filepath.Join(expectedDir, fixture)
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(fPath)
			if err != nil {
				t.Fatalf("failed to read fixture %s: %v", fixture, err)
			}

			// 1. Validate raw fixture matches Schema v1
			if err := validateJSON(t, sch, data); err != nil {
				t.Errorf("fixture %s failed schema validation: %v", fixture, err)
			}

			// 2. Unmarshal fixture into internal/output.Report
			report, err := output.Unmarshal(data)
			if err != nil {
				t.Fatalf("failed to unmarshal fixture %s into output.Report: %v", fixture, err)
			}

			if err := report.Validate(); err != nil {
				t.Fatalf("fixture %s failed report.Validate(): %v", fixture, err)
			}

			// 3. Re-marshal and verify semantic fidelity on known fields
			remarshaled, err := output.Marshal(report)
			if err != nil {
				t.Fatalf("failed to remarshal report for fixture %s: %v", fixture, err)
			}

			report2, err := output.Unmarshal(remarshaled)
			if err != nil {
				t.Fatalf("failed to unmarshal remarshaled report for fixture %s: %v", fixture, err)
			}

			if !reflect.DeepEqual(report, report2) {
				t.Errorf("semantic fidelity mismatch on round-trip for fixture %s", fixture)
			}
		})
	}
}

// TestSchemaV1_MarshalGoStruct asserts that output.Marshal generates valid Schema v1 JSON.
func TestSchemaV1_MarshalGoStruct(t *testing.T) {
	t.Parallel()

	sch := compileSchema(t)

	report := output.NewReport()
	report.Context = output.Context{
		UID:         1000,
		GID:         1000,
		TargetUser:  "developer",
		IsRootless:  true,
		InContainer: false,
	}
	report.Host = output.Host{
		OS:            "fedora",
		OSVersion:     "40",
		Kernel:        "6.8.5-301.fc40.x86_64",
		Architecture:  "x86_64",
		CgroupVersion: "v2",
		Systemd:       true,
	}
	report.Runtimes["podman"] = output.RuntimeInfo{
		Installed:      true,
		Version:        "5.0.1",
		Accessible:     true,
		NetworkBackend: "netavark",
		StorageDriver:  "overlay",
	}
	report.Capabilities["container.lifecycle.systemd_native"] = output.CapabilityReport{
		State:      "supported",
		Confidence: "verified",
		Reason:     "systemd native Quadlet generator verified",
		Evidence:   []string{"quadlet_generator=present", "cgroups=v2"},
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report.Validate failed: %v", err)
	}

	data, err := output.Marshal(report)
	if err != nil {
		t.Fatalf("output.Marshal failed: %v", err)
	}

	if err := validateJSON(t, sch, data); err != nil {
		t.Errorf("output.Marshal JSON failed schema validation: %v\nJSON:\n%s", err, string(data))
	}
}

// TestSchemaV1_NonMutatingSerialization verifies that output.Marshal does NOT mutate the caller's Report.
func TestSchemaV1_NonMutatingSerialization(t *testing.T) {
	t.Parallel()

	report := output.NewReport()
	report.Context = output.Context{
		UID:        1000,
		GID:        1000,
		TargetUser: "testuser",
	}
	report.Host = output.Host{
		OS:            "linux",
		OSVersion:     "1.0",
		Kernel:        "6.0",
		Architecture:  "x86_64",
		CgroupVersion: "v2",
	}

	// Deliberately unsorted evidence
	originalEvidence := []string{"zeta=3", "alpha=1", "mu=2"}
	report.Capabilities["test.cap"] = output.CapabilityReport{
		State:      "supported",
		Confidence: "verified",
		Evidence:   originalEvidence,
	}

	// Capture deep copy before Marshal
	beforeJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to snapshot report: %v", err)
	}

	// Call Marshal (which sorts evidence in its output)
	marshaled, err := output.Marshal(report)
	if err != nil {
		t.Fatalf("output.Marshal failed: %v", err)
	}

	// Verify the original Report struct was NOT mutated
	afterJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to snapshot report after Marshal: %v", err)
	}

	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Errorf("output.Marshal mutated caller's report!\nBefore: %s\nAfter:  %s", string(beforeJSON), string(afterJSON))
	}

	// Verify the original slice ordering was preserved
	currentEvidence := report.Capabilities["test.cap"].Evidence
	if !reflect.DeepEqual(currentEvidence, []string{"zeta=3", "alpha=1", "mu=2"}) {
		t.Errorf("evidence slice in caller's report was mutated: got %v, want %v", currentEvidence, originalEvidence)
	}

	// But in the marshaled JSON, evidence is sorted
	var outReport output.Report
	if err := json.Unmarshal(marshaled, &outReport); err != nil {
		t.Fatalf("failed to unmarshal marshaled JSON: %v", err)
	}
	wantSorted := []string{"alpha=1", "mu=2", "zeta=3"}
	if !reflect.DeepEqual(outReport.Capabilities["test.cap"].Evidence, wantSorted) {
		t.Errorf("marshaled evidence was not sorted: got %v, want %v", outReport.Capabilities["test.cap"].Evidence, wantSorted)
	}
}

// TestSchemaV1_DeterministicByteStability verifies that output.Marshal produces identical byte sequences.
func TestSchemaV1_DeterministicByteStability(t *testing.T) {
	t.Parallel()

	report := output.NewReport()
	report.Context = output.Context{
		UID:        1000,
		GID:        1000,
		TargetUser: "testuser",
	}
	report.Host = output.Host{
		OS:            "linux",
		OSVersion:     "1.0",
		Kernel:        "6.0",
		Architecture:  "x86_64",
		CgroupVersion: "v2",
	}
	report.Capabilities["b.cap"] = output.CapabilityReport{
		State:      "supported",
		Confidence: "verified",
		Evidence:   []string{"z=9", "a=1"},
	}
	report.Capabilities["a.cap"] = output.CapabilityReport{
		State:      "supported",
		Confidence: "verified",
		Evidence:   []string{"b=2", "c=3"},
	}

	first, err := output.Marshal(report)
	if err != nil {
		t.Fatalf("initial Marshal failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		subsequent, err := output.Marshal(report)
		if err != nil {
			t.Fatalf("iteration %d Marshal failed: %v", i, err)
		}
		if !bytes.Equal(first, subsequent) {
			t.Fatalf("iteration %d produced non-deterministic output:\nFirst:\n%s\nSubsequent:\n%s", i, string(first), string(subsequent))
		}
	}
}

// TestSchemaV1_UnknownFieldsAccepted verifies Policy A: unknown properties are accepted without error.
func TestSchemaV1_UnknownFieldsAccepted(t *testing.T) {
	t.Parallel()

	sch := compileSchema(t)

	// JSON payload containing unknown fields at root, context, and host
	data := []byte(`{
		"schema_version": 1,
		"unknown_root_field": "future_feature",
		"context": {
			"uid": 1000,
			"gid": 1000,
			"target_user": "developer",
			"is_rootless": true,
			"in_container": false,
			"unknown_context_detail": {"nested": true}
		},
		"host": {
			"os": "fedora",
			"os_version": "40",
			"kernel": "6.8.5",
			"architecture": "x86_64",
			"cgroup_version": "v2",
			"systemd": true,
			"selinux_mode": "enforcing"
		},
		"runtimes": {},
		"capabilities": {}
	}`)

	// 1. Must be valid against schema (open additive evolution)
	if err := validateJSON(t, sch, data); err != nil {
		t.Errorf("schema unexpectedly rejected unknown fields: %v", err)
	}

	// 2. output.Unmarshal must succeed without error (Policy A)
	report, err := output.Unmarshal(data)
	if err != nil {
		t.Fatalf("output.Unmarshal failed on JSON with unknown fields: %v", err)
	}

	if report.Context.TargetUser != "developer" {
		t.Errorf("expected target_user 'developer', got %q", report.Context.TargetUser)
	}
}

// TestSchemaV1_DiagnosticsAndEdgeCases asserts schema rejects missing required fields and invalid enums,
// while properly handling escaped characters in diagnostic strings.
func TestSchemaV1_DiagnosticsAndEdgeCases(t *testing.T) {
	t.Parallel()

	sch := compileSchema(t)

	tests := []struct {
		name      string
		rawJSON   string
		wantError bool
	}{
		{
			name: "missing schema_version is rejected",
			rawJSON: `{
				"context": {"uid": 0, "gid": 0, "target_user": "root", "is_rootless": false, "in_container": false},
				"host": {"os": "linux", "os_version": "1", "kernel": "6", "architecture": "x86_64", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {}
			}`,
			wantError: true,
		},
		{
			name: "invalid schema_version integer is rejected",
			rawJSON: `{
				"schema_version": 2,
				"context": {"uid": 0, "gid": 0, "target_user": "root", "is_rootless": false, "in_container": false},
				"host": {"os": "linux", "os_version": "1", "kernel": "6", "architecture": "x86_64", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {}
			}`,
			wantError: true,
		},
		{
			name: "missing state in capability is rejected",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "root", "is_rootless": false, "in_container": false},
				"host": {"os": "linux", "os_version": "1", "kernel": "6", "architecture": "x86_64", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {
					"test": {
						"confidence": "verified",
						"evidence": []
					}
				}
			}`,
			wantError: true,
		},
		{
			name: "invalid capability state enum is rejected",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "root", "is_rootless": false, "in_container": false},
				"host": {"os": "linux", "os_version": "1", "kernel": "6", "architecture": "x86_64", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {
					"test": {
						"state": "super_supported",
						"confidence": "verified",
						"evidence": []
					}
				}
			}`,
			wantError: true,
		},
		{
			name: "missing evidence array in capability is rejected",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "root", "is_rootless": false, "in_container": false},
				"host": {"os": "linux", "os_version": "1", "kernel": "6", "architecture": "x86_64", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {
					"test": {
						"state": "supported",
						"confidence": "verified"
					}
				}
			}`,
			wantError: true,
		},
		{
			name: "null runtimes is rejected",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "root", "is_rootless": false, "in_container": false},
				"host": {"os": "linux", "os_version": "1", "kernel": "6", "architecture": "x86_64", "cgroup_version": "v2", "systemd": true},
				"runtimes": null,
				"capabilities": {}
			}`,
			wantError: true,
		},
		{
			name: "escaped characters in diagnostic reason is valid",
			rawJSON: `{
				"schema_version": 1,
				"context": {"uid": 0, "gid": 0, "target_user": "root", "is_rootless": false, "in_container": false},
				"host": {"os": "linux", "os_version": "1", "kernel": "6", "architecture": "x86_64", "cgroup_version": "v2", "systemd": true},
				"runtimes": {},
				"capabilities": {
					"test": {
						"state": "unsupported",
						"confidence": "verified",
						"reason": "error: \"permission denied\" on path /run/user/0\n\tfailed syscall \\ socket connection\nunicode: 🚀",
						"evidence": ["err=13"]
					}
				}
			}`,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateJSON(t, sch, []byte(tt.rawJSON))
			if tt.wantError && err == nil {
				t.Errorf("expected validation error for %s, but got none", tt.name)
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected validation error for %s: %v", tt.name, err)
			}
		})
	}
}
