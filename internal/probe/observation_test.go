package probe_test

import (
	"context"
	"errors"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

func TestSnapshotRuntimePayloadOwnership(t *testing.T) {
	t.Parallel()
	present, runnable, owner := true, true, uint32(0)
	obs := model.Observation{Discovery: &model.RuntimeDiscovery{Installed: &present,
		File: &model.ExecutableMetadata{UID: &owner, GID: &owner}},
		Version: &model.PodmanVersionObservation{Runnable: &runnable, Version: &model.PodmanVersion{Canonical: "5.8.4"}}}
	copy := probe.SnapshotObservation(obs)
	*obs.Discovery.Installed = false
	*obs.Discovery.File.UID = 1000
	*obs.Version.Runnable = false
	obs.Version.Version.Canonical = changedObservation
	if !*copy.Discovery.Installed || *copy.Discovery.File.UID != 0 || *copy.Discovery.File.GID != 0 ||
		!*copy.Version.Runnable || copy.Version.Version.Canonical != "5.8.4" {
		t.Fatal("runtime observation retained mutable input aliases")
	}
}

func TestSnapshotInspectionOwnsNewFields(t *testing.T) {
	t.Parallel()
	flag, value := true, "/storage"
	obs := model.Observation{Podman: &model.PodmanInfo{VersionParts: &model.PodmanVersion{Canonical: "5.8.4"},
		GraphRoot: &value, RunRoot: &value, ServiceIsRemote: &flag, Rootless: &flag, Available: &flag,
		NetworkBackend: &value, StorageDriver: &value, CgroupVersion: &value, CgroupManager: &value},
		PodmanHelper: &model.PodmanHelper{Present: &flag}}
	copy := probe.SnapshotObservation(obs)
	flag, value, obs.Podman.VersionParts.Canonical = false, changedObservation, changedObservation
	for _, field := range []*string{copy.Podman.GraphRoot, copy.Podman.RunRoot, copy.Podman.NetworkBackend,
		copy.Podman.StorageDriver, copy.Podman.CgroupVersion, copy.Podman.CgroupManager} {
		if *field != "/storage" {
			t.Fatal("retained mutable inspection fields")
		}
	}
	if !*copy.Podman.ServiceIsRemote || !*copy.Podman.Rootless || !*copy.Podman.Available || !*copy.PodmanHelper.Present ||
		copy.Podman.VersionParts.Canonical != "5.8.4" {
		t.Fatal("retained mutable inspection version or booleans")
	}
}

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
	backend = changedObservation
	obs.Facts[0].RawData[0] = 'X'
	obs.Diagnostics[0].Code = changedObservation
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

const changedObservation = "mutated observation"

func TestSubIDObservationOwnsRangesAndPrivileges(t *testing.T) {
	t.Parallel()
	flag, owner, total := true, uint32(0), uint64(3)
	r := model.SubIDRange{Start: 100000, Length: 3}
	obs := model.Observation{SubIDs: &model.SubIDObservation{
		UID: model.SubIDAllocation{Present: &flag, Valid: &flag, Total: &total, Ranges: []model.SubIDRange{r},
			Records: []model.SubIDRecord{{Range: &r}}},
		Helpers: []model.MappingHelper{{UID: &owner, Executable: &flag, PrivilegeBlocked: &flag,
			Capabilities: &model.MappingCapabilities{RootID: &owner}}}}}
	copy := probe.SnapshotObservation(obs)
	flag = false
	owner = 1000
	total = 0
	r.Length = 0
	obs.SubIDs.UID.Ranges[0].Length = 0
	if !*copy.SubIDs.UID.Valid || *copy.SubIDs.UID.Total != 3 || copy.SubIDs.UID.Ranges[0].Length != 3 ||
		copy.SubIDs.UID.Records[0].Range.Length != 3 || *copy.SubIDs.Helpers[0].UID != 0 || !*copy.SubIDs.Helpers[0].PrivilegeBlocked ||
		*copy.SubIDs.Helpers[0].Capabilities.RootID != 0 {
		t.Fatal("subordinate observation retained input aliases")
	}
}
