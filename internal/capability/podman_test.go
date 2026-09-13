package capability_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const backendNetavark = "netavark"

func TestNetavarkFiveStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, backend     string
		helper, available bool
		completeness      model.Completeness
		want              model.CapabilityState
	}{
		{"supported", backendNetavark, true, true, model.Complete, model.StateSupported},
		{"other backend", "cni", false, true, model.Complete, model.StateUnsupported},
		{"missing helper", backendNetavark, false, true, model.Complete, model.StateMisconfigured},
		{"runtime unavailable", "", false, false, model.Complete, model.StateUnavailable},
		{"incomplete", backendNetavark, true, true, model.Partial, model.StateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := dataset()
			d.Evidence = nil
			d.Observations[0].Completeness = tt.completeness
			d.Observations[0].Podman = &model.PodmanInfo{NetworkBackend: &tt.backend, Available: &tt.available}
			addHelperObservation(&d, tt.helper)
			registry, err := capability.NewRegistry([]capability.Definition{capability.NetavarkDefinition()})
			if err != nil {
				t.Fatal(err)
			}
			got, err := registry.Evaluate(scope(), d)
			if err != nil {
				t.Fatal(err)
			}
			if got.Candidate.Capabilities[0].State != tt.want {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestNetavarkUnobservedAndUnknownBackend(t *testing.T) {
	t.Parallel()
	present, backend, future := true, backendNetavark, "future-backend"
	for _, info := range []*model.PodmanInfo{nil, {}, {Available: &present}, {Available: &present, NetworkBackend: &backend},
		{Available: &present, NetworkBackend: &future}} {
		d := dataset()
		d.Evidence = nil
		d.Observations[0].Podman = info
		registry, err := capability.NewRegistry([]capability.Definition{capability.NetavarkDefinition()})
		if err != nil {
			t.Fatal(err)
		}
		got, err := registry.Evaluate(scope(), d)
		if err != nil || got.Candidate.Capabilities[0].State != model.StateUnknown {
			t.Fatal(got, err)
		}
	}
}

func TestNetavarkDoesNotEvaluateAnotherRuntime(t *testing.T) {
	t.Parallel()
	d := dataset()
	d.Evidence = nil
	selected := scope()
	selected.Runtime = "docker"
	d.Observations[0].Scope = selected
	d.Observations[0].Facts[0].Scope = selected
	available, helper, backend := true, true, backendNetavark
	d.Observations[0].Podman = &model.PodmanInfo{Available: &available, NetworkBackend: &backend}
	addHelperObservation(&d, helper)
	registry, err := capability.NewRegistry([]capability.Definition{capability.NetavarkDefinition()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Evaluate(selected, d)
	if err != nil || result.Candidate.Capabilities[0].State != model.StateUnknown {
		t.Fatal(result, err)
	}
}

func addHelperObservation(d *capability.Dataset, present bool) {
	info := &d.Observations[0]
	info.Podman.Path, info.Podman.HelperPath = "/usr/bin/podman", "/usr/libexec/podman/netavark"
	h := *info
	h.ID, h.ProbeID, h.Podman = testHelperID, testHelperID, nil
	h.Facts = append([]model.Fact(nil), info.Facts...)
	h.Facts[0].ID = "helper.fact"
	h.PodmanHelper = &model.PodmanHelper{Path: info.Podman.Path, InfoID: info.ID, HelperPath: info.Podman.HelperPath, Present: &present}
	d.Observations = append(d.Observations, h)
}

const testHelperID = "helper"
