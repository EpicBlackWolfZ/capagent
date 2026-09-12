package probe_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

func TestRegistry_TerminalFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		deps [][]string
	}{
		{snapshotMissing, [][]string{{snapshotMissing}}},
		{"self", [][]string{{"a"}}},
		{"cycle", [][]string{{"b"}, {"a"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := probe.NewRegistry()
			for i, deps := range tt.deps {
				if err := r.Register(&fakeProbe{id: string(rune('a' + i)), deps: deps}); err != nil {
					t.Fatal(err)
				}
			}
			first := r.Resolve()
			if first == nil {
				t.Fatal("accepted invalid graph")
			}
			if err := r.Resolve(); err != first {
				t.Errorf("retry = %v, want original %v", err, first)
			}
			if _, err := r.ResolvedPlan(); err != first {
				t.Errorf("plan = %v, want original %v", err, first)
			}
			if _, err := probe.NewOrchestrator(r); err != first {
				t.Errorf("constructor = %v, want original %v", err, first)
			}
			if err := r.Register(&fakeProbe{id: "later"}); !errors.Is(err, probe.ErrRegistryResolved) {
				t.Errorf("register = %v", err)
			}
		})
	}
}

func TestOrchestrator_PreservesPartialObservation(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancellation"}[cancel], func(t *testing.T) {
			t.Parallel()
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			want := model.Observation{ID: "partial", Summary: "measured before failure"}
			r := probe.NewRegistry()
			err := r.Register(&fakeProbe{id: "a",
				run: func(context.Context, platform.Environment) (model.Observation, error) {
					if cancel {
						stop()
						return want, fmt.Errorf("interrupted: %w", context.Canceled)
					}
					return want, errors.New("failed")
				}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Register(&fakeProbe{id: "b", deps: []string{"a"},
				run: func(context.Context, platform.Environment) (model.Observation, error) {
					t.Error("dependent ran")
					return model.Observation{}, nil
				}}); err != nil {
				t.Fatal(err)
			}
			o, err := probe.NewOrchestrator(r)
			if err != nil {
				t.Fatal(err)
			}
			got := o.Run(ctx, newEnv())
			expectedStatus := probe.ProbeFailed
			if cancel {
				expectedStatus = probe.ProbeCancelled
			}
			if got[0].Status != expectedStatus || got[0].Err == nil {
				t.Errorf("outcome = %+v", got[0])
			}
			if !reflect.DeepEqual(got[0].Observation, want) {
				t.Errorf("observation = %+v", got[0].Observation)
			}
			if got[1].Status != probe.ProbeSkipped {
				t.Errorf("dependent = %v", got[1].Status)
			}
		})
	}
}

type contractRunner interface {
	Run(context.Context, platform.Environment) []probe.ProbeResult
}

// Shared by any future implementation through its constructor adapter.
func runConcurrentLifecycleContract(t *testing.T, build func(*probe.Registry) (contractRunner, error)) {
	t.Helper()
	const timeout = 5 * time.Second
	const invocations = 4
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	entered := make(chan struct{}, invocations)
	release := make(chan struct{})
	var closeOnce sync.Once
	unblock := func() { closeOnce.Do(func() { close(release) }) }
	var wg sync.WaitGroup
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/shared", []byte("original"), 0o600)
	scoped := platform.NewScopedMemReader("/proc", mem)
	defer func() {
		unblock()
		cancel()
		wg.Wait()
		if err := scoped.Close(); err != nil {
			t.Errorf("owner close: %v", err)
		}
	}()
	env := platform.NewEnvironment(mem, platform.NewProcfsReader(scoped), nil, nil)
	registry := probe.NewRegistry()
	for _, id := range []string{"a", "b"} {
		if err := registry.Register(&fakeProbe{id: id, run: func(gotCtx context.Context, gotEnv platform.Environment) (model.Observation, error) {
			if gotCtx != ctx {
				t.Error("context replaced")
			}
			data, err := gotEnv.Reader().ReadFile(gotCtx, "/shared")
			if err != nil {
				return model.Observation{}, err
			}
			if string(data) != "original" {
				t.Errorf("shared evidence changed: %q", data)
			}
			data[0] = 'X'
			entered <- struct{}{}
			select {
			case <-release:
			case <-gotCtx.Done():
				return model.Observation{}, gotCtx.Err()
			}
			return model.Observation{Summary: "complete"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
	}
	o, err := build(registry)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan []probe.ProbeResult, 2)
	const runs = 2
	for range runs {
		wg.Add(1)
		go func() { defer wg.Done(); results <- o.Run(ctx, env) }()
	}
	for range invocations {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("concurrent probes did not enter")
		}
	}
	unblock()
	for range runs {
		select {
		case got := <-results:
			if len(got) != runs {
				t.Fatalf("results = %v", got)
			}
			for _, result := range got {
				if result.Status != probe.ProbeSucceeded || result.Observation.Summary != "complete" {
					t.Errorf("result = %+v", result)
				}
			}
		case <-ctx.Done():
			t.Fatal("runs did not join")
		}
	}
	// Run must leave caller-owned resources open.
	if _, err := scoped.Stat(snapshotMissing); errors.Is(err, platform.ErrClosed) {
		t.Fatal("Run closed the owner's reader")
	}
}

func TestOrchestrator_ConcurrentLifecycleContract(t *testing.T) {
	t.Parallel()
	runConcurrentLifecycleContract(t, func(r *probe.Registry) (contractRunner, error) {
		const workers = 2
		return probe.NewOrchestrator(r, probe.WithMaxConcurrency(workers))
	})
}
