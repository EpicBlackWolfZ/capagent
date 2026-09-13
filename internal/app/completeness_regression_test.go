package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

type contextCompletionProbe struct {
	id           string
	dependencies []string
	run          func(context.Context, platform.Environment) (model.Observation, error)
}

func (p contextCompletionProbe) ID() string             { return p.id }
func (p contextCompletionProbe) Dependencies() []string { return p.dependencies }
func (p contextCompletionProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	return p.run(ctx, env)
}

func TestRequiredContextCollectionControlsReportExit(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"complete", "cancel before dispatch", "failed without observation", "retained partial",
		"dependency skipped", "unrequested", "deferred query"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			input := evaluationInput()
			input.Scope.Runtime, input.Scope.Endpoint, input.Requirement = "", "", nil
			input.Mode, input.Provenance = testLiveMode, testLiveMode
			identity := measurement(input)
			identity.Podman = nil
			identity.Identity = &model.IdentityObservation{}
			input.Observations = []model.Observation{identity}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			host := contextCompletionProbe{id: "host.complete", run: func(context.Context, platform.Environment) (model.Observation, error) {
				obs := measurement(input)
				obs.ID, obs.ProbeID, obs.Facts[0].ID = "host.complete", "host.complete", "host.fact"
				obs.Podman, obs.Host = nil, &model.HostObservation{}
				if scenario == "cancel before dispatch" {
					cancel()
				}
				return obs, nil
			}}
			contextProbe := contextCompletionProbe{id: "context.required", dependencies: []string{host.ID()},
				run: func(context.Context, platform.Environment) (model.Observation, error) {
					if scenario == "cancel before dispatch" {
						t.Fatal("cancelled context probe dispatched")
					}
					if scenario == "failed without observation" || scenario == "dependency skipped" {
						return model.Observation{}, errors.New("collection failed")
					}
					obs := measurement(input)
					obs.ID, obs.ProbeID, obs.Facts[0].ID = "context.required", "context.required", "context.fact"
					obs.Podman, obs.UserContext = nil, &model.UserContextObservation{}
					if scenario == "retained partial" {
						obs.Completeness = model.Partial
						return obs, errors.New("partial collection")
					}
					if scenario == "deferred query" {
						obs.Diagnostics = []model.Diagnostic{{Code: "user_manager_query_deferred", Message: "query requires active opt-in"}}
					}
					return obs, nil
				}}
			probes := []probe.Probe{host}
			if scenario != "unrequested" {
				probes = append(probes, contextProbe)
			}
			if scenario == "dependency skipped" {
				probes = append(probes, contextCompletionProbe{id: "context.dependent", dependencies: []string{contextProbe.ID()},
					run: func(context.Context, platform.Environment) (model.Observation, error) {
						t.Fatal("dependent of failed context probe dispatched")
						return model.Observation{}, nil
					}})
			}
			report, err := Evaluate(ctx, input, platform.NewEnvironment(nil, nil, nil, nil).WithScope(input.Scope), probes)
			if err != nil {
				t.Fatal(err)
			}
			complete := scenario == "complete" || scenario == "unrequested" || scenario == "deferred query"
			wantExit := ExitIndeterminate
			if complete {
				wantExit = ExitSatisfied
			}
			var out, diagnostics bytes.Buffer
			if got := writeReport(report, Options{}, &out, &diagnostics); got != wantExit ||
				(report.Context.Completeness == string(model.Complete)) != complete {
				t.Fatalf("context=%s host=%s exit=%d want=%d", report.Context.Completeness, report.Host.Completeness, got, wantExit)
			}
			if scenario == "retained partial" && len(report.Evaluation.Observations) != 3 {
				t.Fatal("lost partial observation")
			}
			if scenario == "cancel before dispatch" && !bytes.Contains(out.Bytes(), []byte("probe_cancelled")) {
				t.Fatal("lost cancellation diagnostic")
			}
		})
	}
}

func TestActiveHostGuidance(t *testing.T) {
	t.Parallel()
	if !validOptions(Options{Active: true}) || !validOptions(Options{Active: true, Context: "uid:1000"}) {
		t.Fatal("host-only active selection rejected")
	}
	var out, diagnostics bytes.Buffer
	if Execute(t.Context(), Options{Active: true, Runtime: "invalid"}, &out, &diagnostics) != ExitUsage {
		t.Fatal("invalid selection accepted")
	}
	if strings.Contains(diagnostics.String(), "requires a runtime") || !strings.Contains(diagnostics.String(), "user-manager") ||
		!strings.Contains(diagnostics.String(), "--json") {
		t.Fatal("misleading active-mode guidance", diagnostics.String())
	}
}
