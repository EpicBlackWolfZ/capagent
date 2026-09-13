package contract_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
)

func TestHostFixtureSchemaAndConsumer(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(findRepoRoot(t), "testdata/fixtures/v1/host-basic")
	var out, stderr bytes.Buffer
	code := app.Execute(t.Context(), app.Options{Fixture: dir, Pretty: true}, &out, &stderr)
	if code != 0 {
		t.Fatalf("host collection: %d %s", code, stderr.String())
	}
	if err := validateJSON(t, compileSchema(t), out.Bytes()); err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), expected) {
		t.Fatal("host fixture output drift")
	}
	if bytes.Contains(out.Bytes(), []byte(`"requirement"`)) {
		t.Fatal("host fixture contains deployment verdict")
	}
}

func TestContextFixtureSchema(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"context-rootless", "context-user-active"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(findRepoRoot(t), "testdata/fixtures/v1/"+name)
			var out, stderr bytes.Buffer
			if code := app.Execute(t.Context(), app.Options{Fixture: dir, Pretty: true}, &out, &stderr); code != app.ExitIndeterminate {
				t.Fatal(code, stderr.String())
			}
			if err := validateJSON(t, compileSchema(t), out.Bytes()); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join(dir, "expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), expected) {
				t.Fatal("context fixture output drift")
			}
		})
	}
}
