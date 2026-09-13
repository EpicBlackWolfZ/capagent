// Package app owns application composition and the observation-to-report lifecycle.
// Passive metadata collection has a separate allowlisted command service.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/fixture"
	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const (
	ExitSatisfied     = 0
	ExitUnsatisfied   = 1
	ExitIndeterminate = 2
	ExitUsage         = 64
	ExitExecution     = 70
)

type Options struct {
	Fixture    string
	Pretty     bool
	Debug      bool
	Active     bool
	Runtime    string
	Context    string
	PodmanPath string
}

// Input is a single explicit deployment candidate. All data is borrowed during
// the call; Evaluate snapshots context metadata before starting workers.
type Input struct {
	AssessPodman bool
	Scope        model.EvaluationScope
	Context      model.EvaluationContext
	At           time.Time
	Provenance   string
	Requirement  *requirement.Node
	Collection   string
	Mode         string
	Now          func() time.Time
	Observations []model.Observation
	Definitions  []capability.Definition
}

// Evaluate joins every worker before returning and never closes borrowed
// environment services. Owners may run it concurrently with shared reentrant
// probes/services, then close only after every Evaluate call has returned.
func Evaluate(ctx context.Context, input Input, env platform.Environment, probes []probe.Probe) (*output.Report, error) {
	if input.Scope != env.Scope() || input.Scope.ContextID != input.Context.ID {
		return nil, errors.New("application scope mismatch")
	}
	input.Context = snapshotContext(input.Context)
	dataset := capability.Dataset{RunID: input.Scope.RunID, At: input.At, Contexts: []model.EvaluationContext{input.Context}}
	for _, obs := range input.Observations {
		dataset.Observations = append(dataset.Observations, probe.SnapshotObservation(obs))
	}
	if err := capability.ValidateDataset(dataset); err != nil {
		return nil, err
	}
	registry := probe.NewRegistry()
	for _, p := range probes {
		if err := registry.Register(p); err != nil {
			return nil, err
		}
	}
	orchestrator, err := probe.NewOrchestrator(registry)
	if err != nil {
		return nil, err
	}
	results := orchestrator.Run(ctx, env)
	for _, result := range results {
		// Skipped/cancelled-before-start probes have no measurement. Their absence
		// becomes unknown capability evidence; completed partial observations stay.
		if result.Observation.ID != "" {
			dataset.Observations = append(dataset.Observations, result.Observation)
		}
	}
	dataset.Observations = collectHelpers(ctx, env, dataset.Observations, input)
	if err := validateRuntimeSelection(input.Scope, dataset.Observations); err != nil {
		return nil, err
	}
	if input.Now != nil {
		input.At = input.Now()
		dataset.At = input.At
	}
	if err := capability.ValidateDataset(dataset); err != nil {
		return nil, err
	}
	input.Context = projectContextMeasurements(input.Context, dataset.Observations)
	input.Context.Host, _ = projectHost(input.Context.Host, dataset.Observations)
	dataset.Contexts[0] = input.Context
	if input.Scope.Runtime == "" {
		if input.Requirement != nil || len(input.Definitions) != 0 {
			return nil, errors.New("host collection cannot evaluate requirements")
		}
		report := projectReport(input, dataset.Observations, capability.Evaluation{}, requirement.Result{}, results)
		return report, report.Validate()
	}
	definitions := input.Definitions
	if definitions == nil {
		definitions = []capability.Definition{capability.NetavarkDefinition()}
	}
	capabilities, err := capability.NewRegistry(definitions)
	if err != nil {
		return nil, err
	}
	evaluation, err := capabilities.Evaluate(input.Scope, dataset)
	if err != nil {
		return nil, err
	}
	decision := requirement.Evaluate(input.Requirement, []model.Candidate{evaluation.Candidate})
	report := projectReport(input, dataset.Observations, evaluation, decision, results)
	if err := report.Validate(); err != nil {
		return nil, err
	}
	return report, nil
}

