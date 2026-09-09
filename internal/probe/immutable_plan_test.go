package probe_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

const (
	snapshotChild   = "child"
	snapshotRoot    = "root"
	snapshotMissing = "missing"
)

type changingProbe struct {
	id    string
	deps  []string
	calls int
}

func (p *changingProbe) ID() string { return p.id }
func (p *changingProbe) Dependencies() []string {
	p.calls++
	if p.calls > 1 {
		return nil
	}
	return p.deps
}
func (p *changingProbe) Run(context.Context, platform.Environment) (model.Observation, error) {
	return model.Observation{}, nil
}

func TestRegistry_CapturesDeclarationsOnce(t *testing.T) {
	t.Parallel()
	r := probe.NewRegistry()
	p := &changingProbe{id: snapshotChild, deps: []string{snapshotRoot}}
	if err := r.Register(p); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(benchmarkProbe{id: snapshotRoot}); err != nil {
		t.Fatal(err)
	}
	if err := r.Resolve(); err != nil {
		t.Fatal(err)
	}
	ids, err := r.ResolvedPlan()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{snapshotRoot, snapshotChild}) {
		t.Errorf("validated plan = %v", ids)
	}
	ids[0] = "changed"
	p.deps[0] = snapshotMissing
	p.id = "renamed"
	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatal(err)
	}
	const runs = 2
	for range runs {
		got := o.Run(context.Background(), newEnv())
		if !reflect.DeepEqual(orderOfIDs(got), []string{snapshotChild, snapshotRoot}) {
			t.Errorf("registration output = %v", orderOfIDs(got))
		}
	}
	if p.calls != 1 {
		t.Errorf("Dependencies called %d times, want once", p.calls)
	}
}

func TestOrchestrator_SnapshotPreservesPrerequisite(t *testing.T) {
	t.Parallel()
	r := probe.NewRegistry()
	deps := []string{snapshotRoot, snapshotRoot}
	rootDone := false
	if err := r.Register(&fakeProbe{id: snapshotChild, deps: deps,
		run: func(context.Context, platform.Environment) (model.Observation, error) {
			if !rootDone {
				t.Error("child before root")
			}
			return model.Observation{}, nil
		}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&fakeProbe{id: snapshotRoot, run: func(context.Context, platform.Environment) (model.Observation, error) {
		rootDone = true
		return model.Observation{}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	got := o.Run(context.Background(), newEnv())
	if got[0].Status != probe.ProbeSucceeded {
		t.Fatal(got)
	}
}
