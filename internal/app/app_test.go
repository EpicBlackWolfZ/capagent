package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestFixtureCLIResults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, state string
		code        int
	}{
		{testSupported, "SATISFIED", app.ExitSatisfied}, {"unsupported", "UNSATISFIED", app.ExitUnsatisfied},
		{"misconfigured", "UNSATISFIED", app.ExitUnsatisfied}, {"unavailable", "INDETERMINATE", app.ExitIndeterminate},
		{"unknown", "INDETERMINATE", app.ExitIndeterminate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			opts := app.Options{Fixture: "../../testdata/fixtures/v1/" + tt.name, Pretty: true, Debug: true}
			code := app.Execute(t.Context(), opts, &stdout, &stderr)
			if code != tt.code {
				t.Fatalf("code=%d stderr=%s", code, &stderr)
			}
			report, err := output.Unmarshal(stdout.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if err := report.Validate(); err != nil {
				t.Fatal(err)
			}
			if report.Evaluation.Requirement.State != tt.state || report.Capabilities["runtime.podman.netavark"].State != tt.name {
				t.Fatalf("wrong report: %s", &stdout)
			}
			wantObservations := 1
			if tt.name == testSupported || tt.name == "misconfigured" {
				wantObservations = 2
			}
			if len(report.Evaluation.Observations) != wantObservations || len(report.Evaluation.Evidence) != 1 {
				t.Fatal("missing provenance")
			}
			for _, forbidden := range []string{"raw_data", "stdout", "stderr", "home_dir"} {
				if strings.Contains(stdout.String(), `"`+forbidden+`"`) {
					t.Fatal("raw command or context leaked")
				}
			}
			var second bytes.Buffer
			if again := app.Execute(t.Context(), opts, &second, &stderr); again != code || !bytes.Equal(stdout.Bytes(), second.Bytes()) {
				t.Fatal("fixture report is not deterministic")
			}
		})
	}
}

func TestFixtureCLIRejectsLiveAndMissingInput(t *testing.T) {
	t.Parallel()
	for _, opts := range []app.Options{{}, {Active: true, Fixture: "ignored"}, {Fixture: "missing"}} {
		var stdout, stderr bytes.Buffer
		if code := app.Execute(t.Context(), opts, &stdout, &stderr); code != app.ExitUsage && code != app.ExitExecution {
			t.Fatal("unsupported invocation succeeded", code)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatal("error stream contract")
		}
	}
}

const testSupported = "supported"
