package probe_test

import (
	"context"
	"errors"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

func TestObservationScopeAndOwnership(t *testing.T) {
	t.Parallel()
	scope := model.EvaluationScope{RunID: "run", ContextID: "user", Runtime: "podman", Endpoint: "local"}
	backend := "netavark"
	obs := model.Observation{ID: "observation", Completeness: model.Partial,
		Facts: []model.Fact{{ID: "fact", RawData: []byte("data")}}, Diagnostics: []model.Diagnostic{{Code: "partial"}},
		Podman: &model.PodmanInfo{NetworkBackend: &backend}}
	registry := probe.NewRegistry()
	registry.Register(&fakeProbe{id: "measure", run: func(_ context.Context, env platform.Environment) (model.Observation, error) {
		if env.Scope() != scope {
			t.Error("explicit scope did not reach probe")
		}
		return obs, errors.New("measurement failed")
	}})
	engine, err := probe.NewOrchestrator(registry)
	if err != nil {
		t.Fatal(err)
	}
	env := newEnv()
	out := engine.Run(t.Context(), env.WithScope(scope))
	backend = "changed"
	obs.Facts[0].RawData[0] = 'X'
	obs.Diagnostics[0].Code = "changed"
	got := out[0].Observation
	if out[0].Status != probe.ProbeFailed || got.Scope != scope || got.Facts[0].Scope != scope || env.Scope() != (model.EvaluationScope{}) {
		t.Fatal("lost failure/scope or mutated environment")
	}
	if *got.Podman.NetworkBackend != "netavark" || string(got.Facts[0].RawData) != "data" || got.Diagnostics[0].Code != "partial" {
		t.Fatal("retained observation aliases probe data")
	}
}

func TestObservationRejectsChangedScope(t *testing.T) {
	t.Parallel()
	scope := model.EvaluationScope{RunID: "run", ContextID: "user", Runtime: "podman", Endpoint: "local"}
	wrong := scope
	wrong.Endpoint = "remote"
	for _, fact := range []bool{false, true} {
		t.Run(map[bool]string{false: "observation", true: "fact"}[fact], func(t *testing.T) {
			t.Parallel()
			obs := model.Observation{Scope: wrong}
			if fact {
				obs.Scope = scope
				obs.Facts = []model.Fact{{Scope: wrong}}
			}
			registry := probe.NewRegistry()
			registry.Register(&fakeProbe{id: "measure", run: func(context.Context, platform.Environment) (model.Observation, error) {
				return obs, nil
			}})
			engine, err := probe.NewOrchestrator(registry)
			if err != nil {
				t.Fatal(err)
			}
			got := engine.Run(t.Context(), newEnv().WithScope(scope))[0]
			if got.Status != probe.ProbeFailed || !errors.Is(got.Err, probe.ErrObservationScope) {
				t.Fatal("scope mismatch succeeded")
			}
		})
	}
}
