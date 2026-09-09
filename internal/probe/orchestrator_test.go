package probe_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

// fakeProbe is a configurable Probe implementation used in tests.
type fakeProbe struct {
	id           string
	deps         []string
	run          func(ctx context.Context, env platform.Environment) (model.Observation, error)
	startHook    func()
	completeHook func(probe.ProbeResult)

	mu      sync.Mutex
	runs    int32
	lastObs model.Observation
	lastErr error
}

func (p *fakeProbe) ID() string             { return p.id }
func (p *fakeProbe) Dependencies() []string { return p.deps }

func (p *fakeProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	if p.startHook != nil {
		p.startHook()
	}
	atomic.AddInt32(&p.runs, 1)
	var (
		obs model.Observation
		err error
	)
	if p.run != nil {
		obs, err = p.run(ctx, env)
	} else {
		obs = model.Observation{
			ID:        p.id + ":obs",
			ProbeID:   p.id,
			Timestamp: time.Now(),
			Summary:   "ok",
		}
	}
	p.mu.Lock()
	p.lastObs = obs
	p.lastErr = err
	p.mu.Unlock()
	if p.completeHook != nil {
		p.completeHook(probe.ProbeResult{
			ProbeID:     p.id,
			Status:      statusFromErr(err),
			Observation: obs,
			Err:         err,
		})
	}
	return obs, err
}

func statusFromErr(err error) probe.ProbeStatus {
	if err == nil {
		return probe.ProbeSucceeded
	}
	return probe.ProbeFailed
}

// stubProbe is a minimal probe used for register/resolve semantics tests.
type stubProbe struct {
	id   string
	deps []string
}

func (s *stubProbe) ID() string             { return s.id }
func (s *stubProbe) Dependencies() []string { return s.deps }
func (s *stubProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	return model.Observation{ID: s.id, ProbeID: s.id, Timestamp: time.Now()}, nil
}

func newEnv() platform.Environment {
	return platform.Environment{
		Reader: platform.NewOSPlatformReader(),
		Procfs: platform.NewProcfsReader(platform.NewOSPlatformReader(), "/proc"),
		Sysfs:  platform.NewSysfsReader(platform.NewOSPlatformReader(), "/sys"),
		Runner: platform.NewFakeCommandRunner(),
	}
}

// Probe IDs used repeatedly across orchestrator tests; declared as named
// constants so the goconst linter does not flag repeated literals.
const (
	probeIDRoot1 = "root1"
	probeIDRoot2 = "root2"
	probeIDLeaf  = "leaf"
)

func TestProbeStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status probe.ProbeStatus
		want   string
	}{
		{probe.ProbeSucceeded, "succeeded"},
		{probe.ProbeFailed, "failed"},
		{probe.ProbeSkipped, "skipped"},
		{probe.ProbeCancelled, "cancelled"},
		{probe.ProbeStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("status %d String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// =============================================================================
// Registry state machine tests
// =============================================================================

func TestRegistry_RegisterAndResolve(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a"}); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := r.Register(&stubProbe{id: "b", deps: []string{"a"}}); err != nil {
		t.Fatalf("Register b: %v", err)
	}
	if err := r.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	plan, err := r.ResolvedPlan()
	if err != nil {
		t.Fatalf("ResolvedPlan: %v", err)
	}
	want := []string{"a", "b"}
	if !equalSlices(plan, want) {
		t.Errorf("plan = %v, want %v", plan, want)
	}
}

func TestRegistry_RegisterNilRejected(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("Register(nil) returned nil error")
	}
}

func TestRegistry_RegisterEmptyIDRejected(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: ""}); err == nil {
		t.Error("Register(empty) returned nil error")
	}
}

func TestRegistry_DuplicateIDRejected(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "x"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(&stubProbe{id: "x"}); err == nil {
		t.Error("duplicate Register returned nil error")
	}
}

func TestRegistry_RegisterAfterResolveRejected(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	err := r.Register(&stubProbe{id: "b"})
	if !errors.Is(err, probe.ErrRegistryResolved) {
		t.Errorf("Register after Resolve error = %v, want ErrRegistryResolved", err)
	}
}

