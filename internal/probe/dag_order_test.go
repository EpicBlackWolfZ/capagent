package probe_test

import (
	"context"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

// TestRegistry_PlanOrder_ZAM verifies the exact registration-order tie
// breaker behavior: registration [Z, A, M] yields plan [Z, A, M].
func TestRegistry_PlanOrder_ZAM(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	for _, id := range []string{"Z", "A", "M"} {
		if err := r.Register(&stubProbe{id: id}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}
	if err := r.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := r.ResolvedPlan()
	if err != nil {
		t.Fatalf("ResolvedPlan: %v", err)
	}
	if !equalSlices(plan, []string{"Z", "A", "M"}) {
		t.Errorf("plan = %v, want [Z A M]", plan)
	}
}

// TestRegistry_PlanOrder_IndependentOfMapIteration verifies that the plan
// is not influenced by Go's non-deterministic map iteration. We register
// many probes with no dependencies in an arbitrary order and verify the
// plan strictly preserves registration order.
func TestRegistry_PlanOrder_IndependentOfMapIteration(t *testing.T) {
	t.Parallel()

	const trials = 20
	want := []string{"p00", "p01", "p02", "p03", "p04", "p05", "p06", "p07", "p08", "p09"}

	for trial := 0; trial < trials; trial++ {
		r := probe.NewRegistry()
		for _, id := range want {
			if err := r.Register(&stubProbe{id: id}); err != nil {
				t.Fatalf("trial %d Register %s: %v", trial, id, err)
			}
		}
		if err := r.Resolve(); err != nil {
			t.Fatalf("trial %d Resolve: %v", trial, err)
		}
		plan, err := r.ResolvedPlan()
		if err != nil {
			t.Fatalf("trial %d ResolvedPlan: %v", trial, err)
		}
		if !equalSlices(plan, want) {
			t.Fatalf("trial %d plan = %v, want %v", trial, plan, want)
		}
	}
}

// TestRegistry_PlanOrder_RespectsRegistrationAfterResolve verifies that the
// canonical plan is immutable after Resolve. Even if registration order
// influences the plan during construction, no subsequent mutation changes
// the cached plan.
func TestRegistry_PlanOrder_RespectsRegistrationAfterResolve(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	for _, id := range []string{"x", "y", "z"} {
		if err := r.Register(&stubProbe{id: id}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}
	if err := r.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plan1, err := r.ResolvedPlan()
	if err != nil {
		t.Fatalf("first ResolvedPlan: %v", err)
	}
	for i := 0; i < 5; i++ {
		plan, err := r.ResolvedPlan()
		if err != nil {
			t.Fatalf("trial %d ResolvedPlan: %v", i, err)
		}
		if !equalSlices(plan, plan1) {
			t.Errorf("trial %d plan drifted: %v vs %v", i, plan, plan1)
		}
	}
}

// TestOrchestrator_NewlyUnlockedVsReadyTieBreaker verifies the canonical
// tie-breaker behavior when a newly-unlocked node (just became ready due
// to a dependency finishing) competes with already-ready nodes.
//
// Graph: Z (root), A (root, depends on Z), M (root). All three are roots
// at start. A's "ready" event comes only after Z finishes; the tie-breaker
// must still emit them in registration order [Z, A, M] regardless of when
// each probe actually transitions to ready.
func TestOrchestrator_NewlyUnlockedVsReadyTieBreaker(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "Z",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			time.Sleep(15 * time.Millisecond)
			return model.Observation{ID: "Z", ProbeID: "Z"}, nil
		},
	}); err != nil {
		t.Fatalf("Register Z: %v", err)
	}
	if err := r.Register(&fakeProbe{
		id: "A", deps: []string{"Z"},
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			return model.Observation{ID: "A", ProbeID: "A"}, nil
		},
	}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{
		id: "M",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			// M is fast; it finishes long before A becomes ready.
			time.Sleep(2 * time.Millisecond)
			return model.Observation{ID: "M", ProbeID: "M"}, nil
		},
	}); err != nil {
		t.Fatalf("Register M: %v", err)
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	got := orderOfIDs(results)
	want := []string{"Z", "A", "M"}
	if !equalSlices(got, want) {
		t.Errorf("result order = %v, want %v", got, want)
	}
}

// TestOrchestrator_PlanIsImmutableDuringRun verifies that the canonical
// plan is fixed at the start of Run and does not change based on which
// probes happen to be ready when others finish.
func TestOrchestrator_PlanIsImmutableDuringRun(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	// Roots: D, C, B, A (registered in that order). All have no deps.
	for _, id := range []string{"D", "C", "B", "A"} {
		id := id
		if err := r.Register(&fakeProbe{
			id: id,
			run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
				// Reverse-delay so earlier registrations finish last.
				time.Sleep(time.Duration(int('D'-id[0]+1)*5) * time.Millisecond)
				return model.Observation{ID: id, ProbeID: id}, nil
			},
		}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	// Snapshot the plan before Run.
	plan, err := r.ResolvedPlan()
	if err != nil {
		t.Fatalf("ResolvedPlan: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	got := orderOfIDs(results)

	// Plan must equal Run result order, byte-for-byte.
	if !equalSlices(plan, got) {
		t.Errorf("plan (%v) does not equal Run result order (%v)", plan, got)
	}
	want := []string{"D", "C", "B", "A"}
	if !equalSlices(got, want) {
		t.Errorf("result order = %v, want %v", got, want)
	}

	// Plan must still be identical after Run.
	planAfter, err := r.ResolvedPlan()
	if err != nil {
		t.Fatalf("post-Run ResolvedPlan: %v", err)
	}
	if !equalSlices(plan, planAfter) {
		t.Errorf("plan changed during Run: before=%v, after=%v", plan, planAfter)
	}
}
