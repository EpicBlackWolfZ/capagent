package contract_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
)

func TestNetworkAssessmentFixtures(t *testing.T) {
	t.Parallel()
	schema, root := compileSchema(t), findRepoRoot(t)
	for _, test := range []struct {
		name string
		exit int
	}{
		{"rootless", 0}, {"rootful", 0}, {"legacy-rootless", 0}, {"helper-missing", 1}, {"helper-denied", 2},
		{"source-denied", 2}, {"invalid", 1}, {"dropin-cni", 1}, {"runtime-conflict", 1}, {"unqualified", 2}, {"opaque-pasta", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(root, fixtureRelativeRoot, "network-"+test.name)
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
				t.Fatal("network assessment golden mismatch")
			}
			if bytes.Contains(out.Bytes(), []byte("synthetic-secret")) || bytes.Contains(diagnostics.Bytes(), []byte("synthetic-secret")) {
				t.Fatal("unrecognized network configuration leaked")
			}
		})
	}
}
