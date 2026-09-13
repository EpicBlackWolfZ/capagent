package podman_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestIncompleteCommandPreservesUnknownAndBlocksInfo(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"drain", "nonzero drain", "transport"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			result := platform.ExecResult{Stdout: []byte("podman version 5.8.4\n")}
			failure := errors.New("unclassified transport failure")
			if scenario != "transport" {
				script := "printf 'podman version 5.8.4\\n'; sleep 1 &"
				if scenario == "nonzero drain" {
					script += " exit 1"
				}
				result, failure = platform.NewOSCommandRunner(time.Second).Run(t.Context(),
					platform.CommandSpec{Path: "/bin/sh", Args: []string{"-c", script}})
				if failure == nil {
					t.Fatal("drain reproduction completed unexpectedly")
				}
			}
			spec := platform.CommandSpec{Path: testPodmanPath, Args: []string{"--version"}}
			runner := runnerFunc(func(_ context.Context, command platform.CommandSpec) (platform.ExecResult, error) {
				if len(command.Args) != 1 {
					t.Fatal("dependent info executed after unsuccessful version")
				}
				return result, failure
			})
			env := platform.NewEnvironment(nil, nil, nil, runner).WithScope(versionScope())
			registry := probe.NewRegistry()
			if err := registry.Register(podman.VersionProbe{Command: spec}); err != nil {
				t.Fatal(err)
			}
			if err := registry.Register(podman.InfoProbe{Command: inspectionSpec(), AfterVersion: true}); err != nil {
				t.Fatal(err)
			}
			orchestrator, err := probe.NewOrchestrator(registry)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range orchestrator.Run(t.Context(), env) {
				if r.ProbeID == "podman.version" && (r.Status != probe.ProbeFailed || r.Observation.Completeness != model.Partial ||
					r.Observation.Version.Runnable != nil) {
					t.Fatal("incomplete version became available/unavailable", r)
				}
				if r.ProbeID == (podman.InfoProbe{}).ID() && r.Status != probe.ProbeSkipped {
					t.Fatal("dependent info not skipped")
				}
			}
			infoRunner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) { return result, failure })
			obs, err := (podman.InfoProbe{Command: inspectionSpec()}).Run(t.Context(), platform.NewEnvironment(nil, nil, nil, infoRunner))
			if err == nil || obs.Completeness != model.Partial || obs.Podman.Available != nil {
				t.Fatal("incomplete info became unavailable", obs)
			}
		})
	}
}
