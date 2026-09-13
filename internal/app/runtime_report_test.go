package app

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestRuntimeProjectionOrderAndPointerOwnership(t *testing.T) {
	t.Parallel()
	scope := evaluationInput().Scope
	yes, no, root, manager := true, false, "/data", "systemd"
	version := &model.PodmanVersion{Canonical: testInspectionVersion, Raw: "podman version 5.8.4", Suffix: "-vendor"}
	discovery := model.Observation{ID: "d", Scope: scope, Completeness: model.Complete,
		Discovery: &model.RuntimeDiscovery{Path: "/podman", Installed: &yes}}
	cli := model.Observation{ID: "v", Scope: scope, Completeness: model.Complete,
		Version: &model.PodmanVersionObservation{Path: "/podman", Version: version, Runnable: &yes}}
	info := model.Observation{ID: "i", Scope: scope, Completeness: model.Complete,
		Podman: &model.PodmanInfo{Path: "/podman", VersionParts: version, Available: &yes, Rootless: &no,
			GraphRoot: &root, RunRoot: &root, CgroupManager: &manager, CgroupVersion: &manager}}
	for _, failed := range []bool{false, true} {
		measurement := info
		if failed {
			measurement.Podman = &model.PodmanInfo{Path: "/podman", Available: &no}
		}
		want, _ := projectRuntime([]model.Observation{discovery, cli, measurement}, scope)
		for _, order := range [][]model.Observation{
			{discovery, measurement, cli}, {measurement, cli, discovery}, {measurement, discovery, cli},
			{cli, discovery, measurement}, {cli, measurement, discovery},
		} {
			before := slices.Clone(order)
			got, diagnostics := projectRuntime(order, scope)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(order, before) || len(diagnostics) != 0 {
				t.Fatal("projection depends on observation order or mutates input")
			}
			if got.VersionDetails == nil || got.VersionDetails.Suffix != "-vendor" || got.Version != version.Canonical {
				t.Fatal("info erased CLI version details")
			}
		}
	}
	want, _ := projectRuntime([]model.Observation{discovery, cli, info}, scope)
	no, root, manager, version.Canonical = true, "mutated", "mutated", "mutated"
	if *want.Rootless || *want.GraphRoot != "/data" || *want.RunRoot != "/data" || *want.CgroupManager != "systemd" ||
		*want.CgroupVersion != "systemd" || want.VersionDetails.Canonical != testInspectionVersion {
		t.Fatal("projection retained source pointers")
	}
}

func TestRuntimeProjectionUsesLatestAndIgnoresOtherScope(t *testing.T) {
	t.Parallel()
	scope := evaluationInput().Scope
	old := model.Observation{ID: "old", Scope: scope, Timestamp: time.Unix(1, 0), Completeness: model.Partial,
		Podman: &model.PodmanInfo{Version: "4.9.4"}}
	newer := old
	newer.ID, newer.Timestamp, newer.Completeness = "new", time.Unix(2, 0), model.Complete
	newer.Podman = &model.PodmanInfo{Version: testInspectionVersion}
	other := newer
	other.Scope.ContextID, other.Timestamp, other.Podman = "other", time.Unix(3, 0), &model.PodmanInfo{Version: "bad"}
	got, _ := projectRuntime([]model.Observation{newer, other, old}, scope)
	if got.Version != testInspectionVersion || got.Completeness != string(model.Partial) {
		t.Fatal("lost latest value or collection incompleteness", got)
	}
}
