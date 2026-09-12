package capability_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestPodmanCLIStates(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	version := &model.PodmanVersion{Major: 5, Minor: 8, Patch: 4, Canonical: "5.8.4", Raw: "podman version 5.8.4"}
	tests := []struct {
		name      string
		discovery *model.RuntimeDiscovery
		version   *model.PodmanVersionObservation
		want      model.CapabilityState
	}{
		{"absent", &model.RuntimeDiscovery{Installed: &no}, nil, model.StateUnsupported},
		{"present deferred", &model.RuntimeDiscovery{Installed: &yes}, nil, model.StateUnknown},
		{"not executable", &model.RuntimeDiscovery{Installed: &yes, File: &model.ExecutableMetadata{Regular: true}}, nil, model.StateUnavailable},
		{"version", nil, &model.PodmanVersionObservation{Runnable: &yes, Version: version}, model.StateSupported},
		{"command failure", nil, &model.PodmanVersionObservation{Runnable: &no}, model.StateUnavailable},
		{"unparsed", nil, &model.PodmanVersionObservation{}, model.StateUnknown},
		{"no observation", nil, nil, model.StateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := dataset()
			d.Evidence = nil
			d.Observations[0].Discovery = tt.discovery
			d.Observations[0].Version = tt.version
			registry, err := capability.NewRegistry([]capability.Definition{capability.PodmanDefinition()})
			if err != nil {
				t.Fatal(err)
			}
			got, err := registry.Evaluate(scope(), d)
			if err != nil || got.Candidate.Capabilities[0].State != tt.want {
				t.Fatal(got, err)
			}
		})
	}
}
