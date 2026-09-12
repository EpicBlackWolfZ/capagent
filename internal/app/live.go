package app

import (
	"context"
	"errors"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func validOptions(opts Options) bool {
	if opts.Active {
		return false
	}
	if opts.Fixture != "" {
		return opts.Runtime == "" && opts.Context == "" && opts.PodmanPath == ""
	}
	return opts.Runtime == "podman" && (opts.Context == "" || opts.Context == "current") &&
		podman.ValidateExecutablePath(opts.PodmanPath) == nil
}

func evaluateLive(ctx context.Context, opts Options) (*output.Report, error) {
	files, err := platform.NewScopedOSReader("/")
	if err != nil {
		return nil, err
	}
	credentials, groupErr := platform.CurrentCredentials()
	report, evalErr := evaluateCurrent(ctx, opts, files, credentials, groupErr, time.Now)
	return report, errors.Join(evalErr, files.Close())
}

// evaluateCurrent borrows the file service until the scheduler has joined.
// The environment deliberately has no command runner, including under UID 0.
func evaluateCurrent(ctx context.Context, opts Options, files platform.ScopedReader,
	credentials model.CurrentCredentials, groupErr error, now func() time.Time,
) (*output.Report, error) {
	scope := model.EvaluationScope{RunID: platform.NewRunID(), ContextID: "current", Runtime: "podman", Endpoint: "local"}
	at := now()
	current, identity := host.ObserveCurrent(ctx, scope, files, credentials, groupErr, at)
	input := Input{Scope: scope, Context: current, At: at, Mode: "live", Provenance: "live", Now: now,
		Requirement: &requirement.Node{Capability: capability.PodmanID}, Observations: []model.Observation{identity},
		Definitions: []capability.Definition{capability.PodmanDefinition()}}
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(scope)
	var probes []probe.Probe
	if credentials.IsValid() == nil {
		probes = []probe.Probe{podman.DiscoveryProbe{Path: opts.PodmanPath, Now: now}}
	}
	report, err := Evaluate(ctx, input, env, probes)
	if report != nil {
		if file := report.Runtimes["podman"].File; file != nil && file.Regular && file.ExecutableBits {
			report.Evaluation.Diagnostics = append(report.Evaluation.Diagnostics, output.Diagnostic{Code: "version_execution_deferred",
				Message: "Podman version execution can modify runtime state; passive discovery does not execute it", Reference: "podman.discovery"})
		}
	}
	return report, err
}