func validateRuntimeSelection(scope model.EvaluationScope, observations []model.Observation) error {
	var selected string
	for _, obs := range observations {
		if obs.Scope != scope {
			return errors.New("application observation scope mismatch")
		}
		var paths []string
		if obs.Discovery != nil {
			paths = append(paths, obs.Discovery.Path)
		}
		if obs.Version != nil {
			paths = append(paths, obs.Version.Path)
		}
		if obs.Podman != nil {
			paths = append(paths, obs.Podman.Path)
		}
		if obs.PodmanHelper != nil {
			paths = append(paths, obs.PodmanHelper.Path)
		}
		if obs.Executable != nil {
			paths = append(paths, obs.Executable.RuntimePath)
		}
		for _, path := range paths {
			if path == "" {
				continue
			}
			if selected != "" && selected != path {
				return errors.New("application executable selection mismatch")
			}
			selected = path
		}
	}
	return nil
}

func evaluateOwned(
	ctx context.Context, input Input, env platform.Environment, probes []probe.Probe, owner io.Closer,
) (*output.Report, error) {
	report, err := Evaluate(ctx, input, env, probes)
	closeErr := owner.Close()
	return report, errors.Join(err, closeErr)
}

func evaluateFixture(ctx context.Context, doc *fixture.Document) (*output.Report, error) {
	services, err := fixture.Open(doc)
	if err != nil {
		return nil, err
	}
	input := Input{Scope: doc.Scope(), Context: doc.Context, At: doc.Timestamp, Provenance: doc.Provenance.Kind,
		Requirement: services.Requirement}
	if doc.Probe == "assessment" {
		return evaluateAssessmentFixture(ctx, doc, input, services)
	}
	if doc.Probe == "context" {
		now := func() time.Time { return doc.Timestamp }
		user := host.UserContextProbe{Target: *doc.Context.Identity.Target, Now: now, Active: doc.UserQuery}
		if doc.Context.Identity.XDGRuntimeDir != "" {
			user.RuntimeDirectory = &doc.Context.Identity.XDGRuntimeDir
		}
		probes := []probe.Probe{host.SubIDProbe{Target: *doc.Context.Identity.Target, Now: now}, user}
		return evaluateOwned(ctx, input, services.Environment, probes, services)
	}
	if doc.Probe == "host" {
		now := func() time.Time { return doc.Timestamp }
		return evaluateOwned(ctx, input, services.Environment, host.Probes(now), services)
	}
	probes := []probe.Probe{podman.InfoProbe{Command: services.Command, LegacyInfo: true, Timestamp: doc.Timestamp}}
	if doc.Probe == "version" {
		input.Definitions = []capability.Definition{capability.PodmanDefinition()}
		now := func() time.Time { return doc.Timestamp }
		probes = []probe.Probe{podman.DiscoveryProbe{Path: services.Command.Path, Now: now},
			podman.VersionProbe{Command: services.Command, Now: now}}
	}
	if doc.Probe == "inspection" {
		input.Definitions = []capability.Definition{capability.PodmanDefinition(),
			capability.PodmanInfoDefinition(), capability.NetavarkDefinition()}
		now := func() time.Time { return doc.Timestamp }
		discovery, _ := (podman.DiscoveryProbe{Path: services.Command.Path, Now: now}).Run(ctx, services.Environment)
		// Discovery failures are retained in the observation and prevent dispatch.
		input.Observations = []model.Observation{discovery}
		probes = nil
		if d := discovery.Discovery; d != nil && d.File != nil && d.File.Regular && d.File.ExecutableBits {
			probes = []probe.Probe{podman.VersionProbe{Command: services.Command, Now: now},
				podman.InfoProbe{Command: services.InfoCommand, Now: now, AfterVersion: true}}
		}
	}
	return evaluateOwned(ctx, input, services.Environment, probes, services)
}

