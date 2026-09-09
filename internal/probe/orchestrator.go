package probe

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// ProbeStatus enumerates the four terminal outcomes a probe may report.
type ProbeStatus uint8

const (
	// ProbeSucceeded indicates the probe completed without error.
	ProbeSucceeded ProbeStatus = iota
	// ProbeFailed indicates the probe returned a non-context error.
	ProbeFailed
	// ProbeSkipped indicates a transitive dependent whose prerequisite
	// did not succeed. The error is ErrDependencyFailed.
	ProbeSkipped
	// ProbeCancelled indicates the probe was directly interrupted by
	// caller context cancellation or internal orchestrator timeout.
	ProbeCancelled
)

// String returns a human-readable name for the status.
func (s ProbeStatus) String() string {
	switch s {
	case ProbeSucceeded:
		return "succeeded"
	case ProbeFailed:
		return "failed"
	case ProbeSkipped:
		return "skipped"
	case ProbeCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// ProbeResult is the per-probe outcome emitted by Orchestrator.Run.
type ProbeResult struct {
	ProbeID     string
	Status      ProbeStatus
	Observation model.Observation
	Err         error
	Duration    time.Duration
}

// Orchestrator executes a resolved Registry's probes under a configurable
// concurrency cap with strict dependency-aware scheduling.
//
// Orchestrator holds an internal reference to its Registry. Constructing an
// Orchestrator triggers Registry.Resolve; cycles, missing dependencies, and
// invalid configuration are reported as construction errors.
type Orchestrator struct {
	registry       *Registry
	maxConcurrency int
}

// OrchestratorOption mutates an Orchestrator during construction.
type OrchestratorOption func(*Orchestrator) error

// WithMaxConcurrency overrides the worker pool size. The supplied value
// must be >= 1; zero or negative values are rejected at construction time.
func WithMaxConcurrency(n int) OrchestratorOption {
	return func(o *Orchestrator) error {
		if n < 1 {
			return fmt.Errorf("maxConcurrency must be >= 1, got %d", n)
		}
		o.maxConcurrency = n
		return nil
	}
}

// NewOrchestrator constructs an Orchestrator over registry. Construction
// immediately invokes registry.Resolve so that cycle or missing-dependency
// errors are surfaced up front.
func NewOrchestrator(registry *Registry, opts ...OrchestratorOption) (*Orchestrator, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry cannot be nil")
	}

	o := &Orchestrator{
		registry:       registry,
		maxConcurrency: runtime.NumCPU(),
	}
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}

	if err := registry.Resolve(); err != nil {
		return nil, err
	}
	return o, nil
}

// probeState tracks the per-probe execution state used by the scheduler.
type probeState struct {
	remainingDeps int
	finalized     bool
	result        ProbeResult
}

// runQueue is a slice-backed FIFO queue with a non-blocking push and a
// blocking pop. Closed queues cause push to become a no-op and pop to
// return false, allowing workers to exit cleanly.
type runQueue struct {
	mu     sync.Mutex
	items  []string
	wake   chan struct{}
	closed bool
}

func newRunQueue() *runQueue {
	return &runQueue{wake: make(chan struct{}, 1)}
}

// push enqueues id; if the queue has been closed, push is a no-op.
func (q *runQueue) push(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.items = append(q.items, id)
}

// notifyOne wakes at most one blocked pop caller. Safe to call after push.
func (q *runQueue) notifyOne() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// pop blocks until an item is available or the queue is closed and drained.
// Returns (id, true) for each dequeued item, or ("", false) once the queue
// is closed and fully drained.
func (q *runQueue) pop() (string, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			id := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return id, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return "", false
		}
		<-q.wake
	}
}

// close marks the queue as closed and wakes any blocked workers. Idempotent.
func (q *runQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.wake)
}

// schedulerContext bundles per-Run mutable state.
type schedulerContext struct {
	plan       []string
	states     map[string]*probeState
	dependents map[string][]string
	queue      *runQueue
	stateMu    sync.Mutex
	finalized  atomic.Int32
	allDone    chan struct{}
}

// Run executes every registered probe respecting the dependency DAG and the
// configured concurrency limit. The returned slice is canonically ordered
// by registry.ResolvedPlan() regardless of execution completion order.
//
// An empty registry yields an empty result without spawning goroutines.
// Direct context cancellation marks active and queued probes as
// ProbeCancelled; dependents of cancelled, failed, or skipped probes are
// ProbeSkipped with ErrDependencyFailed.
func (o *Orchestrator) Run(ctx context.Context, env platform.Environment) []ProbeResult {
	plan, err := o.registry.ResolvedPlan()
	if err != nil || len(plan) == 0 {
		return []ProbeResult{}
	}
	n := len(plan)

	states := make(map[string]*probeState, n)
	dependents := make(map[string][]string, n)
	for _, id := range plan {
		p, _ := o.registry.Get(id)
		states[id] = &probeState{remainingDeps: len(p.Dependencies())}
	}
	for _, id := range plan {
		p, _ := o.registry.Get(id)
		for _, dep := range p.Dependencies() {
			dependents[dep] = append(dependents[dep], id)
		}
	}

	queue := newRunQueue()
	for _, id := range plan {
		if states[id].remainingDeps == 0 {
			queue.push(id)
			queue.notifyOne()
		}
	}

	effective := o.maxConcurrency
	if effective > n {
		effective = n
	}

	sc := &schedulerContext{
		plan:       plan,
		states:     states,
		dependents: dependents,
		queue:      queue,
		allDone:    make(chan struct{}),
	}

	var wg sync.WaitGroup
	for i := 0; i < effective; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				id, ok := queue.pop()
				if !ok {
					return
				}
				o.processProbe(ctx, sc, id, env)
			}
		}()
	}

	<-sc.allDone
	queue.close()
	wg.Wait()

	out := make([]ProbeResult, 0, n)
	for _, id := range plan {
		s := states[id]
		if s.finalized {
			out = append(out, s.result)
			continue
		}
		// Defensive fallback; should be unreachable because allDone
		// fires only when every probe has been finalized.
		out = append(out, ProbeResult{
			ProbeID: id,
			Status:  ProbeSkipped,
			Err:     ErrDependencyFailed,
		})
	}
	return out
}

