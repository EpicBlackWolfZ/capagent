package fixture_test

import (
	"github.com/EpicBlackWolfZ/capagent/internal/fixture"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"testing"
)

func TestAssessmentCommandAuthority(t *testing.T) {
	t.Parallel()
	data, err := platform.ReadDocument(t.Context(), "../../testdata/fixtures/v1/assessment-rootless", "fixture.json", fixture.MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "helper execution", "environment injection", "info environment mismatch",
		"passive execution", "target mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			d, err := fixture.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "helper execution":
				d.Commands[0].Path = "/usr/bin/crun"
			case "environment injection":
				d.Environment["CONTAINERS_CONF"] = "/tmp/private"
			case "info environment mismatch":
				d.Commands[1].Environment["HOME"] = "/root"
			case "passive execution":
				d.Active = false
				d.UserQuery = false
			case "target mismatch":
				d.Context.Identity.Execution.UID = 0
			}
			s, err := fixture.Open(d)
			if s != nil {
				defer s.Close()
			}
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("authority result: %v", err)
			}
		})
	}
}
