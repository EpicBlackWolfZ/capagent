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
// Orchestrator holds an immutable execution snapshot. Constructing an
// Orchestrator triggers Registry.Resolve; cycles, missing dependencies, and
// invalid configuration are reported as construction errors.
type Orchestrator struct {
	plan           *executionPlan
	maxConcurrency int
}

// OrchestratorOption mutates an Orchestrator during construction.
type OrchestratorOption func(*Orchestrator) error

// defaultConcurrencyCap limits implicit resource use on large hosts. Explicit
// WithMaxConcurrency values may exceed this conservative default.
const defaultConcurrencyCap = 8

func defaultConcurrency(cpuCount int) int {
	return max(1, min(cpuCount, defaultConcurrencyCap))
}

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
// errors are surfaced up front. The default worker count is min(NumCPU, 8),
// with a minimum of one; WithMaxConcurrency overrides that safety default.
func NewOrchestrator(registry *Registry, opts ...OrchestratorOption) (*Orchestrator, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry cannot be nil")
	}

	o := &Orchestrator{
		maxConcurrency: defaultConcurrency(runtime.NumCPU()),
	}
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}

	if err := registry.Resolve(); err != nil {
		return nil, err
	}
	plan, err := registry.execution()
	if err != nil {
		return nil, err
	}
	o.plan = plan
	return o, nil
}

// probeState tracks the per-probe execution state used by the scheduler.
type probeState struct {
	remainingDeps int
	finalized     bool
	result        ProbeResult
}

// runQueue is a slice-backed FIFO queue with a non-blocking push and a
// blocking pop. The synchronization primitive is sync.Cond so that wakeups
// cannot be lost between an item becoming available and a consumer noticing.
//
// Semantics:
//
//   - push() appends atomically while holding the queue lock and broadcasts
//     a wakeup so any blocked pop() call observes the new item.
//   - pop() blocks until an item is available or the queue is closed and
//     drained; it returns (id, true) for each dequeued item and ("", false)
//     once the queue has been closed and fully drained.
//   - close() marks the queue closed and wakes every blocked worker.
//   - close() is idempotent; a second call is a no-op.
//   - push() after close() is a no-op; the caller cannot panic the queue.
type runQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []string
	closed bool
}

func newRunQueue() *runQueue {
	q := &runQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push enqueues id; if the queue has been closed, push is a no-op. push
// broadcasts a wakeup so that any consumer blocked in pop() re-checks the
// queue state.
func (q *runQueue) push(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.items = append(q.items, id)
	q.cond.Broadcast()
}

// pop blocks until an item is available or the queue is closed and drained.
// Returns (id, true) for each dequeued item, or ("", false) once the queue
// is closed and fully drained.
func (q *runQueue) pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return "", false
	}
	id := q.items[0]
	q.items = q.items[1:]
	return id, true
}

// close marks the queue as closed and wakes every blocked worker. Idempotent.
func (q *runQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	q.cond.Broadcast()
}

// schedulerContext bundles per-Run mutable state.
type schedulerContext struct {
	plan      *executionPlan
	states    []probeState
	queue     *runQueue
	stateMu   sync.Mutex
	finalized atomic.Int32
	allDone   chan struct{}
}

