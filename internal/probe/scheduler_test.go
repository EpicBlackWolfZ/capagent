package probe_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

// TestScheduler_Barrier_FailurePropagatesAsSkipped verifies that when one
// prerequisite of a multi-prerequisite barrier fails, the dependent is
// skipped with ErrDependencyFailed.
//
// Graph: A -> C, B -> C. A succeeds; B fails; C is Skipped.
func TestScheduler_Barrier_FailurePropagatesAsSkipped(t *testing.T) {
	t.Parallel()

	bErr := errors.New("B failed deliberately")
	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{id: "A"}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{
		id: "B",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			return model.Observation{}, bErr
		},
	}); err != nil {
		t.Fatalf("Register B: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "C", deps: []string{"A", "B"}}); err != nil {
		t.Fatalf("Register C: %v", err)
	}

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	a := findResult(results, "A")
	if a == nil || a.Status != probe.ProbeSucceeded {
		t.Errorf("A.Status = %v, want ProbeSucceeded", a)
	}
	b := findResult(results, "B")
	if b == nil || b.Status != probe.ProbeFailed {
		t.Errorf("B.Status = %v, want ProbeFailed", b)
	}
	if !errors.Is(b.Err, bErr) {
		t.Errorf("B.Err = %v, want wraps %v", b.Err, bErr)
	}
	c := findResult(results, "C")
	if c == nil || c.Status != probe.ProbeSkipped {
		t.Errorf("C.Status = %v, want ProbeSkipped", c)
	}
	if !errors.Is(c.Err, probe.ErrDependencyFailed) {
		t.Errorf("C.Err = %v, want ErrDependencyFailed", c.Err)
	}
}

// TestScheduler_Barrier_CancellationPropagatesAsSkipped verifies that when
// one prerequisite of a multi-prerequisite barrier is cancelled, the
// dependent is skipped with ErrDependencyFailed (NOT cancelled).
//
// Graph: A -> C, B -> C. A succeeds; B is cancelled; C is Skipped.
//
// Synchronization is achieved via a startHook on B (signalled when B
// actually begins executing) rather than a time.Sleep so the cancellation
// is deterministically applied while B is in-flight.
func TestScheduler_Barrier_CancellationPropagatesAsSkipped(t *testing.T) {
	t.Parallel()

	bStarted := make(chan struct{})

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{id: "A"}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{
		id: "B",
		startHook: func() {
			close(bStarted)
		},
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register B: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "C", deps: []string{"A", "B"}}); err != nil {
		t.Fatalf("Register C: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	done := make(chan []probe.ProbeResult, 1)
	go func() {
		done <- o.Run(ctx, newEnv())
	}()

	// Wait until B is actually running, then cancel deterministically.
	select {
	case <-bStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("B did not start in time")
	}
	cancel()

	var results []probe.ProbeResult
	select {
	case results = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}

	a := findResult(results, "A")
	if a == nil {
		t.Fatalf("A result missing")
	}
	b := findResult(results, "B")
	if b == nil || b.Status != probe.ProbeCancelled {
		t.Errorf("B.Status = %v, want ProbeCancelled", b)
	}
	c := findResult(results, "C")
	if c == nil || c.Status != probe.ProbeSkipped {
		t.Errorf("C.Status = %v, want ProbeSkipped", c)
	}
	if !errors.Is(c.Err, probe.ErrDependencyFailed) {
		t.Errorf("C.Err = %v, want ErrDependencyFailed", c.Err)
	}
}

// TestScheduler_Barrier_SkippedDependencyCascadesAsSkipped verifies that a
// dependent of an already-skipped probe is also skipped, regardless of
// whether its other prerequisites succeeded.
//
// Graph: A -> B -> D, C -> D. A fails; B is skipped. C succeeds. D should
// be skipped because both B and C are prerequisites; C succeeded but B did
// not.
func TestScheduler_Barrier_SkippedDependencyCascadesAsSkipped(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "A",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			return model.Observation{}, errors.New("nope")
		},
	}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "B", deps: []string{"A"}}); err != nil {
		t.Fatalf("Register B: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "C"}); err != nil {
		t.Fatalf("Register C: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "D", deps: []string{"B", "C"}}); err != nil {
		t.Fatalf("Register D: %v", err)
	}

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	wantStatus := map[string]probe.ProbeStatus{
		"A": probe.ProbeFailed,
		"B": probe.ProbeSkipped,
		"C": probe.ProbeSucceeded,
		"D": probe.ProbeSkipped,
	}
	for _, res := range results {
		if want, ok := wantStatus[res.ProbeID]; ok && res.Status != want {
			t.Errorf("%s.Status = %v, want %v", res.ProbeID, res.Status, want)
		}
	}
}

// TestScheduler_ProbeExecutesAtMostOnce verifies that a probe is not
// re-invoked when the same probe is enqueued multiple times (e.g. when one
// dependency finishes and pushes the probe, but the probe had already been
// queued or finalized).
func TestScheduler_ProbeExecutesAtMostOnce(t *testing.T) {
	t.Parallel()

	var runs atomic.Int32
	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "X",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			runs.Add(1)
			return model.Observation{ID: "X", ProbeID: "X"}, nil
		},
	}); err != nil {
		t.Fatalf("Register X: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "Y", deps: []string{"X"}}); err != nil {
		t.Fatalf("Register Y: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "Z", deps: []string{"X"}}); err != nil {
		t.Fatalf("Register Z: %v", err)
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(8))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3", len(results))
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("X ran %d times, want 1", got)
	}
}

