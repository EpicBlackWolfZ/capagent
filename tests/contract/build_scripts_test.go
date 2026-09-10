package contract_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestBuildScripts_TrustAndReleaseContracts(t *testing.T) {
	t.Parallel()
	const timeout = 90 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-B", "-m", "unittest", "discover", "-s", "scripts/tests", "-v")
	cmd.Dir = findRepoRoot(t)
	release := readWorkflow(t, ".github/workflows/release.yml")
	publisher := release["jobs"].(map[string]any)["publish"].(map[string]any)
	var verifier string
	for _, step := range publisher["steps"].([]any) {
		fields := step.(map[string]any)
		if fields["id"] == "verify-handoff" {
			verifier = fields["run"].(string)
		}
	}
	if verifier == "" {
		t.Fatal("publisher handoff verifier is missing")
	}
	cmd.Env = append(os.Environ(), "CAPAGENT_HANDOFF_SCRIPT="+verifier)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-script contracts: %v\n%s", err, out)
	}
}
