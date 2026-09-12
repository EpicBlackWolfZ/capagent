package app_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestVersionFixtures(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name, state string
		code        int
	}{
		{"version-supported", "supported", app.ExitSatisfied},
		{"version-nonzero", "unavailable", app.ExitIndeterminate},
		{"version-malformed", "unknown", app.ExitIndeterminate},
		{"version-timeout", "unknown", app.ExitIndeterminate},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			var out, diagnostics bytes.Buffer
			code := app.Execute(t.Context(), app.Options{Fixture: "../../testdata/fixtures/v1/" + scenario.name, Debug: true}, &out, &diagnostics)
			if code != scenario.code {
				t.Fatalf("exit %d: %s", code, &diagnostics)
			}
			r, err := output.Unmarshal(out.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if r.Capabilities["runtime.podman"].State != scenario.state || r.Evaluation.Mode != "fixture" {
				t.Fatal("wrong version outcome", out.String())
			}
			if r.Runtimes["podman"].Accessible != nil {
				t.Fatal("version asserted engine access")
			}
			if strings.Contains(out.String(), "SECRET") || strings.Contains(diagnostics.String(), "SECRET") {
				t.Fatal("raw command data escaped")
			}
		})
	}
}
