package probe

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/tests/fuzzutil"
)

const fuzzGraphNodes = 32
const fuzzGraphEdges = 128
const fuzzEdgeWidth = 2

type fuzzProbe struct {
	id   string
	deps []string
}

func (p *fuzzProbe) ID() string             { return p.id }
func (p *fuzzProbe) Dependencies() []string { return p.deps }
func (p *fuzzProbe) Run(context.Context, platform.Environment) (model.Observation, error) {
	return model.Observation{}, nil
}

func FuzzRegistryDAG(f *testing.F) {
	for _, seed := range [][]byte{{}, {1, 0, 0}, {2, 0, 1, 1, 0}, {3, 1, 0, 2, 1}, {1, 0, 2}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		fuzzutil.Default(fuzzutil.GraphInput).Run(t, input, func() {
			a, ae := checkFuzzRegistry(t, input)
			b, be := checkFuzzRegistry(t, input)
			if !reflect.DeepEqual(a, b) || ae != be {
				t.Fatal("registry outcome depends on state outside input")
			}
		})
	})
}

func checkFuzzRegistry(t *testing.T, input []byte) ([]string, string) {
	t.Helper()
	count := 0
	if len(input) > 0 {
		count = int(input[0]) % (fuzzGraphNodes + 1)
	}
	nodes := make([]*fuzzProbe, count)
	for i := range nodes {
		nodes[i] = &fuzzProbe{id: strconv.Itoa(i)}
	}
	for i := 1; count > 0 && i+1 < len(input) && i < 1+fuzzGraphEdges*fuzzEdgeWidth; i += fuzzEdgeWidth {
		from, to := int(input[i])%count, int(input[i+1])%(count+1)
		nodes[from].deps = append(nodes[from].deps, strconv.Itoa(to))
	}
	// Independent DFS oracle checks graph validity, not the implementation's Kahn heap.
	valid := fuzzGraphValid(nodes)
	registry := NewRegistry()
	for _, p := range nodes {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	err := registry.Resolve()
	if (err == nil) != valid {
		t.Fatal("resolution disagrees with graph oracle")
	}
	if retry := registry.Resolve(); retry != err {
		t.Fatal("terminal resolution changed")
	}
	if err := registry.Register(&fuzzProbe{id: "late"}); !errors.Is(err, ErrRegistryResolved) {
		t.Fatal("registry not sealed")
	}
	plan, planErr := registry.ResolvedPlan()
	if planErr != err {
		t.Fatal("plan hides resolution error")
	}
	if err != nil {
		if _, constructErr := NewOrchestrator(registry); constructErr != err {
			t.Fatal("constructor hides resolution failure")
		}
		return nil, err.Error()
	}
	positions := make(map[string]int, len(plan))
	for i, id := range plan {
		if _, exists := positions[id]; exists {
			t.Fatal("duplicate plan node")
		}
		positions[id] = i
	}
	if len(plan) != count {
		t.Fatal("missing plan node")
	}
	for _, p := range nodes {
		for _, dep := range p.deps {
			if positions[dep] >= positions[p.id] {
				t.Fatal("prerequisite ordered after dependent")
			}
		}
		p.deps = []string{"missing-after-snapshot"}
	}
	copyPlan := append([]string(nil), plan...)
	if len(plan) > 0 {
		plan[0] = "caller-mutation"
	}
	again, err := registry.ResolvedPlan()
	if err != nil || fmt.Sprint(again) != fmt.Sprint(copyPlan) {
		t.Fatal("plan aliases mutable declarations or results")
	}
	return copyPlan, ""
}

func fuzzGraphValid(nodes []*fuzzProbe) bool {
	const visiting, finished = 1, 2
	state := make([]int, len(nodes))
	var visit func(int) bool
	visit = func(i int) bool {
		if i >= len(nodes) || state[i] == visiting {
			return false
		}
		if state[i] == finished {
			return true
		}
		state[i] = visiting
		for _, name := range nodes[i].deps {
			dependency, _ := strconv.Atoi(name)
			if !visit(dependency) {
				return false
			}
		}
		state[i] = finished
		return true
	}
	for i := range nodes {
		if !visit(i) {
			return false
		}
	}
	return true
}