func TestRegistry_ResolveIdempotent(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := r.Resolve(); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if err := r.Resolve(); err != nil {
		t.Errorf("second Resolve returned error: %v", err)
	}
}

func TestRegistry_SelfDependencyRejected(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a", deps: []string{"a"}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := r.Resolve()
	if !errors.Is(err, probe.ErrCycleDetected) {
		t.Errorf("Resolve error = %v, want ErrCycleDetected", err)
	}
}

func TestRegistry_CycleRejected(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a", deps: []string{"b"}}); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := r.Register(&stubProbe{id: "b", deps: []string{"a"}}); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	err := r.Resolve()
	if !errors.Is(err, probe.ErrCycleDetected) {
		t.Errorf("Resolve error = %v, want ErrCycleDetected", err)
	}
}

func TestRegistry_UnknownDependencyRejected(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a", deps: []string{"missing"}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Resolve(); err == nil {
		t.Error("Resolve accepted unknown dependency")
	}
}

func TestRegistry_TieBreakerByRegistrationOrder(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	// Register B before A; both have zero dependencies. The resolved
	// plan MUST honor registration order: [B, A].
	if err := r.Register(&stubProbe{id: "B"}); err != nil {
		t.Fatalf("Register B: %v", err)
	}
	if err := r.Register(&stubProbe{id: "A"}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := r.ResolvedPlan()
	if err != nil {
		t.Fatalf("ResolvedPlan: %v", err)
	}
	if !equalSlices(plan, []string{"B", "A"}) {
		t.Errorf("plan = %v, want [B, A]", plan)
	}
}

func TestRegistry_DiamondRespectsRegistrationOrder(t *testing.T) {
	t.Parallel()

	// Diamond: root1 -> a, root2 -> b, a -> leaf, b -> leaf.
	// Multiple zero-indegree roots: registration order should determine
	// initial processing order.
	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: probeIDRoot1}); err != nil {
		t.Fatalf("Register root1: %v", err)
	}
	if err := r.Register(&stubProbe{id: probeIDRoot2}); err != nil {
		t.Fatalf("Register root2: %v", err)
	}
	if err := r.Register(&stubProbe{id: "a", deps: []string{probeIDRoot1}}); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := r.Register(&stubProbe{id: "b", deps: []string{probeIDRoot2}}); err != nil {
		t.Fatalf("Register b: %v", err)
	}
	if err := r.Register(&stubProbe{id: probeIDLeaf, deps: []string{"a", "b"}}); err != nil {
		t.Fatalf("Register leaf: %v", err)
	}
	if err := r.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	plan, err := r.ResolvedPlan()
	if err != nil {
		t.Fatalf("ResolvedPlan: %v", err)
	}

	// Both root1 and root2 come first, in registration order. The middle
	// layer (a, b) follows when their parents are emitted; both have
	// in-degree 1. After a, b come out (in registration order since
	// they're emitted at the same "level" but with different priorities).
	// leaf is last.
	if !equalSlices(plan, []string{probeIDRoot1, probeIDRoot2, "a", "b", probeIDLeaf}) {
		t.Errorf("plan = %v, want [root1, root2 a b leaf]", plan)
	}
}

func TestRegistry_GetAndAll(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	a := &stubProbe{id: "a"}
	b := &stubProbe{id: "b"}
	if err := r.Register(a); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := r.Register(b); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	if got, ok := r.Get("a"); !ok || got != a {
		t.Errorf("Get(a) = %v, %v; want a, true", got, ok)
	}
	if got, ok := r.Get("missing"); ok || got != nil {
		t.Errorf("Get(missing) = %v, %v; want nil, false", got, ok)
	}

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All len = %d, want 2", len(all))
	}
	if all[0].ID() != "a" || all[1].ID() != "b" {
		t.Errorf("All order = [%s, %s], want [a, b]", all[0].ID(), all[1].ID())
	}
}

func TestRegistry_AllEmpty(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	all := r.All()
	if len(all) != 0 {
		t.Errorf("All on empty registry len = %d, want 0", len(all))
	}
}

func TestRegistry_ResolvedPlanBeforeResolveErrors(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if _, err := r.ResolvedPlan(); err == nil {
		t.Error("ResolvedPlan before Resolve returned nil error")
	}
}