// Execute writes a validated report, or a bounded generic failure diagnostic.
// Parser/command error strings may contain untrusted data and are not printed.
// --debug emits structured codes and fixed application messages only.
func Execute(ctx context.Context, opts Options, stdout, stderr io.Writer) int {
	ctx, cancel := platform.TargetContext(ctx)
	defer cancel()
	if !validOptions(opts) {
		return failure(stderr, ExitUsage, "use --json for host facts, --runtime podman or --fixture DIR; "+
			"--active permits live user-manager queries and local runtime inspection")
	}
	var report *output.Report
	var err error
	if opts.Fixture == "" {
		report, err = evaluateLive(ctx, opts)
	} else {
		var code int
		report, code = readFixture(ctx, opts, stderr)
		if code != 0 {
			return code
		}
	}
	if err != nil {
		return failure(stderr, ExitExecution, executionMessage(err))
	}
	return writeReport(report, opts, stdout, stderr)
}

func readFixture(ctx context.Context, opts Options, stderr io.Writer) (*output.Report, int) {
	data, err := platform.ReadDocument(ctx, opts.Fixture, "fixture.json", fixture.MaxBytes)
	if err != nil {
		return nil, failure(stderr, ExitExecution, "cannot read fixture document")
	}
	doc, err := fixture.Parse(data)
	if err != nil {
		return nil, failure(stderr, ExitUsage, "invalid fixture document")
	}
	report, err := evaluateFixture(ctx, doc)
	if err != nil {
		return nil, failure(stderr, ExitExecution, "fixture evaluation failed")
	}
	return report, 0
}

func writeReport(report *output.Report, opts Options, stdout, stderr io.Writer) int {
	encode := output.MarshalCompact
	if opts.Pretty {
		encode = output.Marshal
	}
	data, err := encode(report)
	if err != nil {
		return failure(stderr, ExitExecution, "report encoding failed")
	}
	if opts.Debug {
		for _, diagnostic := range report.Evaluation.Diagnostics {
			if _, err := fmt.Fprintf(stderr, "capagent: %s\n", diagnostic.Code); err != nil {
				return ExitExecution
			}
		}
	}
	if _, err := stdout.Write(data); err != nil {
		return failure(stderr, ExitExecution, "cannot write report")
	}
	if report.Evaluation.Scope.Runtime == "" {
		return hostExit(report)
	}
	return exitCode(report.Evaluation.Requirement.State)
}
func failure(stderr io.Writer, code int, message string) int {
	if _, err := fmt.Fprintf(stderr, "capagent: %s\n", message); err != nil {
		return ExitExecution
	}
	return code
}
func exitCode(state string) int {
	switch state {
	case "SATISFIED":
		return ExitSatisfied
	case "UNSATISFIED":
		return ExitUnsatisfied
	default:
		return ExitIndeterminate
	}
}

func collectHelpers(ctx context.Context, env platform.Environment, observations []model.Observation, input Input) []model.Observation {
	count := len(observations)
	for i := range count {
		obs := observations[i]
		if input.AssessPodman {
			for _, p := range podman.SelectedHelperProbes(obs, input.Now) {
				if p.Now == nil {
					p.Now = func() time.Time { return input.At }
				}
				helper, _ := p.Run(ctx, env) // Failure remains in the partial observation and fixed diagnostics.
				observations = append(observations, helper)
			}
		}
		p := obs.Podman
		if p == nil || p.Available == nil || !*p.Available || p.NetworkBackend == nil || *p.NetworkBackend != "netavark" {
			continue
		}
		at := input.At
		if input.Now != nil {
			at = input.Now()
		}
		helper, _ := podman.ObserveHelper(ctx, env, obs, at) // Failure is retained in the partial helper observation and diagnostics.
		observations = append(observations, helper)
	}
	return observations
}

func executionMessage(err error) string {
	if errors.Is(err, platform.ErrSymlinkUnsupported) {
		return "host confinement unavailable: openat2 requires Linux 5.6+"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "required host access blocked by execution policy"
	}
	return "live evaluation failed"
}
