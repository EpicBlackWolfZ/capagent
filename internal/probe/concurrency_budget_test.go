package probe_test

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

func TestOrchestratorWorkerBudgetBarrier(t *testing.T) {
	t.Parallel()
	const defaultCap = 8
	const explicitCap = 16
	const probeCount = 32
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			t.Parallel()
			workers := min(runtime.NumCPU(), defaultCap)
			var opts []probe.OrchestratorOption
			if explicit {
				workers = explicitCap
				opts = append(opts, probe.WithMaxConcurrency(workers))
			}
			registry := probe.NewRegistry()
			entered := make(chan struct{}, probeCount)
			release := make(chan struct{})
			var running, peak atomic.Int32
			for i := 0; i < probeCount; i++ {
				p := &fakeProbe{id: fmt.Sprint(i), run: func(ctx context.Context, _ platform.Environment) (model.Observation, error) {
					current := running.Add(1)
					defer running.Add(-1)
					for previous := peak.Load(); current > previous; previous = peak.Load() {
						if peak.CompareAndSwap(previous, current) {
							break
						}
					}
					entered <- struct{}{}
					select {
					case <-release:
						return model.Observation{}, nil
					case <-ctx.Done():
						return model.Observation{}, ctx.Err()
					}
				}}
				if err := registry.Register(p); err != nil {
					t.Fatal(err)
				}
			}
			orchestrator, err := probe.NewOrchestrator(registry, opts...)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan []probe.ProbeResult, 1)
			go func() { done <- orchestrator.Run(ctx, newEnv()) }()
			// On any assertion failure, release every blocked worker and join Run.
			defer func() {
				close(release)
				results := <-done
				if len(results) != probeCount || running.Load() != 0 {
					t.Error("workers/results not fully joined")
				}
				if int(peak.Load()) != workers {
					t.Errorf("peak=%d, want %d", peak.Load(), workers)
				}
			}()
			for i := 0; i < workers; i++ {
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("workers did not reach barrier")
				}
			}
		})
	}
}
