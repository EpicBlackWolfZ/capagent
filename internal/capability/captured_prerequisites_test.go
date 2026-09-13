package capability_test

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
	"testing"
	"time"
)

func TestCapturedPodmanPrerequisiteReplay(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"nobara-5.8.4-rootless.json"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			const maxCaptureBytes = 2 << 20
			data, err := platform.ReadDocument(t.Context(), "../../testdata/podman/prerequisites", name, maxCaptureBytes)
			if err != nil {
				t.Fatal(err)
			}
			var capture struct {
				Scope        model.EvaluationScope
				Timestamp    time.Time
				RuntimePath  string `json:"runtime_path"`
				Context      model.EvaluationContext
				Info         jsontext.Value
				Observations []model.Observation
				States       map[string]model.CapabilityState
			}
			if err := json.Unmarshal(data, &capture, json.MatchCaseInsensitiveNames(true)); err != nil {
				t.Fatal(err)
			}
			runner := platform.NewFakeCommandRunner()
			command := platform.CommandSpec{Path: capture.RuntimePath, Args: podman.LocalInfoArgs(), Dir: "/", Timeout: podman.InfoTimeout}
			if err := runner.RegisterWithError(command, platform.ExecResult{Stdout: capture.Info}, nil); err != nil {
				t.Fatal(err)
			}
			env := platform.NewEnvironment(nil, nil, nil, runner).WithScope(capture.Scope)
			info, err := (podman.InfoProbe{Command: command, Timestamp: capture.Timestamp}).Run(t.Context(), env)
			if err != nil {
				t.Fatal(err)
			}
			definitions := append(capability.HelperDefinitions(), capability.QuadletDefinitions(capture.Context.Identity.Target.UID != 0)...)
			registry, err := capability.NewRegistry(definitions)
			if err != nil {
				t.Fatal(err)
			}
			dataset := capability.Dataset{RunID: capture.Scope.RunID, At: capture.Timestamp, Contexts: []model.EvaluationContext{capture.Context},
				Observations: append(capture.Observations, info)}
			if err := capability.ValidateDataset(dataset); err != nil {
				t.Fatal(err)
			}
			result, err := registry.Evaluate(capture.Scope, dataset)
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range result.Candidate.Capabilities {
				if got.State != capture.States[string(got.ID)] {
					t.Fatalf("%s: replay %s, captured %s", got.ID, got.State, capture.States[string(got.ID)])
				}
			}
		})
	}
}
