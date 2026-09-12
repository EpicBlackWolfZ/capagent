// Package app owns application composition and the observation-to-report lifecycle.
// Passive live discovery never constructs a command runner.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/fixture"
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
	Scope        model.EvaluationScope
	Context      model.EvaluationContext
	At           time.Time
	Provenance   string
	Requirement  *requirement.Node
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
	if err := validateRuntimeSelection(input.Scope, dataset.Observations); err != nil {
		return nil, err
	}
	if input.Now != nil {
		input.At = input.Now()
		dataset.At = input.At
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
	probes := []probe.Probe{podman.InfoProbe{Command: services.Command, Timestamp: doc.Timestamp}}
	if doc.Probe == "version" {
		input.Definitions = []capability.Definition{capability.PodmanDefinition()}
		now := func() time.Time { return doc.Timestamp }
		probes = []probe.Probe{podman.DiscoveryProbe{Path: services.Command.Path, Now: now},
			podman.VersionProbe{Command: services.Command, Now: now}}
	}
	return evaluateOwned(ctx, input, services.Environment, probes, services)
}

// Execute writes a validated report, or a bounded generic failure diagnostic.
// Parser/command error strings may contain untrusted data and are not printed.
// --debug emits structured codes and fixed application messages only.
func Execute(ctx context.Context, opts Options, stdout, stderr io.Writer) int {
	if !validOptions(opts) {
		return failure(stderr, ExitUsage, "select --fixture DIR or --runtime podman with current context; active execution is unavailable")
	}
	var report *output.Report
	var err error
	if opts.Runtime != "" {
		report, err = evaluateLive(ctx, opts)
	} else {
		var code int
		report, code = readFixture(ctx, opts, stderr)
		if code != 0 {
			return code
		}
	}
	if err != nil {
		return failure(stderr, ExitExecution, "live evaluation failed")
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