// Run executes every registered probe respecting the dependency DAG and the
// configured concurrency limit. The returned slice is canonically ordered
// by registration order regardless of execution completion order. The
// topological ResolvedPlan is separate from this output order. Concurrent
// runs require reentrant shared probes and concurrent-safe environment services.
// Run joins all its workers and never closes caller-owned environment services.
//
// An empty registry yields an empty result without spawning goroutines.
//
// Cancellation contract:
//
//   - Direct context cancellation marks active and queued probes as
//     ProbeCancelled; their Err is ctx.Err().
//   - Dependents of cancelled, failed, or skipped probes are ProbeSkipped
//     with ErrDependencyFailed. They are NEVER ProbeCancelled.
//   - Probe.Run implementations are expected to honor ctx.Done(). A probe
//     that blocks indefinitely will block its worker goroutine; the
//     orchestrator cannot forcibly interrupt arbitrary Go code.
//   - Returning ctx.Err() (or any error that wraps it via fmt.Errorf("%w", ...)
//     or errors.Is) is classified as ProbeCancelled by the orchestrator.
func (o *Orchestrator) Run(ctx context.Context, env platform.Environment) []ProbeResult {
	n := len(o.plan.nodes)
	if n == 0 {
		return []ProbeResult{}
	}
	states := make([]probeState, n)
	queue := newRunQueue()
	for i, node := range o.plan.nodes {
		states[i].remainingDeps = len(node.dependencies)
		if states[i].remainingDeps == 0 {
			queue.push(node.id)
		}
	}

	effective := o.maxConcurrency
	if effective > n {
		effective = n
	}

	sc := &schedulerContext{
		plan:    o.plan,
		states:  states,
		queue:   queue,
		allDone: make(chan struct{}),
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
				o.processProbe(ctx, sc, o.plan.indices[id], env)
			}
		}()
	}

	<-sc.allDone
	queue.close()
	wg.Wait()

	out := make([]ProbeResult, 0, n)
	for i, node := range o.plan.nodes {
		s := states[i]
		if s.finalized {
			out = append(out, s.result)
			continue
		}
		// Defensive fallback; should be unreachable because allDone
		// fires only when every probe has been finalized.
		out = append(out, ProbeResult{
			ProbeID: node.id,
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
	id int,
	env platform.Environment,
) {
	//nolint:gosec // G115: registry probe counts are bounded by realistic CLI usage and cannot exceed int32.
	totalProbes := int32(len(sc.plan.nodes))

	// Fast path: skip if already finalized by another cascade path.
	sc.stateMu.Lock()
	if sc.states[id].finalized {
		sc.stateMu.Unlock()
		return
	}
	sc.stateMu.Unlock()

	// If ctx is already cancelled, skip execution and mark directly as cancelled.
	if ctxErr := ctx.Err(); ctxErr != nil {
		o.recordFinal(sc, id, ProbeResult{
			ProbeID: sc.plan.nodes[id].id,
			Status:  ProbeCancelled,
			Err:     ctxErr,
		}, totalProbes)
		return
	}

	p := sc.plan.nodes[id].probe

	// Execute the probe.
	start := time.Now()
	obs, runErr := p.Run(ctx, env)
	duration := time.Since(start)

	var result ProbeResult
	result.ProbeID = sc.plan.nodes[id].id
	result.Duration = duration
	result.Observation = obs
	switch {
	case runErr == nil:
		result.Status = ProbeSucceeded
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
func (o *Orchestrator) recordFinal(sc *schedulerContext, id int, result ProbeResult, totalProbes int32) {
	var newlyRunnable []int
	var cascadeCount int32

	sc.stateMu.Lock()
	s := &sc.states[id]
	if s.finalized {
		sc.stateMu.Unlock()
		return
	}
	s.finalized = true
	s.result = result

	if result.Status == ProbeSucceeded {
		for _, dep := range sc.plan.nodes[id].downstream {
			sc.states[dep].remainingDeps--
			if sc.states[dep].remainingDeps == 0 {
				newlyRunnable = append(newlyRunnable, dep)
			}
		}
	} else {
		cascadeCount = cascadeSkipLocked(sc, sc.plan.nodes[id].downstream)
	}
	sc.stateMu.Unlock()

	// Account for the originating probe AND the cascade-skipped probes
	// in the global finalized counter. Pre-increment cascadeCount to
	// fold them into the next signalCompletion call.
	for i := int32(0); i < cascadeCount; i++ {
		sc.finalized.Add(1)
	}

	// Enqueue newly runnable probes outside the critical section. push()
	// broadcasts a wakeup so any blocked consumer re-checks the queue.
	for _, dep := range newlyRunnable {
		sc.queue.push(sc.plan.nodes[dep].id)
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
func cascadeSkipLocked(sc *schedulerContext, seeds []int) int32 {
	var count int32
	stack := make([]int, 0, len(seeds))
	stack = append(stack, seeds...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		s := &sc.states[id]
		if s.finalized {
			continue
		}
		s.finalized = true
		s.result = ProbeResult{
			ProbeID: sc.plan.nodes[id].id,
			Status:  ProbeSkipped,
			Err:     ErrDependencyFailed,
		}
		count++
		for _, dep := range sc.plan.nodes[id].downstream {
			if !sc.states[dep].finalized {
				stack = append(stack, dep)
			}
		}
	}
	return count
}