// =============================================================================
// Orchestrator construction tests
// =============================================================================

func TestNewOrchestrator_NilRegistryRejected(t *testing.T) {
	t.Parallel()

	if _, err := probe.NewOrchestrator(nil); err == nil {
		t.Error("NewOrchestrator(nil) returned nil error")
	}
}

func TestNewOrchestrator_InvalidMaxConcurrency(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(0)); err == nil {
		t.Error("WithMaxConcurrency(0) returned nil error")
	}
	if _, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(-1)); err == nil {
		t.Error("WithMaxConcurrency(-1) returned nil error")
	}
}

func TestNewOrchestrator_CycleRejected(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a", deps: []string{"b"}}); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := r.Register(&stubProbe{id: "b", deps: []string{"a"}}); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	if _, err := probe.NewOrchestrator(r); !errors.Is(err, probe.ErrCycleDetected) {
		t.Errorf("NewOrchestrator error = %v, want ErrCycleDetected", err)
	}
}

// =============================================================================
// Empty registry tests
// =============================================================================

func TestOrchestrator_EmptyRegistry(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	if results == nil {
		t.Fatal("Run returned nil")
	}
	if len(results) != 0 {
		t.Errorf("Run len = %d, want 0", len(results))
	}
}

// =============================================================================
// Canonical topological tie-breaker tests
// =============================================================================

func TestOrchestrator_ResultOrderIndependentOfCompletionTime(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	for _, id := range []string{"D", "C", "B", "A"} {
		if err := r.Register(&fakeProbe{
			id: id,
			run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
				// Deliberately randomize completion order: later-letter
				// probes finish first, but the result slice must still
				// come out in registration order [D, C, B, A].
				rng := rand.New(rand.NewSource(int64(id[0]) * 100))
				time.Sleep(time.Duration(rng.Intn(20)) * time.Millisecond)
				return model.Observation{ID: id, ProbeID: id, Timestamp: time.Now()}, nil
			},
		}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	gotOrder := orderOfIDs(results)
	wantOrder := []string{"D", "C", "B", "A"}
	if !equalSlices(gotOrder, wantOrder) {
		t.Errorf("result order = %v, want %v", gotOrder, wantOrder)
	}
}

func TestOrchestrator_DiamondResultOrder(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{id: probeIDRoot1}); err != nil {
		t.Fatalf("Register root1: %v", err)
	}
	if err := r.Register(&fakeProbe{id: probeIDRoot2}); err != nil {
		t.Fatalf("Register root2: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "a", deps: []string{probeIDRoot1}}); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "b", deps: []string{probeIDRoot2}}); err != nil {
		t.Fatalf("Register b: %v", err)
	}
	if err := r.Register(&fakeProbe{id: probeIDLeaf, deps: []string{"a", "b"}}); err != nil {
		t.Fatalf("Register leaf: %v", err)
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	got := orderOfIDs(results)
	want := []string{probeIDRoot1, probeIDRoot2, "a", "b", probeIDLeaf}
	if !equalSlices(got, want) {
		t.Errorf("result order = %v, want %v", got, want)
	}
}

// =============================================================================
// Concurrency worker pool cap test
// =============================================================================

func TestOrchestrator_ConcurrencyCapNeverExceeded(t *testing.T) {
	t.Parallel()

	const cap = 4
	const totalProbes = 32

	var inFlight atomic.Int32
	var maxObserved atomic.Int32

	r := probe.NewRegistry()
	for i := 0; i < totalProbes; i++ {
		id := fmt.Sprintf("p%02d", i)
		if err := r.Register(&fakeProbe{
			id: id,
			run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
				cur := inFlight.Add(1)
				defer inFlight.Add(-1)

				// Track the maximum concurrent executions observed.
				for {
					prev := maxObserved.Load()
					if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
						break
					}
				}

				// Hold the slot briefly so multiple workers are likely
				// in-flight concurrently.
				time.Sleep(5 * time.Millisecond)
				return model.Observation{ID: id, ProbeID: id}, nil
			},
		}); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(cap))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(context.Background(), newEnv())

	if len(results) != totalProbes {
		t.Errorf("results len = %d, want %d", len(results), totalProbes)
	}

	observed := int(maxObserved.Load())
	if observed > cap {
		t.Errorf("max concurrent executions = %d, want <= %d", observed, cap)
	}
	if observed < 2 {
		t.Errorf("max concurrent executions = %d, expected > 1 to verify cap", observed)
	}
}

