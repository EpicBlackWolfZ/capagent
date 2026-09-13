package app

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestPodmanUnresolvedTargetRemainsIndeterminate(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{"user:absent", "user:root"} {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			services := targetServices(t, 1000)
			services.runner = func() platform.CommandRunner {
				t.Fatal("constructed runtime command authority without the selected target")
				return nil
			}
			for _, active := range []bool{false, true} {
				report, err := evaluateCurrentServices(t.Context(), Options{Runtime: testPodmanRuntime, Context: selector, Active: active}, services)
				if err != nil {
					t.Fatal(err)
				}
				if report.Evaluation.Requirement.State != testIndeterminate || report.Evaluation.Execution != nil {
					t.Fatal("unavailable target became an executable assessment")
				}
				if selector == "user:absent" && report.Evaluation.Target != nil {
					t.Fatal("absent target fell back to the current user")
				}
				if err := report.Validate(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
