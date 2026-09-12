package contract_test

import (
	"bytes"
	json "encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestVersionFixtureSchemaAndConsumer(t *testing.T) {
	t.Parallel()
	schema := compileSchema(t)
	root := findRepoRoot(t)
	for _, name := range []string{"version-supported", "version-nonzero", "version-malformed", "version-timeout"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			fixture := filepath.Join(root, fixtureRelativeRoot, name)
			code := app.Execute(t.Context(), app.Options{Fixture: fixture, Pretty: true}, &stdout, &stderr)
			if code != 0 && code != app.ExitIndeterminate {
				t.Fatalf("fixture: %d %s", code, &stderr)
			}
			if err := validateJSON(t, schema, stdout.Bytes()); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join(fixture, "expected.json"))
			if err != nil || !bytes.Equal(stdout.Bytes(), expected) {
				t.Fatal("version golden mismatch", err)
			}
			consumer := exec.CommandContext(t.Context(), "python3", filepath.Join(root, "examples/check-report.py"), "runtime.podman")
			consumer.Stdin = bytes.NewReader(stdout.Bytes())
			if err := consumer.Run(); (err == nil) != (name == "version-supported") {
				t.Fatal("CLI consumer verdict", err)
			}
		})
	}
}

func TestLiveSchemaModeAndProvenance(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := app.Execute(t.Context(), app.Options{Runtime: "podman", PodmanPath: "/usr"}, &stdout, &stderr)
	if code != app.ExitIndeterminate {
		t.Fatal(code, stderr.String())
	}
	schema := compileSchema(t)
	if err := validateJSON(t, schema, stdout.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"fixture", "live"} {
		for _, provenance := range []string{"captured", "synthetic", "live", "invalid"} {
			r, err := output.Unmarshal(stdout.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			r.Evaluation.Mode, r.Evaluation.Provenance = mode, provenance
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			valid := mode == "live" && provenance == "live" || mode == "fixture" && (provenance == "captured" || provenance == "synthetic")
			if (r.Validate() == nil) != valid || (validateJSON(t, schema, data) == nil) != valid {
				t.Fatal("schema/DTO mode disagreement", mode, provenance)
			}
		}
	}
}
