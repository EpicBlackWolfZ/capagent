package contract_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestPodmanEngineAssessment(t *testing.T) {
	t.Parallel()
	schema, root := compileSchema(t), findRepoRoot(t)
	for _, test := range []struct {
		name string
		exit int
	}{
		{"rootless", 0}, {"rootful", 0}, {"malformed", 1}, {"denied", 2}, {"unqualified", 2}, {"runtime-override", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var out, diagnostics bytes.Buffer
			dir := filepath.Join(root, fixtureRelativeRoot, "engine-"+test.name)
			code := app.Execute(t.Context(), app.Options{Fixture: dir, Pretty: true}, &out, &diagnostics)
			if code != test.exit {
				t.Fatalf("exit=%d want=%d: %s", code, test.exit, &diagnostics)
			}
			if err := validateJSON(t, schema, out.Bytes()); err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(dir, "expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatal("engine assessment differs from golden report")
			}
			if bytes.Contains(out.Bytes(), []byte("synthetic-secret")) {
				t.Fatal("configuration value leaked into report")
			}
			report, err := output.Unmarshal(out.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, observation := range report.Evaluation.Observations {
				if observation.Configuration != nil {
					found = true
					if observation.Configuration.RuntimePath != "/usr/bin/podman" {
						t.Fatal("configuration runtime selection lost")
					}
				}
			}
			if !found {
				t.Fatal("missing configuration provenance")
			}
		})
	}
}
