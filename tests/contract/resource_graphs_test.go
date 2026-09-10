package contract_test

import (
	"context"
	"errors"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

const (
	resourceGraphNodes      = 4096
	resourceGraphEdges      = 16384
	resourceAllocationLimit = 128 << 20
	resourceRepeats         = 32
)

func TestResourceGraphs(t *testing.T) {
	t.Parallel()
	for _, shape := range []string{"flat", "chain", "fan-out", "fan-in", "diamond"} {
		for _, workers := range []string{"1", "4", "16"} {
			t.Run(shape+"/"+workers, func(t *testing.T) {
				requireHardeningChild(t, helperParentTimeout, "graph", shape, workers)
			})
		}
	}
}

func TestResourceCommands(t *testing.T) {
	t.Parallel()
	requireHardeningChild(t, helperParentTimeout, "commands")
}

func TestResourceOwnership(t *testing.T) {
	t.Parallel()
	requireHardeningChild(t, helperParentTimeout, "ownership")
}

func graphDependencies(shape string, id, count int) []int {
	switch shape {
	case "chain":
		if id > 0 {
			return []int{id - 1}
		}
	case "fan-out":
		if id > 0 {
			return []int{0}
		}
	case "fan-in":
		if id == count-1 {
			deps := make([]int, id)
			for i := range deps {
				deps[i] = i
			}
			return deps
		}
	case "diamond":
		const width = 4
		first := id / width * width
		if first > 0 {
			deps := make([]int, width)
			for i := range deps {
				deps[i] = first - width + i
			}
			return deps
		}
	}
	return nil
}

func runResourceGraph(t *testing.T, shape, rawWorkers string) {
	t.Helper()
	stop := scenarioWatchdog("graph " + shape + " workers=" + rawWorkers)
	defer stop()
	workers, err := strconv.Atoi(rawWorkers)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "failed-root", "cancel-before", "cancel-running"} {
		registry := probe.NewRegistry()
		entered := make(chan struct{}, resourceGraphNodes)
		var active, peak atomic.Int32
		deps := make([][]int, resourceGraphNodes)
		blocked := make([]bool, resourceGraphNodes)
		edges, roots := 0, 0
		for id := 0; id < resourceGraphNodes; id++ {
			deps[id] = graphDependencies(shape, id, resourceGraphNodes)
			edges += len(deps[id])
			if len(deps[id]) == 0 {
				roots++
			}
			p := &scriptedProbe{id: strconv.Itoa(id)}
			for _, parent := range deps[id] {
				p.deps = append(p.deps, strconv.Itoa(parent))
				blocked[id] = blocked[id] || parent == 0 || blocked[parent]
			}
			p.run = func(ctx context.Context, _ platform.Environment) (model.Observation, error) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				if mode == "cancel-running" {
					entered <- struct{}{}
					<-ctx.Done()
					return model.Observation{Summary: partialSummary}, ctx.Err()
				}
				if mode == "failed-root" && id == 0 {
					return model.Observation{Summary: partialSummary}, errChaosFault
				}
				return model.Observation{Summary: "complete"}, nil
			}
			if err := registry.Register(p); err != nil {
				t.Fatal(err)
			}
		}
		if edges > resourceGraphEdges {
			t.Fatal("fixture exceeded edge envelope")
		}
		o, err := probe.NewOrchestrator(registry, probe.WithMaxConcurrency(workers))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		if mode == "cancel-before" {
			cancel()
		}
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		done := make(chan []probe.ProbeResult, 1)
		go func() { done <- o.Run(ctx, platform.Environment{}) }()
		var results []probe.ProbeResult
		func() {
			joined := false
			defer func() {
				cancel()
				if !joined {
					<-done
				}
			}()
			if mode == "cancel-running" {
				for range min(roots, workers) {
					<-entered
				}
				cancel()
			}
			results = <-done
			joined = true
		}()
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		if allocated > resourceAllocationLimit {
			t.Fatalf("%s allocated %d", mode, allocated)
		}
		if len(results) != resourceGraphNodes || active.Load() != 0 || peak.Load() > int32(workers) {
			t.Fatal("result/worker budget violation")
		}
		for id, result := range results {
			want := probe.ProbeSucceeded
			switch mode {
			case "failed-root":
				if id == 0 {
					want = probe.ProbeFailed
				} else if blocked[id] {
					want = probe.ProbeSkipped
				}
			case "cancel-before", "cancel-running":
				if len(deps[id]) == 0 {
					want = probe.ProbeCancelled
				} else {
					want = probe.ProbeSkipped
				}
			}
			if result.ProbeID != strconv.Itoa(id) || result.Status != want {
				t.Fatalf("%s/%s id=%d got=%v want=%v", shape, mode, id, result.Status, want)
			}
			if want == probe.ProbeSkipped && !errors.Is(result.Err, probe.ErrDependencyFailed) {
				t.Fatal("lost dependency cause")
			}
		}
		assertNoProbeWorkers(t)
		hardeningEvent(t, map[string]any{eventKind: "resource_case", "scenario": "graph/" + shape + "/" + mode,
			"nodes": resourceGraphNodes, "edges": edges, "workers": workers, "peak_workers": peak.Load(),
			"allocated_bytes": allocated, "allocation_limit": resourceAllocationLimit})
	}
}

