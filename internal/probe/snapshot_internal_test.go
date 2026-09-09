package probe

import (
	"context"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

type snapshotProbe struct {
	id   string
	deps []string
}

func (p *snapshotProbe) ID() string             { return p.id }
func (p *snapshotProbe) Dependencies() []string { return p.deps }
func (p *snapshotProbe) Run(context.Context, platform.Environment) (model.Observation, error) {
	return model.Observation{}, nil
}

func TestExecutionSnapshotOwnsAdjacency(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	deps := []string{"parent"}
	if _, err := r.execution(); err == nil {
		t.Fatal("unresolved execution accepted")
	}
	if err := r.Register(&snapshotProbe{id: "child", deps: deps}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&snapshotProbe{id: "parent"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Resolve(); err != nil {
		t.Fatal(err)
	}
	deps[0] = "child"
	plan, err := r.execution()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.nodes[0].dependencies) != 1 || plan.nodes[0].dependencies[0] != 1 {
		t.Fatal("snapshot aliases caller declarations")
	}
	if len(plan.nodes[1].downstream) != 1 || plan.nodes[1].downstream[0] != 0 {
		t.Fatal("invalid downstream snapshot")
	}
	failed := NewRegistry()
	if err := failed.Register(&snapshotProbe{id: "broken", deps: []string{"absent"}}); err != nil {
		t.Fatal(err)
	}
	first := failed.Resolve()
	if first == nil {
		t.Fatal("invalid graph accepted")
	}
	if _, err := failed.execution(); err != first {
		t.Fatal("execution lost original failure")
	}
}
