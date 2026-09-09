package probe_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

type waitingBenchmarkProbe struct{ benchmarkProbe }

func (p waitingBenchmarkProbe) Run(ctx context.Context, _ platform.Environment) (model.Observation, error) {
	const wait = 100 * time.Microsecond
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return model.Observation{}, ctx.Err()
	case <-timer.C:
		return model.Observation{}, nil
	}
}

func BenchmarkOrchestratorConcurrency(b *testing.B) {
	const nodes = 32
	for _, waiting := range []bool{false, true} {
		for _, workers := range []int{0, 1, 4, 8, 16} {
			for _, shape := range []string{"flat", "chain", "fan"} {
				b.Run(fmt.Sprintf("waiting=%t/workers=%d/%s", waiting, workers, shape), func(b *testing.B) {
					registry := probe.NewRegistry()
					for i := 0; i < nodes; i++ {
						p := benchmarkProbe{id: fmt.Sprint(i)}
						if i > 0 && shape == "chain" {
							p.deps = []string{fmt.Sprint(i - 1)}
						}
						if i > 0 && shape == "fan" {
							p.deps = []string{"0"}
						}
						var implementation probe.Probe = p
						if waiting {
							implementation = waitingBenchmarkProbe{p}
						}
						if err := registry.Register(implementation); err != nil {
							b.Fatal(err)
						}
					}
					var opts []probe.OrchestratorOption
					if workers > 0 {
						opts = append(opts, probe.WithMaxConcurrency(workers))
					}
					orchestrator, err := probe.NewOrchestrator(registry, opts...)
					if err != nil {
						b.Fatal(err)
					}
					env := newEnv()
					b.ReportAllocs()
					for b.Loop() {
						orchestrator.Run(context.Background(), env)
					}
				})
			}
		}
	}
}
