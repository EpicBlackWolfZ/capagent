package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

func TestLiveFilesystemExecution(t *testing.T) {
	t.Parallel()
	// This invokes only capagent's passive filesystem path. An existing directory
	// is deliberately selected, so no installed runtime or external process is needed.
	var out, diagnostics bytes.Buffer
	code := Execute(t.Context(), Options{Runtime: testPodmanRuntime, PodmanPath: "/usr", Debug: true}, &out, &diagnostics)
	if code != ExitIndeterminate {
		t.Fatalf("exit=%d diagnostic=%s", code, &diagnostics)
	}
	r, err := output.Unmarshal(out.Bytes())
	if err != nil || r.Validate() != nil {
		t.Fatal("invalid live report", err)
	}
	if r.Evaluation.Mode != testLiveMode || r.Evaluation.Current == nil || r.Evaluation.Target == nil {
		t.Fatal("missing live identity")
	}
	if r.Runtimes[testPodmanRuntime].Installed != nil {
		t.Fatal("directory asserted installed CLI")
	}
}

func TestEvaluationTimeFollowsCollection(t *testing.T) {
	t.Parallel()
	input := evaluationInput()
	input.Mode, input.Provenance = testLiveMode, testLiveMode
	input.Requirement = &requirement.Node{Capability: capability.PodmanID}
	input.Definitions = []capability.Definition{capability.PodmanDefinition()}
	measured := input.At.Add(time.Second)
	input.Now = func() time.Time { return measured.Add(time.Second) }
	available := true
	p := evaluationProbe{run: func(context.Context, platform.Environment) (model.Observation, error) {
		obs := measurement(input)
		obs.Podman = nil
		obs.Timestamp = measured
		obs.Facts[0].Timestamp = measured
		obs.Version = &model.PodmanVersionObservation{Path: "/usr/bin/podman", Runnable: &available,
			Version: &model.PodmanVersion{Canonical: "5.8.4"}}
		return obs, nil
	}}
	r, err := Evaluate(t.Context(), input, platform.NewEnvironment(nil, nil, nil, nil).WithScope(input.Scope), []probe.Probe{p})
	if err != nil || r.Evaluation.Requirement.State != "SATISFIED" || r.Evaluation.Timestamp.Before(measured) {
		t.Fatal("fresh evidence rejected", r, err)
	}
}

func TestEvaluationRejectsMixedExecutables(t *testing.T) {
	t.Parallel()
	input := evaluationInput()
	obs := measurement(input)
	obs.Podman = nil
	obs.Discovery = &model.RuntimeDiscovery{Path: "/usr/bin/podman"}
	obs.Version = &model.PodmanVersionObservation{Path: "/opt/podman"}
	input.Observations = []model.Observation{obs}
	if _, err := Evaluate(t.Context(), input, platform.NewEnvironment(nil, nil, nil, nil).WithScope(input.Scope), nil); err == nil {
		t.Fatal("combined different executable identities")
	}
}
