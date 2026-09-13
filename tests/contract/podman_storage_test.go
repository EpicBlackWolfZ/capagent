package contract_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestStorageReportIncludesScopedSourceAndPathEvidence(t *testing.T) {
	t.Parallel()
	var out, diagnostics bytes.Buffer
	dir := filepath.Join(findRepoRoot(t), fixtureRelativeRoot, "engine-rootless")
	code := app.Execute(t.Context(), app.Options{Fixture: dir}, &out, &diagnostics)
	if code != app.ExitSatisfied {
		t.Fatalf("existing engine requirement changed: %d %s", code, &diagnostics)
	}
	report, err := output.Unmarshal(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if report.Capabilities["runtime.podman.config.storage.parsed"].State != "supported" {
		t.Fatal("missing selected storage assessment")
	}
	source, metadata := false, false
	for _, obs := range report.Evaluation.Observations {
		if obs.Configuration != nil && obs.Configuration.Family == "storage" {
			source = true
		}
		if obs.StoragePaths != nil {
			metadata = true
		}
	}
	if !source || !metadata {
		t.Fatal("storage evidence absent from report")
	}
	if err := validateJSON(t, compileSchema(t), out.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestStorageAssessmentFixtures(t *testing.T) {
	t.Parallel()
	schema, root := compileSchema(t), findRepoRoot(t)
	for _, test := range []struct {
		name string
		exit int
	}{
		{"rootless", 0}, {"rootful", 0}, {"replacement", 1}, {"helper-denied", 2}, {"helper-missing", 1}, {"source-denied", 2},
		{"malformed", 1}, {"runtime-override", 1}, {"missing-root", 2}, {"missing-additional", 1}, {"read-only", 1}, {"legacy-roots", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(root, fixtureRelativeRoot, "storage-"+test.name)
			var out, diagnostics bytes.Buffer
			code := app.Execute(t.Context(), app.Options{Fixture: dir, Pretty: true}, &out, &diagnostics)
			if code != test.exit {
				t.Fatalf("exit=%d want=%d: %s", code, test.exit, &diagnostics)
			}
			if err := validateJSON(t, schema, out.Bytes()); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join(dir, "expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), expected) {
				t.Fatal("storage assessment golden mismatch")
			}
		})
	}
}