// TestScheduler_TransitiveCascadeProducesExactlyOneResultPerProbe verifies
// that no probe produces more than one ProbeResult, even when its
// dependency chain has multiple failure points.
func TestScheduler_TransitiveCascadeProducesExactlyOneResultPerProbe(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "A",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			return model.Observation{}, errors.New("A failed")
		},
	}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	// Chain A -> B -> C -> D. A failing should skip B, C, D.
	for _, pair := range []struct {
		id   string
		deps []string
	}{
		{"B", []string{"A"}},
		{"C", []string{"B"}},
		{"D", []string{"C"}},
	} {
		if err := r.Register(&fakeProbe{id: pair.id, deps: pair.deps}); err != nil {
			t.Fatalf("Register %s: %v", pair.id, err)
		}
	}

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(context.Background(), newEnv())

	count := make(map[string]int)
	for _, res := range results {
		count[res.ProbeID]++
	}
	for id, n := range count {
		if n != 1 {
			t.Errorf("%s appeared %d times, want 1", id, n)
		}
	}
	if len(count) != 4 {
		t.Errorf("results cover %d distinct probes, want 4", len(count))
	}
}

// TestScheduler_IndependentSiblingsContinueRunning verifies that probes
// without a dependency relationship continue running after a sibling
// branch fails or is cancelled.
func TestScheduler_IndependentSiblingsContinueRunning(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "F",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			return model.Observation{}, errors.New("F failed")
		},
	}); err != nil {
		t.Fatalf("Register F: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "G"}); err != nil {
		t.Fatalf("Register G: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "H"}); err != nil {
		t.Fatalf("Register H: %v", err)
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(context.Background(), newEnv())

	g := findResult(results, "G")
	if g == nil || g.Status != probe.ProbeSucceeded {
		t.Errorf("G.Status = %v, want ProbeSucceeded", g)
	}
	h := findResult(results, "H")
	if h == nil || h.Status != probe.ProbeSucceeded {
		t.Errorf("H.Status = %v, want ProbeSucceeded", h)
	}
}

// TestScheduler_ConcurrencyNeverExceedsCapForMultiPrereqBarrier verifies
// that the concurrency cap is honored even when many prerequisites are
// ready simultaneously, and the dependent only runs once they all
// complete.
func TestScheduler_ConcurrencyNeverExceedsCapForMultiPrereqBarrier(t *testing.T) {
	t.Parallel()

	const cap = 3
	const prerequisites = 8

	var inFlight atomic.Int32
	var maxObserved atomic.Int32

	r := probe.NewRegistry()
	for i := 0; i < prerequisites; i++ {
		id := fmt.Sprintf("p%02d", i)
		if err := r.Register(&fakeProbe{
			id: id,
			run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
				cur := inFlight.Add(1)
				defer inFlight.Add(-1)
				for {
					prev := maxObserved.Load()
					if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				return model.Observation{ID: id, ProbeID: id}, nil
			},
		}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}
	deps := make([]string, prerequisites)
	for i := range deps {
		deps[i] = fmt.Sprintf("p%02d", i)
	}
	if err := r.Register(&fakeProbe{id: "downstream", deps: deps}); err != nil {
		t.Fatalf("Register downstream: %v", err)
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(cap))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(context.Background(), newEnv())

	if len(results) != prerequisites+1 {
		t.Fatalf("results len = %d, want %d", len(results), prerequisites+1)
	}
	if observed := int(maxObserved.Load()); observed > cap {
		t.Errorf("max concurrent executions = %d, want <= %d", observed, cap)
	}

	downstream := findResult(results, "downstream")
	if downstream == nil || downstream.Status != probe.ProbeSucceeded {
		t.Errorf("downstream.Status = %v, want ProbeSucceeded", downstream)
	}
}

// TestScheduler_NoDependentExecutesPrematurely hammers the barrier with
// fast prerequisites and verifies the dependent never starts before all
// prerequisites have signaled completion.
func TestScheduler_NoDependentExecutesPrematurely(t *testing.T) {
	t.Parallel()

	const prerequisites = 6

	var doneCount atomic.Int32
	var mu sync.Mutex
	startedProbes := make(map[string]bool)
	var downstreamStartedBeforeAllDone atomic.Bool

	r := probe.NewRegistry()
	for i := 0; i < prerequisites; i++ {
		id := fmt.Sprintf("p%02d", i)
		if err := r.Register(&fakeProbe{
			id: id,
			run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
				mu.Lock()
				startedProbes[id] = true
				mu.Unlock()
				doneCount.Add(1)
				return model.Observation{ID: id, ProbeID: id}, nil
			},
		}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}
	deps := make([]string, prerequisites)
	for i := range deps {
		deps[i] = fmt.Sprintf("p%02d", i)
	}
	if err := r.Register(&fakeProbe{
		id: "downstream", deps: deps,
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			if int(doneCount.Load()) < prerequisites {
				downstreamStartedBeforeAllDone.Store(true)
			}
			return model.Observation{ID: "downstream", ProbeID: "downstream"}, nil
		},
	}); err != nil {
		t.Fatalf("Register downstream: %v", err)
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(8))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(context.Background(), newEnv())

	if len(results) != prerequisites+1 {
		t.Fatalf("results len = %d, want %d", len(results), prerequisites+1)
	}
	if downstreamStartedBeforeAllDone.Load() {
		t.Error("downstream started before all prerequisites completed")
	}
}
