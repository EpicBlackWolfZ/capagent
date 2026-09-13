package contract_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestInspectionSchemaCollectionAndFields(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := app.Execute(t.Context(), app.Options{Runtime: "podman", PodmanPath: "/usr"}, &stdout, &stderr); code != app.ExitIndeterminate {
		t.Fatal(code)
	}
	schema := compileSchema(t)
	for _, collection := range []string{"passive", "active", testInvalid} {
		r, err := output.Unmarshal(stdout.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		r.Evaluation.Collection = collection
		data, err := output.MarshalCompact(r)
		if err != nil {
			t.Fatal(err)
		}
		if (validateJSON(t, schema, data) == nil) != (collection != testInvalid) {
			t.Fatal("collection schema mismatch", collection)
		}
	}
	r := output.NewReport()
	r.Host.OS, r.Host.CgroupVersion = testUnknown, testUnknown
	rootless, graph := false, ""
	r.Runtimes["podman"] = output.RuntimeInfo{Rootless: &rootless, GraphRoot: &graph}
	data, err := output.MarshalCompact(r)
	if err != nil || validateJSON(t, schema, data) != nil {
		t.Fatal("valid presence rejected", err)
	}
	for _, bad := range []string{
		strings.ReplaceAll(string(data), `"rootless":false`, `"rootless":"false"`),
		strings.ReplaceAll(string(data), `"graph_root":""`, `"graph_root":42`),
	} {
		if validateJSON(t, schema, []byte(bad)) == nil {
			t.Fatal("invalid runtime field type accepted")
		}
	}
}

func TestInspectionFixturesSchemaAndConsumer(t *testing.T) {
	t.Parallel()
	schema, root := compileSchema(t), findRepoRoot(t)
	for _, name := range []string{fixtureSupported, "info-failed", "version-failed", "truncated",
		"missing-field", "version-conflict", "helper-missing"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			dir := filepath.Join(root, fixtureRelativeRoot, "inspection-"+name)
			code := app.Execute(t.Context(), app.Options{Fixture: dir, Pretty: true}, &stdout, &stderr)
			if code != app.ExitSatisfied && code != app.ExitIndeterminate {
				t.Fatal("fixture failed", code, &stderr)
			}
			if err := validateJSON(t, schema, stdout.Bytes()); err != nil {
				t.Fatal(err)
			}
			consumer := exec.CommandContext(t.Context(), "python3", filepath.Join(root, "examples/check-report.py"), "runtime.podman.info")
			consumer.Stdin = bytes.NewReader(stdout.Bytes())
			if err := consumer.Run(); (err == nil) != (code == app.ExitSatisfied) {
				t.Fatal("inspection consumer disagrees", err)
			}
			want, err := os.ReadFile(filepath.Join(dir, "expected.json"))
			if err != nil || !bytes.Equal(want, stdout.Bytes()) {
				t.Fatal("inspection golden mismatch", err)
			}
		})
	}
}

func TestInspectionConsumerRequiresBothPredicates(t *testing.T) {
	t.Parallel()
	for _, state := range []string{fixtureSupported, "unsupported", testUnknown, "unavailable", "misconfigured", ""} {
		var stdout, stderr bytes.Buffer
		opts := app.Options{Fixture: filepath.Join(findRepoRoot(t), fixtureRelativeRoot, "inspection-supported")}
		app.Execute(t.Context(), opts, &stdout, &stderr)
		r, err := output.Unmarshal(stdout.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		cli := r.Capabilities["runtime.podman"]
		cli.State = state
		r.Capabilities["runtime.podman"] = cli
		data, err := output.MarshalCompact(r)
		if err != nil {
			t.Fatal(err)
		}
		consumer := exec.CommandContext(t.Context(), "python3", filepath.Join(findRepoRoot(t), "examples/check-report.py"), "runtime.podman.info")
		consumer.Stdin = bytes.NewReader(data)
		if err := consumer.Run(); (err == nil) != (state == fixtureSupported) {
			t.Fatal("consumer accepted info without a supported CLI", state, err)
		}
	}
}

const testUnknown = "unknown"
const testInvalid = "invalid"
