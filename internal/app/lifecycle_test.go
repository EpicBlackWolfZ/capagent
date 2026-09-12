package app

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

type evaluationProbe struct {
	run func(context.Context, platform.Environment) (model.Observation, error)
}

func (evaluationProbe) ID() string             { return "measurement" }
func (evaluationProbe) Dependencies() []string { return nil }
func (p evaluationProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	return p.run(ctx, env)
}

type closeFunc func() error

func (f closeFunc) Close() error { return f() }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func evaluationInput() Input {
	scope := model.EvaluationScope{RunID: "run", ContextID: "user", Runtime: "podman", Endpoint: "local"}
	identity := model.UserIdentity{UID: 1000, GID: 1000}
	return Input{Scope: scope, At: time.Unix(1, 0), Provenance: "synthetic",
		Requirement: &requirement.Node{Capability: "runtime.podman.netavark"},
		Context:     model.EvaluationContext{ID: scope.ContextID, Identity: model.IdentityContext{Current: &identity, Target: &identity}}}
}
func measurement(input Input) model.Observation {
	backend, available := "cni", true
	return model.Observation{ID: "measurement", ProbeID: "measurement", Scope: input.Scope, Timestamp: input.At, Completeness: model.Complete,
		Facts:  []model.Fact{{ID: "fact", Scope: input.Scope, Timestamp: input.At, Source: "fixture", Completeness: model.Complete}},
		Podman: &model.PodmanInfo{Available: &available, NetworkBackend: &backend}}
}

func TestOwnedResourcesCloseAfterJoinedProbes(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"success", "failure", "cancelled", "setup failure", "close failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			input := evaluationInput()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithScope(input.Scope)
			var active, closed atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p := evaluationProbe{run: func(context.Context, platform.Environment) (model.Observation, error) {
				active.Add(1)
				defer active.Add(-1)
				observation := measurement(input)
				switch scenario {
				case "cancelled":
					cancel()
					observation.Completeness = model.Partial
					return observation, ctx.Err()
				case "failure":
					observation.Completeness = model.Partial
					return observation, errors.New("secret raw diagnostic")
				default:
					return observation, nil
				}
			}}
			owner := closeFunc(func() error {
				if active.Load() != 0 {
					t.Error("resource closed while worker still running")
				}
				closed.Add(1)
				if scenario == "close failure" {
					return io.ErrClosedPipe
				}
				return nil
			})
			probes := []probe.Probe{p}
			if scenario == "setup failure" {
				probes = append(probes, p)
			}
			report, err := evaluateOwned(ctx, input, env, probes, owner)
			wantErr := scenario == "setup failure" || scenario == "close failure"
			if (err != nil) != wantErr || closed.Load() != 1 {
				t.Fatal(err, closed.Load())
			}
			if !wantErr && report == nil {
				t.Fatal("lost partial report")
			}
		})
	}
}

func TestConcurrentBorrowedEnvironmentRemainsOpen(t *testing.T) {
	t.Parallel()
	const runs = 3
	input := evaluationInput()
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/value", []byte("value"), 0o644)
	reader := platform.NewScopedMemReader("/", mem)
	defer reader.Close()
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(reader).WithScope(input.Scope)
	var ready sync.WaitGroup
	ready.Add(runs)
	p := evaluationProbe{run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
		ready.Done()
		ready.Wait()
		if data, err := env.Files().ReadFile(ctx, "value"); err != nil || string(data) != "value" {
			t.Error("borrowed read", err)
		}
		return measurement(input), nil
	}}
	var joined sync.WaitGroup
	for range runs {
		joined.Go(func() {
			if _, err := Evaluate(t.Context(), input, env, []probe.Probe{p}); err != nil {
				t.Error(err)
			}
		})
	}
	joined.Wait()
	if _, err := reader.Stat("value"); err != nil {
		t.Fatal("borrowed service was closed", err)
	}
}

