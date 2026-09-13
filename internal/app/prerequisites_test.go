package app_test

import (
	"bytes"
	"github.com/EpicBlackWolfZ/capagent/internal/app"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"testing"
)

func TestAssessmentPrerequisiteReplay(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name              string
		exit              int
		capability, state string
	}{
		{"rootless", 0, "runtime.podman.quadlet.manager", "supported"},
		{"rootful", 0, "runtime.podman.quadlet.manager", "supported"},
		{"generator-absent", 1, "runtime.podman.quadlet.generator", "unsupported"},
		{"generator-denied", 2, "runtime.podman.quadlet.generator", string(model.StateUnknown)},
		{"generator-masked", 1, "runtime.podman.quadlet.generator", string(model.StateMisconfigured)},
		{"helper-denied", 2, "runtime.podman.oci_runtime.executable", string(model.StateUnknown)},
		{"manager-deferred", 2, "runtime.podman.quadlet.manager", string(model.StateUnknown)},
		{"runtime-invalid", 1, "runtime.podman.quadlet.runtime_directory", string(model.StateMisconfigured)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var out, diagnostics bytes.Buffer
			code := app.Execute(t.Context(), app.Options{Fixture: "../../testdata/fixtures/v1/assessment-" + test.name}, &out, &diagnostics)
			if code != test.exit {
				t.Fatalf("exit=%d want=%d: %s", code, test.exit, &diagnostics)
			}
			r, err := output.Unmarshal(out.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if r.Capabilities[test.capability].State != test.state {
				t.Fatal(r.Capabilities[test.capability])
			}
			for id := range r.Capabilities {
				if id == "runtime.podman.quadlet.operational" {
					t.Fatal("invented operational proof")
				}
			}
		})
	}
}
