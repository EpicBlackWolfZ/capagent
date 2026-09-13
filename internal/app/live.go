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
	if opts.Fixture != "" {
		return !opts.Active && opts.Runtime == "" && opts.Context == "" && opts.PodmanPath == ""
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

// evaluateCurrent borrows services until all command workers have joined.
func evaluateCurrent(ctx context.Context, opts Options, files platform.ScopedReader,
	credentials model.CurrentCredentials, groupErr error, now func() time.Time,
) (*output.Report, error) {
	return evaluateCurrentServices(ctx, opts, currentServices{files: files, credentials: credentials, groupErr: groupErr, now: now,
		capture: func() (platform.EnvPolicy, error) { return platform.NewEnvPolicy(podman.InspectionEnvNames(), nil) },
		runner:  func() platform.CommandRunner { return platform.NewOSCommandRunner(podman.InfoTimeout) }})
}

type currentServices struct {
	files       platform.ScopedReader
	credentials model.CurrentCredentials
	groupErr    error
	now         func() time.Time
	capture     func() (platform.EnvPolicy, error)
	runner      func() platform.CommandRunner
}

func evaluateCurrentServices(ctx context.Context, opts Options, services currentServices) (*output.Report, error) {
	if opts.Active {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, podman.InspectionTimeout)
		defer cancel()
	}
	scope := model.EvaluationScope{RunID: platform.NewRunID(), ContextID: "current", Runtime: "podman", Endpoint: "local"}
	at := services.now()
	current, identity := host.ObserveCurrent(ctx, scope, services.files, services.credentials, services.groupErr, at)
	input := Input{Scope: scope, Context: current, At: at, Mode: "live", Provenance: "live", Now: services.now, Collection: "passive",
		Requirement: &requirement.Node{Capability: capability.PodmanID}, Observations: []model.Observation{identity},
		Definitions: []capability.Definition{capability.PodmanDefinition()}}
	if opts.Active {
		input.Collection = "active"
		input.Requirement = &requirement.Node{All: []*requirement.Node{{Capability: capability.PodmanID}, {Capability: capability.PodmanInfoID}}}
		input.Definitions = append(input.Definitions, capability.PodmanInfoDefinition(), capability.NetavarkDefinition())
	}
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(services.files).WithScope(scope)
	var probes []probe.Probe
	var diagnostics []output.Diagnostic
	if services.credentials.IsValid() == nil && ctx.Err() == nil {
		discovery, discoveryErr := (podman.DiscoveryProbe{Path: opts.PodmanPath, Now: services.now}).Run(ctx, env)
		input.Observations = append(input.Observations, discovery)
		if discoveryErr != nil {
			diagnostics = append(diagnostics, liveDiagnostic("discovery_failed"))
		}
		d := discovery.Discovery
		if d != nil && d.File != nil && d.File.Regular && d.File.ExecutableBits {
			if opts.Active {
				var setupErr error
				env, probes, setupErr = activeProbes(ctx, services, current, env, d.Path)
				if setupErr != nil {
					diagnostics = append(diagnostics, liveDiagnostic("inspection_environment_unavailable"))
				}
			} else {
				diagnostics = append(diagnostics, output.Diagnostic{Code: "version_execution_deferred",
					Message: "Podman version execution can modify runtime state; passive discovery does not execute it", Reference: "podman.discovery"})
			}
		}
	}
	report, err := Evaluate(ctx, input, env, probes)
	if report != nil {
		report.Evaluation.Diagnostics = append(report.Evaluation.Diagnostics, diagnostics...)
	}
	return report, err
}

func activeProbes(ctx context.Context, services currentServices, current model.EvaluationContext,
	env platform.Environment, executable string,
) (platform.Environment, []probe.Probe, error) {
	if !services.credentials.GroupsKnown || services.groupErr != nil || ctx.Err() != nil {
		return env, nil, errors.New("current execution credentials are incomplete")
	}
	captured, err := services.capture()
	if err != nil {
		return env, nil, err
	}
	commands, err := podman.PrepareInspection(ctx, services.files, *current.Identity.Current, executable, captured)
	if err != nil {
		return env, nil, err
	}
	activeEnv := platform.NewEnvironment(nil, nil, nil, services.runner()).WithFiles(services.files).WithScope(env.Scope())
	probes := []probe.Probe{podman.VersionProbe{Command: commands.Version, Now: services.now},
		podman.InfoProbe{Command: commands.Info, Now: services.now, AfterVersion: true}}
	return activeEnv, probes, nil
}

func liveDiagnostic(code string) output.Diagnostic {
	return output.Diagnostic{Code: code, Message: "current runtime collection is incomplete"}
}