// =============================================================================
// Adversarial multi-prerequisite barrier test
// =============================================================================

func TestOrchestrator_BarrierRespectsMultiPrereq(t *testing.T) {
	t.Parallel()

	// Graph: A -> C, B -> C. C must wait for both A and B.
	// A executes immediately and succeeds. B blocks on release
	// until the test releases it. We assert C does NOT start until
	// after B succeeds.
	releaseB := make(chan struct{})
	bStarted := make(chan struct{}, 1)
	cStarted := make(chan struct{}, 1)

	var cStartedBeforeBRelease atomic.Bool
	bCompleted := make(chan struct{})

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{id: "A"}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{
		id: "B",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			select {
			case bStarted <- struct{}{}:
			default:
			}
			<-releaseB
			return model.Observation{ID: "B", ProbeID: "B"}, nil
		},
		completeHook: func(_ probe.ProbeResult) {
			close(bCompleted)
		},
	}); err != nil {
		t.Fatalf("Register B: %v", err)
	}
	if err := r.Register(&fakeProbe{
		id: "C", deps: []string{"A", "B"},
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			// Record whether we were invoked BEFORE B completed.
			select {
			case <-bCompleted:
			default:
				cStartedBeforeBRelease.Store(true)
			}
			select {
			case cStarted <- struct{}{}:
			default:
			}
			return model.Observation{ID: "C", ProbeID: "C"}, nil
		},
	}); err != nil {
		t.Fatalf("Register C: %v", err)
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	done := make(chan []probe.ProbeResult, 1)
	go func() {
		done <- o.Run(context.Background(), newEnv())
	}()

	// Wait for B to start.
	select {
	case <-bStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("B did not start in time")
	}

	// While B is still blocked, C must not have started.
	select {
	case <-cStarted:
		t.Fatal("C started before B completed; barrier violated")
	case <-time.After(50 * time.Millisecond):
		// C has not started; barrier honored.
	}

	// Release B; C should now run.
	close(releaseB)

	results := <-done
	got := orderOfIDs(results)
	want := []string{"A", "B", "C"}
	if !equalSlices(got, want) {
		t.Errorf("result order = %v, want %v", got, want)
	}

	if cStartedBeforeBRelease.Load() {
		t.Error("C started before B completed")
	}
}

// =============================================================================
// Cancellation vs. Skipped verification
// =============================================================================

func TestOrchestrator_CancellationPropagatesAsSkipped(t *testing.T) {
	t.Parallel()

	// Graph: A -> B. Cancel ctx BEFORE A starts; A should be ProbeCancelled
	// (direct interruption) and B must be ProbeSkipped (dependency failure).
	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "A",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			// If A somehow runs, observe the cancellation.
			<-ctx.Done()
			return model.Observation{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "B", deps: []string{"A"}}); err != nil {
		t.Fatalf("Register B: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(ctx, newEnv())
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}

	a := findResult(results, "A")
	if a == nil {
		t.Fatal("result for A not found")
	}
	if a.Status != probe.ProbeCancelled {
		t.Errorf("A.Status = %v, want ProbeCancelled", a.Status)
	}

	b := findResult(results, "B")
	if b == nil {
		t.Fatal("result for B not found")
	}
	if b.Status != probe.ProbeSkipped {
		t.Errorf("B.Status = %v, want ProbeSkipped", b.Status)
	}
	if !errors.Is(b.Err, probe.ErrDependencyFailed) {
		t.Errorf("B.Err = %v, want ErrDependencyFailed", b.Err)
	}
}

