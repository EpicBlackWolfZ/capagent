package capability_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func completeInfoPayload() *model.PodmanInfo {
	available, rootless := true, false
	backend, driver, cgroup, manager := "cni", "vfs", "v1", "cgroupfs"
	graph, run := "/var/lib/containers/storage", "/run/containers/storage"
	return &model.PodmanInfo{Path: "/usr/bin/podman", Version: "5.8.4", Available: &available, Rootless: &rootless,
		VersionParts: &model.PodmanVersion{Canonical: "5.8.4"}, NetworkBackend: &backend, StorageDriver: &driver,
		CgroupVersion: &cgroup, CgroupManager: &manager, GraphRoot: &graph, RunRoot: &run}
}

func TestInfoVerdictRequiresCompleteInspection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*capability.Dataset)
		want   model.CapabilityState
	}{
		{"rootful CNI and vfs", func(*capability.Dataset) {}, model.StateSupported},
		{"missing graph root", func(d *capability.Dataset) { d.Observations[0].Podman.GraphRoot = nil }, model.StateUnknown},
		{"missing run root", func(d *capability.Dataset) { d.Observations[0].Podman.RunRoot = nil }, model.StateUnknown},
		{"missing rootless", func(d *capability.Dataset) { d.Observations[0].Podman.Rootless = nil }, model.StateUnknown},
		{"missing version", func(d *capability.Dataset) { d.Observations[0].Podman.VersionParts = nil }, model.StateUnknown},
		{"missing backend", func(d *capability.Dataset) { d.Observations[0].Podman.NetworkBackend = nil }, model.StateUnknown},
		{"missing driver", func(d *capability.Dataset) { d.Observations[0].Podman.StorageDriver = nil }, model.StateUnknown},
		{"missing cgroup", func(d *capability.Dataset) { d.Observations[0].Podman.CgroupVersion = nil }, model.StateUnknown},
		{"missing manager", func(d *capability.Dataset) { d.Observations[0].Podman.CgroupManager = nil }, model.StateUnknown},
		{"empty manager", func(d *capability.Dataset) { *d.Observations[0].Podman.CgroupManager = "" }, model.StateUnknown},
		{"unknown access", func(d *capability.Dataset) { d.Observations[0].Podman.Available = nil }, model.StateUnknown},
		{"failed info", func(d *capability.Dataset) { *d.Observations[0].Podman.Available = false }, model.StateUnavailable},
		{"partial", func(d *capability.Dataset) { d.Observations[0].Completeness = model.Partial }, model.StateUnknown},
		{"unobserved", func(d *capability.Dataset) { d.Observations[0].Podman = nil }, model.StateUnknown},
		{"different version", func(d *capability.Dataset) {
			d.Observations[0].Version = &model.PodmanVersionObservation{Path: "/usr/bin/podman",
				Version: &model.PodmanVersion{Canonical: "4.9.4"}}
		}, model.StateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := dataset()
			d.Evidence = nil
			d.Observations[0].Podman = completeInfoPayload()
			tt.change(&d)
			registry, err := capability.NewRegistry([]capability.Definition{capability.PodmanInfoDefinition()})
			if err != nil {
				t.Fatal(err)
			}
			got, err := registry.Evaluate(scope(), d)
			if err != nil || got.Candidate.Capabilities[0].State != tt.want {
				t.Fatal("incorrect inspection verdict", got, err)
			}
		})
	}
}

func TestNetavarkRequiresMatchingHelperObservation(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []string{"", "path", "info", testHelperID, "partial"} {
		t.Run(mismatch, func(t *testing.T) {
			t.Parallel()
			d := dataset()
			d.Evidence = nil
			p := completeInfoPayload()
			backend, present := "netavark", true
			p.NetworkBackend, p.HelperPath = &backend, "/usr/libexec/podman/netavark"
			d.Observations[0].Podman = p
			h := d.Observations[0]
			h.ID, h.ProbeID, h.Podman = testHelperID, testHelperID, nil
			h.Facts = append([]model.Fact(nil), h.Facts...)
			h.Facts[0].ID = "helper.fact"
			h.PodmanHelper = &model.PodmanHelper{Path: p.Path, InfoID: "obs", HelperPath: p.HelperPath, Present: &present}
			switch mismatch {
			case "path":
				h.PodmanHelper.Path = "/opt/podman"
			case "info":
				h.PodmanHelper.InfoID = "other"
			case testHelperID:
				h.PodmanHelper.HelperPath = "/other/helper"
			case "partial":
				h.Completeness = model.Partial
			}
			d.Observations = append(d.Observations, h)
			registry, err := capability.NewRegistry([]capability.Definition{capability.NetavarkDefinition()})
			if err != nil {
				t.Fatal(err)
			}
			got, err := registry.Evaluate(scope(), d)
			want := model.StateUnknown
			if mismatch == "" {
				want = model.StateSupported
			}
			if err != nil || got.Candidate.Capabilities[0].State != want {
				t.Fatal(got, err)
			}
		})
	}
}