func TestApplicationRejectsInvalidInputAndRetainsUnobservedMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		change  func(*Input)
		wantErr bool
	}{
		{"scope mismatch", func(i *Input) { i.Scope.ContextID = "other" }, true},
		{"invalid run", func(i *Input) { i.At = time.Time{} }, true},
		{"invalid host", func(i *Input) { i.Context.Host.CgroupVersion = "invalid" }, true},
		{"unobserved identity", func(i *Input) { i.Context.Identity = model.IdentityContext{} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := evaluationInput()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithScope(input.Scope)
			tt.change(&input)
			report, err := Evaluate(t.Context(), input, env, nil)
			if (err != nil) != tt.wantErr {
				t.Fatal(err)
			}
			if !tt.wantErr && report.Evaluation.Requirement.State != "INDETERMINATE" {
				t.Fatal(report)
			}
		})
	}
	input := evaluationInput()
	invalid := evaluationProbe{run: func(context.Context, platform.Environment) (model.Observation, error) {
		obs := measurement(input)
		obs.Facts = nil
		return obs, nil
	}}
	env := platform.NewEnvironment(nil, nil, nil, nil).WithScope(input.Scope)
	if _, err := Evaluate(t.Context(), input, env, []probe.Probe{invalid}); err == nil {
		t.Fatal("invalid measurement accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := Evaluate(ctx, input, env, []probe.Probe{invalid})
	if err != nil || len(report.Evaluation.Observations) != 0 {
		t.Fatal("cancelled-before-start measurement fabricated", err)
	}
}

func TestOutputFailures(t *testing.T) {
	t.Parallel()
	opts := Options{Fixture: "../../testdata/fixtures/v1/supported"}
	if got := Execute(t.Context(), opts, failingWriter{}, io.Discard); got != ExitExecution {
		t.Fatal(got)
	}
	opts.Fixture = "../../testdata/fixtures/v1/unknown"
	opts.Debug = true
	if got := Execute(t.Context(), opts, io.Discard, failingWriter{}); got != ExitExecution {
		t.Fatal(got)
	}
	if got := Execute(t.Context(), Options{}, io.Discard, failingWriter{}); got != ExitExecution {
		t.Fatal(got)
	}
}

func TestContextSnapshotOwnership(t *testing.T) {
	const mutatedValue = "mutated"
	t.Parallel()
	input := evaluationInput().Context
	value := true
	input.Identity.IsRootless = &value
	input.Identity.HasUserSystemd = &value
	input.Identity.InContainer = &value
	input.Host.SystemdActive = &value
	input.Identity.Current.SupplementaryGroups = []uint32{1000}
	input.Identity.SubUIDRanges = []model.SubIDRange{{Start: 100000, Length: 65536}}
	input.Identity.SubGIDRanges = []model.SubIDRange{{Start: 100000, Length: 65536}}
	input.Namespaces = []model.Namespace{{Kind: "user", ID: "fixture"}}
	input.Runtime.ActiveRuntimes = []string{"podman"}
	input.Configuration.SearchPaths = []string{"/fixture"}
	snapshot := snapshotContext(input)
	input.Identity.Current.SupplementaryGroups[0] = 0
	input.Identity.SubUIDRanges[0].Start = 0
	input.Identity.SubGIDRanges[0].Start = 0
	input.Namespaces[0].ID = mutatedValue
	input.Runtime.ActiveRuntimes[0] = mutatedValue
	input.Configuration.SearchPaths[0] = mutatedValue
	value = false
	if snapshot.Identity.Current.SupplementaryGroups[0] == 0 || snapshot.Identity.SubUIDRanges[0].Start == 0 ||
		snapshot.Identity.SubGIDRanges[0].Start == 0 || snapshot.Namespaces[0].ID == mutatedValue ||
		snapshot.Runtime.ActiveRuntimes[0] == mutatedValue || snapshot.Configuration.SearchPaths[0] == mutatedValue ||
		!*snapshot.Identity.IsRootless || !*snapshot.Identity.HasUserSystemd || !*snapshot.Identity.InContainer || !*snapshot.Host.SystemdActive {
		t.Fatal("context aliases caller-owned data")
	}
}
