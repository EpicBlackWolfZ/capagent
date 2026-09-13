package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestCombinedInspectionReplay(t *testing.T) {
	t.Parallel()
	for _, name := range []string{testSupported, "info-failed", "version-failed", "truncated",
		"missing-field", "version-conflict", "helper-missing"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := app.Execute(t.Context(), app.Options{Fixture: "../../testdata/fixtures/v1/inspection-" + name, Debug: true}, &stdout, &stderr)
			want := app.ExitIndeterminate
			if name == testSupported || name == "helper-missing" {
				want = app.ExitSatisfied
			}
			if code != want {
				t.Fatalf("code=%d want=%d: %s", code, want, &stderr)
			}
			r, err := output.Unmarshal(stdout.Bytes())
			if err != nil || r.Validate() != nil {
				t.Fatal("invalid report", err)
			}
			if r.Evaluation.Mode != "fixture" || r.Evaluation.Collection != "" || strings.Contains(stdout.String()+stderr.String(), "PRIVATE") {
				t.Fatal("fixture provenance or sanitization violated")
			}
			if name != "version-failed" && r.Capabilities["runtime.podman"].State != testSupported {
				t.Fatal("CLI version was erased")
			}
			if name == "helper-missing" && r.Capabilities["runtime.podman.netavark"].State != "misconfigured" {
				t.Fatal("helper failure erased engine info")
			}
			if name == "version-failed" {
				for _, obs := range r.Evaluation.Observations {
					if obs.ProbeID == "podman.info" {
						t.Fatal("info ran after version failure")
					}
				}
			}
		})
	}
}
