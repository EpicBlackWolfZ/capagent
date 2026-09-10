package contract_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestWorkflow_FinalHardeningGate(t *testing.T) {
	t.Parallel()
	ci := readWorkflow(t, ".github/workflows/ci.yml")
	jobs := ci["jobs"].(map[string]any)
	gate, ok := jobs["hardening-gate"].(map[string]any)
	if !ok {
		t.Fatal("missing final M1.1 aggregate check")
	}
	if gate["if"] != "always()" {
		t.Fatal("aggregate must diagnose failed/missing prerequisites")
	}
	for _, name := range []string{"lint", "test", "vulncheck", "gitleaks", "build", "fuzz"} {
		if !strings.Contains(fmt.Sprint(gate["needs"]), name) {
			t.Errorf("aggregate does not require %s", name)
		}
		job := jobs[name].(map[string]any)
		if !strings.Contains(fmt.Sprint(job["steps"]), "scripts/gate.py stage --stage "+name) {
			t.Errorf("%s bypasses shared gate component", name)
		}
	}
	steps := fmt.Sprint(gate["steps"])
	for _, required := range []string{"scripts/gate.py report", "CAPAGENT_JOB_RESULTS", "include-hidden-files:true", "always()"} {
		if !strings.Contains(steps, required) {
			t.Errorf("aggregate missing %s", required)
		}
	}
	nightly := readWorkflow(t, ".github/workflows/hardening.yml")
	fuzz, ok := nightly["jobs"].(map[string]any)["fuzz"].(map[string]any)
	if !ok || fuzz["timeout-minutes"] != 30 {
		t.Fatal("missing bounded nightly fuzz job")
	}
	if !strings.Contains(fmt.Sprint(fuzz["steps"]), "scripts/fuzzing.py stress") {
		t.Fatal("nightly does not run deep fuzzing")
	}
	release := readWorkflow(t, ".github/workflows/release.yml")
	build := release["jobs"].(map[string]any)["build"].(map[string]any)
	if !strings.Contains(fmt.Sprint(build["steps"]), "scripts/gate.py report") {
		t.Fatal("publisher is not gated by aggregate verification")
	}
}

func TestArchitecture_FuzzHelpersStayTestOnly(t *testing.T) {
	t.Parallel()
	imports, err := collectPackageImports(findRepoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for packageName, names := range imports {
		for _, name := range names {
			if name == modulePrefix+"tests/fuzzutil" {
				t.Errorf("production package %s imports fuzz tooling", packageName)
			}
		}
	}
}
