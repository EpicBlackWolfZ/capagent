package contract_test

import (
	"bytes"
	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"os"
	"path/filepath"
	"testing"
)

func TestPodmanPrerequisitesSchemaAndGolden(t *testing.T) {
	t.Parallel()
	schema, root := compileSchema(t), findRepoRoot(t)
	for _, name := range []string{"rootless", "rootful", "generator-absent", "generator-denied", "generator-masked",
		"helper-denied", "manager-deferred", "runtime-invalid"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(root, fixtureRelativeRoot, "assessment-"+name)
			var out, diagnostics bytes.Buffer
			if code := app.Execute(t.Context(), app.Options{Fixture: dir, Pretty: true}, &out, &diagnostics); code > app.ExitIndeterminate {
				t.Fatal("assessment execution failure", code, &diagnostics)
			}
			if err := validateJSON(t, schema, out.Bytes()); err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(dir, "expected.json"))
			if err != nil || !bytes.Equal(want, out.Bytes()) {
				t.Fatal("assessment golden mismatch", err)
			}
		})
	}
}
