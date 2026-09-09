package probe

import (
	"container/heap"
	"fmt"
	"sync"
)

// Registry collects probes and resolves their declared dependencies into a
// canonical topological execution plan.
//
// State machine:
//
//	[Open]  --Register()-->  [Open]
//	[Open]  --Resolve()-->   [Resolved]
//	[Resolved]  --Register()-->  ErrRegistryResolved
//	[Resolved]  --Resolve()-->   no-op (idempotent)
//
// Once a registry has reached Resolved, its execution plan MUST NOT change.
// The plan returned by ResolvedPlan() is byte-identical across calls for
// the lifetime of the resolved registry; subsequent Register calls are
// rejected with ErrRegistryResolved.
//
// Concurrency: Registry is safe for concurrent use. Reads (Get, All,
// ResolvedPlan) may proceed in parallel; Register and Resolve serialize
// against each other and against reads.
type Registry struct {
	mu sync.RWMutex

	probes     map[string]Probe
	orderedIDs []string
	regIndex   map[string]int

	resolved     bool
	resolvedPlan []string
}

// NewRegistry constructs an empty probe registry.
func NewRegistry() *Registry {
	return &Registry{
		probes:   make(map[string]Probe),
		regIndex: make(map[string]int),
	}
}

// Register adds a probe to the registry.
//
// Register is rejected with ErrRegistryResolved if the registry has already
// been resolved. It is also rejected if the probe is nil, if its ID is
// empty, or if the ID has already been registered.
//
// Register preserves the call order in r.orderedIDs and r.regIndex, which is
// used as the deterministic tie-breaker for topological ordering.
func (r *Registry) Register(p Probe) error {
	if p == nil {
		return fmt.Errorf("cannot register nil probe")
	}
	id := p.ID()
	if id == "" {
		return fmt.Errorf("cannot register probe with empty ID")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.resolved {
		return ErrRegistryResolved
	}
	if _, exists := r.probes[id]; exists {
		return fmt.Errorf("duplicate probe ID %q", id)
	}

	r.probes[id] = p
	r.regIndex[id] = len(r.orderedIDs)
	r.orderedIDs = append(r.orderedIDs, id)
	return nil
}

// Resolve validates dependencies and computes the canonical topological
// execution plan via Kahn's algorithm with a registration-order tie-breaker.
//
// Resolve returns ErrCycleDetected for any cycle (including self-dependency)
// and an error if any declared dependency references an unknown probe ID.
// Subsequent calls on a resolved registry are no-ops and return the cached
// plan's validation status.
func (r *Registry) Resolve() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.resolved {
		return nil
	}

	// Validate that every declared dependency refers to a known probe.
	for _, id := range r.orderedIDs {
		deps := r.probes[id].Dependencies()
		for _, dep := range deps {
			if dep == id {
				r.resolved = true
				r.resolvedPlan = nil
				return fmt.Errorf("%w: probe %q depends on itself", ErrCycleDetected, id)
			}
			if _, ok := r.probes[dep]; !ok {
				r.resolved = true
				r.resolvedPlan = nil
				return fmt.Errorf("probe %q declares unknown dependency %q", id, dep)
			}
		}
	}

	plan, err := kahnTopoSort(r.orderedIDs, r.probes, r.regIndex)
	if err != nil {
		r.resolved = true
		r.resolvedPlan = nil
		return err
	}

	r.resolved = true
	r.resolvedPlan = plan
	return nil
}

// All returns every registered probe in registration order.
func (r *Registry) All() []Probe {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Probe, 0, len(r.orderedIDs))
	for _, id := range r.orderedIDs {
		out = append(out, r.probes[id])
	}
	return out
}

// Get returns the probe registered under id, if any.
func (r *Registry) Get(id string) (Probe, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.probes[id]
	return p, ok
}

// ResolvedPlan returns the canonical topological execution plan computed by
// the most recent successful Resolve call. If Resolve has not yet been
// called, or the most recent Resolve failed, an error is returned along
// with whatever (possibly empty) plan is currently cached.
func (r *Registry) ResolvedPlan() ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.resolved {
		return nil, fmt.Errorf("registry has not been resolved")
	}
	if r.resolvedPlan == nil {
		return nil, fmt.Errorf("registry resolved in error state")
	}
	out := make([]string, len(r.resolvedPlan))
	copy(out, r.resolvedPlan)
	return out, nil
}

// kahnTopoSort implements Kahn's algorithm using a min-heap keyed by
// registration index. Nodes with in-degree zero are emitted in strict
// registration order; the heap guarantees deterministic output across runs.
//
// Returns ErrCycleDetected when the resulting plan contains fewer nodes
// than the input set, indicating that at least one cycle remains.
func kahnTopoSort(ids []string, probes map[string]Probe, regIndex map[string]int) ([]string, error) {
	// Build in-degree map: in-degree of node X = number of declared
	// dependencies pointing to X. We also build a reverse adjacency map
	// (dependents of X) to decrement in-degrees as nodes are emitted.
	inDegree := make(map[string]int, len(ids))
	dependents := make(map[string][]string, len(ids))

	for _, id := range ids {
		inDegree[id] = 0
	}
	for _, id := range ids {
		for _, dep := range probes[id].Dependencies() {
			inDegree[id]++
			dependents[dep] = append(dependents[dep], id)
		}
	}

	pq := &regOrderHeap{}
	heap.Init(pq)
	for _, id := range ids {
		if inDegree[id] == 0 {
			heap.Push(pq, heapEntry{id: id, order: regIndex[id]})
		}
	}

	plan := make([]string, 0, len(ids))
	for pq.Len() > 0 {
		entry := heap.Pop(pq).(heapEntry)
		plan = append(plan, entry.id)
		for _, dep := range dependents[entry.id] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				heap.Push(pq, heapEntry{id: dep, order: regIndex[dep]})
			}
		}
	}

	if len(plan) != len(ids) {
		return nil, fmt.Errorf("%w: %d of %d nodes reached", ErrCycleDetected, len(plan), len(ids))
	}
	return plan, nil
}

// heapEntry pairs a probe ID with its registration-order index.
type heapEntry struct {
	id    string
	order int
}

// regOrderHeap is a min-heap ordered by registration order (lowest first),
// giving a deterministic tie-breaker when multiple nodes have in-degree zero.
type regOrderHeap []heapEntry

func (h regOrderHeap) Len() int           { return len(h) }
func (h regOrderHeap) Less(i, j int) bool { return h[i].order < h[j].order }
func (h regOrderHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *regOrderHeap) Push(x any) {
	*h = append(*h, x.(heapEntry))
}

func (h *regOrderHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
