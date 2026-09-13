package contract_test

import (
	"bytes"
	json "encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

const fixtureSupported = "supported"

const fixtureRelativeRoot = "testdata/fixtures/v1"

func TestFixtureExecutableConsumer(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	binary := filepath.Join(t.TempDir(), "capagent")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/capagent")
	build.Dir = root
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, data)
	}
	schema := compileSchema(t)
	for _, name := range []string{fixtureSupported, "unsupported", "misconfigured", "unavailable", testUnknown} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixtureDir := filepath.Join(root, fixtureRelativeRoot, name)
			cmd := exec.CommandContext(t.Context(), binary, "--fixture", fixtureDir, "--json", "--pretty", "--debug")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			expectedCode := app.ExitIndeterminate
			switch name {
			case fixtureSupported:
				expectedCode = app.ExitSatisfied
			case "unsupported", "misconfigured":
				expectedCode = app.ExitUnsatisfied
			}
			if err != nil && cmd.ProcessState.ExitCode() != expectedCode || err == nil && expectedCode != 0 {
				t.Fatalf("exit: %v stderr: %s", err, &stderr)
			}
			if err := validateJSON(t, schema, stdout.Bytes()); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join(fixtureDir, "expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(expected, stdout.Bytes()) {
				t.Fatalf("fixture output differs from expected.json: %s", &stdout)
			}
			consumer := exec.CommandContext(t.Context(), "python3", filepath.Join(root, "examples/check-report.py"))
			consumer.Stdin = bytes.NewReader(stdout.Bytes())
			result, err := consumer.CombinedOutput()
			if (err == nil) != (name == fixtureSupported) {
				t.Fatalf("consumer: %v %s", err, result)
			}
			// Additive unknown properties are accepted and discarded predictably by
			// the DTO reader; older flat-key consumers remain unaffected.
			var generic map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &generic); err != nil {
				t.Fatal(err)
			}
			generic["future_field"] = map[string]any{"nested": true}
			extended, err := json.Marshal(generic)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateJSON(t, schema, extended); err != nil {
				t.Fatal(err)
			}
			report, err := output.Unmarshal(extended)
			if err != nil {
				t.Fatal(err)
			}
			roundtrip, err := output.Marshal(report)
			if err != nil || !bytes.Equal(roundtrip, expected) {
				t.Fatal("unknown-field round trip", err)
			}
		})
	}
}

func TestPublishedCapabilityCatalog(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(findRepoRoot(t), "docs/roadmap.md"))
	if err != nil {
		t.Fatal(err)
	}
	entries := regexp.MustCompile("(?m)^- `([^`]+)`: ").FindAllSubmatch(data, -1)
	if len(entries) == 0 {
		t.Fatal("published catalog is empty")
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		id := string(entry[1])
		if err := model.CapabilityID(id).Validate(); err != nil {
			t.Errorf("catalog: %s: %v", id, err)
		}
		if seen[id] {
			t.Errorf("duplicate catalog entry: %s", id)
		}
		seen[id] = true
	}
	if !seen["context.root"] || !seen["runtime.podman.netavark"] {
		t.Fatal("catalog contract lost required namespaces")
	}
}

func TestConsumerFailsClosedOnIncompleteAndMalformedReports(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"{}", "null", "{", `{"schema_version":2}`, `{"capabilities":{"runtime":{"podman":{"netavark":true}}}}`} {
		cmd := exec.CommandContext(t.Context(), "python3", filepath.Join(findRepoRoot(t), "examples/check-report.py"))
		cmd.Stdin = bytes.NewBufferString(raw)
		if err := cmd.Run(); err == nil {
			t.Fatal("consumer accepted invalid/incomplete report")
		}
	}
}

func TestIdentitySchemaAndDTOBoundsAgree(t *testing.T) {
	t.Parallel()
	const maximumUID = 4294967295
	schema := compileSchema(t)
	expected, err := os.ReadFile(filepath.Join(findRepoRoot(t), fixtureRelativeRoot, "supported", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		value any
		valid bool
	}{
		{nil, true}, {0, true}, {maximumUID, true}, {-1, false}, {maximumUID + 1, false}, {1.5, false}, {false, false},
	}
	for _, field := range []string{"uid", "gid"} {
		for _, tt := range tests {
			var wire map[string]any
			if err := json.Unmarshal(expected, &wire); err != nil {
				t.Fatal(err)
			}
			wire["context"].(map[string]any)[field] = tt.value
			encoded, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			schemaErr := validateJSON(t, schema, encoded)
			_, dtoErr := output.Unmarshal(encoded)
			if (schemaErr == nil) != tt.valid || (dtoErr == nil) != tt.valid {
				t.Errorf("%s=%v schema=%v DTO=%v", field, tt.value, schemaErr, dtoErr)
			}
		}
	}
}
