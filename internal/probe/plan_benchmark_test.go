package probe_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

type benchmarkProbe struct {
	id   string
	deps []string
}

func (p benchmarkProbe) ID() string             { return p.id }
func (p benchmarkProbe) Dependencies() []string { return p.deps }
func (p benchmarkProbe) Run(context.Context, platform.Environment) (model.Observation, error) {
	return model.Observation{}, nil
}

func benchmarkRegistry(b *testing.B, shape string) *probe.Registry {
	b.Helper()
	const nodes = 32
	r := probe.NewRegistry()
	if shape == "empty" {
		return r
	}
	for i := 0; i < nodes; i++ {
		var deps []string
		switch shape {
		case "chain":
			if i > 0 {
				deps = []string{strconv.Itoa(i - 1)}
			}
		case "fan":
			if i > 0 {
				deps = []string{"0"}
			}
			if i == nodes-1 {
				deps = nil
				for j := 1; j < i; j++ {
					deps = append(deps, strconv.Itoa(j))
				}
			}
		}
		if err := r.Register(benchmarkProbe{id: strconv.Itoa(i), deps: deps}); err != nil {
			b.Fatal(err)
		}
	}
	return r
}
func BenchmarkExecutionPlan(b *testing.B) {
	for _, shape := range []string{"empty", "flat", "chain", "fan"} {
		b.Run(shape+"/resolve", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r := benchmarkRegistry(b, shape)
				if err := r.Resolve(); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(shape+"/run", func(b *testing.B) {
			const workers = 4
			o, err := probe.NewOrchestrator(benchmarkRegistry(b, shape), probe.WithMaxConcurrency(workers))
			if err != nil {
				b.Fatal(err)
			}
			env := newEnv()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				o.Run(context.Background(), env)
			}
		})
	}
}