func assertNoProbeWorkers(t *testing.T) {
	t.Helper()
	const joinGrace = 250 * time.Millisecond
	deadline := time.Now().Add(joinGrace)
	for {
		var stacks strings.Builder
		const stackDetail = 2
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, stackDetail); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stacks.String(), "capagent/internal/probe.") &&
			!strings.Contains(stacks.String(), "capagent/internal/platform.") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("probe goroutines survived join:\n%s", stacks.String())
		}
		runtime.Gosched()
	}
}

func runResourceOwnership(t *testing.T) {
	stop := scenarioWatchdog("repeated ownership")
	defer stop()
	cfg := defaultChaosConfig()
	// Warm up runtime helpers before looking for owned resource retention.
	executeChaosScenario(t, chaosScenario(cfg, 0))
	for i := 0; i < resourceRepeats; i++ {
		executeChaosScenario(t, chaosScenario(cfg, i))
		assertNoProbeWorkers(t)
	}
	checkRepeatedDescriptors(t)
	allocated := checkRepeatedOrchestrator(t)
	checkRepeatedCommands(t)
	hardeningEvent(t, map[string]any{eventKind: "resource_case", "scenario": "ownership",
		"iterations": resourceRepeats, "workers_remaining": 0, "max_run_allocated_bytes": allocated, "allocation_limit": resourceAllocationLimit})
}

func checkRepeatedOrchestrator(t *testing.T) uint64 {
	t.Helper()
	registry := probe.NewRegistry()
	mode := 0
	const modes = 3
	for id := 0; id < defaultChaosConfig().Probes; id++ {
		p := &scriptedProbe{id: strconv.Itoa(id)}
		if id > 0 {
			p.deps = []string{strconv.Itoa(id - 1)}
		}
		p.run = func(context.Context, platform.Environment) (model.Observation, error) {
			if mode == 1 && id == 0 {
				return model.Observation{}, errChaosFault
			}
			return model.Observation{}, nil
		}
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	o, err := probe.NewOrchestrator(registry)
	if err != nil {
		t.Fatal(err)
	}
	var largest uint64
	for i := 0; i < resourceRepeats; i++ {
		// The same orchestrator/probes are reused; mode changes only after Run
		// joins, respecting the sequential probe reuse contract.
		mode = i % modes
		ctx, cancel := context.WithCancel(t.Context())
		if mode == modes-1 {
			cancel()
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		results := o.Run(ctx, platform.Environment{})
		cancel()
		runtime.ReadMemStats(&after)
		largest = max(largest, after.TotalAlloc-before.TotalAlloc)
		if len(results) != defaultChaosConfig().Probes {
			t.Fatal("reused orchestrator lost results")
		}
		for id, result := range results {
			want := probe.ProbeSucceeded
			if mode != 0 {
				want = probe.ProbeSkipped
				if id == 0 {
					want = probe.ProbeFailed
					if mode == modes-1 {
						want = probe.ProbeCancelled
					}
				}
			}
			if result.Status != want || result.ProbeID != strconv.Itoa(id) {
				t.Fatal("reused orchestrator retained prior outcomes")
			}
		}
		assertNoProbeWorkers(t)
	}
	if largest > resourceAllocationLimit {
		t.Fatal("repeated allocation budget")
	}
	return largest
}