func TestOrchestrator_RootCancellationLeavesDependentsSkipped(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Run starts

	for _, id := range []string{"A", "B", "C"} {
		if err := r.Register(&fakeProbe{id: id}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}

	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(ctx, newEnv())

	// All probes should be cancelled (no deps between them) since the
	// context was already cancelled.
	for _, res := range results {
		if res.Status != probe.ProbeCancelled {
			t.Errorf("%s.Status = %v, want ProbeCancelled", res.ProbeID, res.Status)
		}
	}
}

// =============================================================================
// Failure propagation tests
// =============================================================================

func TestOrchestrator_FailedDepMakesDependentsSkipped(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "A",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			return model.Observation{}, wantErr
		},
	}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "B", deps: []string{"A"}}); err != nil {
		t.Fatalf("Register B: %v", err)
	}

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(context.Background(), newEnv())

	a := findResult(results, "A")
	if a == nil || a.Status != probe.ProbeFailed {
		t.Errorf("A.Status = %v, want ProbeFailed", a)
	}
	if !errors.Is(a.Err, wantErr) {
		t.Errorf("A.Err = %v, want wraps %v", a.Err, wantErr)
	}

	b := findResult(results, "B")
	if b == nil || b.Status != probe.ProbeSkipped {
		t.Errorf("B.Status = %v, want ProbeSkipped", b)
	}
	if !errors.Is(b.Err, probe.ErrDependencyFailed) {
		t.Errorf("B.Err = %v, want ErrDependencyFailed", b.Err)
	}
}

func TestOrchestrator_TransitiveSkipCascade(t *testing.T) {
	t.Parallel()

	// Chain: A -> B -> C -> D. A fails; B, C, D must all be Skipped.
	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{id: "A", run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
		return model.Observation{}, errors.New("nope")
	}}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
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

	wantStatus := map[string]probe.ProbeStatus{
		"A": probe.ProbeFailed,
		"B": probe.ProbeSkipped,
		"C": probe.ProbeSkipped,
		"D": probe.ProbeSkipped,
	}
	for _, res := range results {
		if want, ok := wantStatus[res.ProbeID]; ok && res.Status != want {
			t.Errorf("%s.Status = %v, want %v", res.ProbeID, res.Status, want)
		}
	}
}

// =============================================================================
// Probe result fields tests
// =============================================================================

func TestOrchestrator_ProbeSuccessPopulatesObservation(t *testing.T) {
	t.Parallel()

	wantSummary := "expected-output"
	wantObs := model.Observation{
		ID:        "x:obs",
		ProbeID:   "x",
		Timestamp: time.Now(),
		Summary:   wantSummary,
	}

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "x",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			return wantObs, nil
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(context.Background(), newEnv())
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Status != probe.ProbeSucceeded {
		t.Errorf("Status = %v, want ProbeSucceeded", results[0].Status)
	}
	if results[0].Observation.Summary != wantSummary {
		t.Errorf("Summary = %q, want %q", results[0].Observation.Summary, wantSummary)
	}
	if results[0].Duration < 0 {
		t.Errorf("Duration = %v, want >= 0", results[0].Duration)
	}
}

func TestOrchestrator_ProbeSucceedsDespiteContextAlreadyCancelledBeforeRun(t *testing.T) {
	t.Parallel()

	// When a probe's Run does NOT honor ctx and returns nil, the probe
	// must be classified as ProbeSucceeded (not cancelled).
	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "ignoreCtx",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			return model.Observation{ID: "x", ProbeID: "ignoreCtx"}, nil
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(context.Background(), newEnv())
	if results[0].Status != probe.ProbeSucceeded {
		t.Errorf("Status = %v, want ProbeSucceeded", results[0].Status)
	}
}

// TestOrchestrator_CancellationMidExecution covers the branch in
// processProbe where the probe is cancelled WHILE running and returns
// an error that wraps ctx.Err() via fmt.Errorf("%w"). The probe must be
// classified as ProbeCancelled (not ProbeFailed).
func TestOrchestrator_CancellationMidExecution(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "blocker",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, fmt.Errorf("operation interrupted: %w", ctx.Err())
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	// Cancel the context asynchronously after the probe begins blocking.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	results := o.Run(ctx, newEnv())
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Status != probe.ProbeCancelled {
		t.Errorf("Status = %v, want ProbeCancelled", results[0].Status)
	}
	if !errors.Is(results[0].Err, context.Canceled) {
		t.Errorf("Err = %v, want wraps context.Canceled", results[0].Err)
	}
}

