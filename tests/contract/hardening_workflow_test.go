package contract_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestWorkflow_Hardening(t *testing.T) {
	t.Parallel()
	ci := readWorkflow(t, ".github/workflows/ci.yml")
	testJob := ci["jobs"].(map[string]any)["test"].(map[string]any)
	steps := fmt.Sprint(testJob["steps"])
	for _, required := range []string{
		"scripts/gate.py stage --stage test", ".work/gate/", "include-hidden-files:true",
	} {
		if !strings.Contains(steps, required) {
			t.Errorf("ordinary CI missing %s", required)
		}
	}
	nightly := readWorkflow(t, ".github/workflows/hardening.yml")
	permissions := nightly["permissions"].(map[string]any)
	if len(permissions) != 1 || permissions["contents"] != "read" {
		t.Fatal("nightly must be read-only")
	}
	triggers := nightly["on"].(map[string]any)
	if !strings.Contains(fmt.Sprint(triggers["schedule"]), "23 2 * * *") || triggers["workflow_dispatch"] == nil {
		t.Fatal("missing nightly/manual triggers")
	}
	job := nightly["jobs"].(map[string]any)["stress"].(map[string]any)
	if job["timeout-minutes"] != 20 {
		t.Fatal("nightly job must have a 20-minute watchdog")
	}
	for _, required := range []string{
		"make hardening-stress", "always()", "retention-days:14", "include-hidden-files:true", "persist-credentials:false",
	} {
		if !strings.Contains(fmt.Sprint(job["steps"]), required) {
			t.Errorf("nightly missing %s", required)
		}
	}
}