// processProbe executes a single probe and propagates the outcome to its
// direct and transitive dependents.
//
// Concurrency contract: probe.Run invocations execute WITHOUT holding
// sc.stateMu; only brief critical sections mutate per-probe state.
func (o *Orchestrator) processProbe(
	ctx context.Context,
	sc *schedulerContext,
	id string,
	env platform.Environment,
) {
	//nolint:gosec // G115: registry probe counts are bounded by realistic CLI usage and cannot exceed int32.
	totalProbes := int32(len(sc.plan))

	// Fast path: skip if already finalized by another cascade path.
	sc.stateMu.Lock()
	if sc.states[id].finalized {
		sc.stateMu.Unlock()
		o.signalCompletion(sc, totalProbes)
		return
	}
	sc.stateMu.Unlock()

	// If ctx is already cancelled, skip execution and mark directly as cancelled.
	if ctxErr := ctx.Err(); ctxErr != nil {
		o.recordFinal(sc, id, ProbeResult{
			ProbeID: id,
			Status:  ProbeCancelled,
			Err:     ctxErr,
		}, totalProbes)
		return
	}

	p, ok := o.registry.Get(id)
	if !ok {
		o.recordFinal(sc, id, ProbeResult{
			ProbeID: id,
			Status:  ProbeFailed,
			Err:     fmt.Errorf("probe %q not found in registry", id),
		}, totalProbes)
		return
	}

	// Execute the probe.
	start := time.Now()
	obs, runErr := p.Run(ctx, env)
	duration := time.Since(start)

	var result ProbeResult
	result.ProbeID = id
	result.Duration = duration
	switch {
	case runErr == nil:
		result.Status = ProbeSucceeded
		result.Observation = obs
	case ctx.Err() != nil && (runErr == ctx.Err() || errors.Is(runErr, ctx.Err())):
		result.Status = ProbeCancelled
		result.Err = ctx.Err()
	default:
		result.Status = ProbeFailed
		result.Err = runErr
	}

	o.recordFinal(sc, id, result, totalProbes)
}

// recordFinal atomically finalizes probe id with the given outcome. It
// cascades the outcome to dependents, queues newly-runnable probes for
// execution, and signals completion when the last probe finalizes.
func (o *Orchestrator) recordFinal(sc *schedulerContext, id string, result ProbeResult, totalProbes int32) {
	var newlyRunnable []string
	var cascadeCount int32

	sc.stateMu.Lock()
	s := sc.states[id]
	if s.finalized {
		sc.stateMu.Unlock()
		o.signalCompletion(sc, totalProbes)
		return
	}
	s.finalized = true
	s.result = result

	if result.Status == ProbeSucceeded {
		for _, dep := range sc.dependents[id] {
			sc.states[dep].remainingDeps--
			if sc.states[dep].remainingDeps == 0 {
				newlyRunnable = append(newlyRunnable, dep)
			}
		}
	} else {
		cascadeCount = cascadeSkipLocked(sc, sc.dependents[id])
	}
	sc.stateMu.Unlock()

	// Account for the originating probe AND the cascade-skipped probes
	// in the global finalized counter. Pre-increment cascadeCount to
	// fold them into the next signalCompletion call.
	for i := int32(0); i < cascadeCount; i++ {
		sc.finalized.Add(1)
	}

	// Enqueue newly runnable probes outside the critical section.
	for _, dep := range newlyRunnable {
		sc.queue.push(dep)
	}
	if len(newlyRunnable) > 0 {
		sc.queue.notifyOne()
	}

	o.signalCompletion(sc, totalProbes)
}

// signalCompletion increments the finalized counter and closes sc.allDone
// when the last probe has been recorded.
func (o *Orchestrator) signalCompletion(sc *schedulerContext, totalProbes int32) {
	if sc.finalized.Add(1) == totalProbes {
		select {
		case <-sc.allDone:
		default:
			close(sc.allDone)
		}
	}
}

// cascadeSkipLocked marks every transitive dependent of seeds as
// ProbeSkipped with ErrDependencyFailed. Caller must hold sc.stateMu.
// Returns the number of probes newly finalized by this cascade (excluding
// already-finalized probes).
func cascadeSkipLocked(sc *schedulerContext, seeds []string) int32 {
	var count int32
	stack := make([]string, 0, len(seeds))
	stack = append(stack, seeds...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		s := sc.states[id]
		if s.finalized {
			continue
		}
		s.finalized = true
		s.result = ProbeResult{
			ProbeID: id,
			Status:  ProbeSkipped,
			Err:     ErrDependencyFailed,
		}
		count++
		for _, dep := range sc.dependents[id] {
			if !sc.states[dep].finalized {
				stack = append(stack, dep)
			}
		}
	}
	return count
}