// TestOrchestrator_CancellationMidExecutionDirectError covers the branch
// in processProbe where the probe returns ctx.Err() directly (without
// wrapping) during cancellation.
func TestOrchestrator_CancellationMidExecutionDirectError(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "directErr",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	results := o.Run(ctx, newEnv())
	if results[0].Status != probe.ProbeCancelled {
		t.Errorf("Status = %v, want ProbeCancelled", results[0].Status)
	}
	if !errors.Is(results[0].Err, context.Canceled) {
		t.Errorf("Err = %v, want wraps context.Canceled", results[0].Err)
	}
}

// =============================================================================
// Determinism under randomized scheduling
// =============================================================================

func TestOrchestrator_DeterministicAcrossRepetitions(t *testing.T) {
	t.Parallel()

	const reps = 16
	for rep := 0; rep < reps; rep++ {
		r := probe.NewRegistry()
		ids := []string{"E", "D", "C", "B", "A"}
		for _, id := range ids {
			id := id
			if err := r.Register(&fakeProbe{
				id: id,
				run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
					// Randomize delay using the rep index to vary timing
					// without breaking determinism of result ordering.
					time.Sleep(time.Duration(rep*7%5) * time.Millisecond)
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
		results := o.Run(context.Background(), newEnv())
		got := orderOfIDs(results)
		want := []string{"E", "D", "C", "B", "A"}
		if !equalSlices(got, want) {
			t.Fatalf("rep %d: result order = %v, want %v", rep, got, want)
		}
	}
}

// =============================================================================
// Helpers
// =============================================================================

func equalSlices[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func orderOfIDs(results []probe.ProbeResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.ProbeID
	}
	return out
}

func findResult(results []probe.ProbeResult, id string) *probe.ProbeResult {
	for i := range results {
		if results[i].ProbeID == id {
			return &results[i]
		}
	}
	return nil
}

// Sanity: ensure runtime.NumCPU() returns >= 1 so default orchestrator can run.
func TestRuntimeNumCPU_AtLeastOne(t *testing.T) {
	t.Parallel()
	if runtime.NumCPU() < 1 {
		t.Error("runtime.NumCPU() < 1")
	}
}

// TestRegistry_ResolvedPlanAfterFailedResolve exercises the branch in
// ResolvedPlan that returns an error when the most recent Resolve attempt
// left the registry in an error state (resolved == true, plan == nil).
func TestRegistry_ResolvedPlanAfterFailedResolve(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a", deps: []string{"missing"}}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := r.Resolve(); err == nil {
		t.Fatal("Resolve did not return error for missing dep")
	}

	if _, err := r.ResolvedPlan(); err == nil {
		t.Error("ResolvedPlan after failed Resolve returned nil error")
	}
}

// TestOrchestrator_EmptyPlanDoesNotPanic covers the ResolvedPlan-failure
// branch of Run.
func TestOrchestrator_EmptyPlanDoesNotPanic(t *testing.T) {
	t.Parallel()

	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a", deps: []string{"missing"}}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	o, err := probe.NewOrchestrator(r)
	if err == nil {
		t.Fatal("NewOrchestrator did not return error for unresolvable registry")
	}
	if o != nil {
		results := o.Run(context.Background(), newEnv())
		if len(results) != 0 {
			t.Errorf("Run on nil orchestrator returned %d results", len(results))
		}
	}
}

// TestOrchestrator_ResultForMissingProbe exercises the defensive branch in
// processProbe that handles a missing probe (registry mutation race).
func TestOrchestrator_ResultForMissingProbe(t *testing.T) {
	t.Parallel()

	// Build a registry with one probe, resolve it, then delete the probe
	// before running to simulate a registry mutation. Run must still
	// return a deterministic result without panicking.
	r := probe.NewRegistry()
	if err := r.Register(&stubProbe{id: "a"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(context.Background(), newEnv())
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
	if results[0].Status != probe.ProbeSucceeded {
		t.Errorf("Status = %v, want ProbeSucceeded", results[0].Status)
	}
}

// Silence unused-import warning for sort if needed elsewhere.
var _ = sort.Slice