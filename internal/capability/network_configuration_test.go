package capability

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"testing"
	"time"
)

const networkTestProfile = "podman-5.8.4"

func TestNetworkSelectionEvidenceAndEffectivePrecedence(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"configured", "effective CNI", "invalid backend", "unknown selection", networkTestWrongVersion} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			scope := model.EvaluationScope{RunID: networkTestID, ContextID: storageTestTarget,
				Runtime: storageTestRuntime, Endpoint: storageTestEndpoint}
			at := time.Unix(1, 0)
			yes := true
			version := model.Observation{ID: storageVersionID, ProbeID: storageVersionID, Scope: scope, Timestamp: at, Completeness: model.Complete,
				Version: &model.PodmanVersionObservation{Path: configuredPodmanPath, Runnable: &yes,
					Version: &model.PodmanVersion{Canonical: networkTestVersion}}}
			c := &model.ConfigurationObservation{Family: networkTestID, Profile: networkTestProfile, VersionSourceID: version.ID,
				RuntimePath:       configuredPodmanPath,
				SelectionComplete: true, ParseComplete: true, Network: &model.NetworkConfiguration{
					Strings: map[string]model.ConfigString{"network.network_backend": {Value: "netavark", SourceID: "file"}},
					Lists:   map[string]model.ConfigList{}}}
			source := model.Observation{ID: networkTestID, ProbeID: networkTestID, Scope: scope, Timestamp: at, Completeness: model.Complete,
				Configuration: c}
			observations := []model.Observation{version, source}
			want := model.StateSupported
			switch scenario {
			case "effective CNI":
				backend := "cni"
				observations = append(observations, model.Observation{ID: "effective", ProbeID: "effective", Scope: scope, Timestamp: at,
					Completeness: model.Complete,
					Podman:       &model.PodmanInfo{Path: configuredPodmanPath, Available: &yes, NetworkBackend: &backend}})
				want = model.StateUnsupported
			case "invalid backend":
				c.Network.Strings["network.network_backend"] = model.ConfigString{Invalid: true}
				want = model.StateMisconfigured
			case "unknown selection":
				c.SelectionComplete = false
				want = model.StateUnknown
			case networkTestWrongVersion:
				version.Version.Path = "/other"
				want = model.StateUnknown
			}
			for _, d := range NetworkDefinitions(true) {
				if d.ID != NetworkBackendNetavarkID {
					continue
				}
				resolved := Resolve(scope, d.ID, at, d.Evaluate(scope, observations))
				if resolved.Capability.State != want {
					t.Fatalf("backend=%s want=%s", resolved.Capability.State, want)
				}
			}
		})
	}
}

func TestNetworkParsedStateKeepsInactiveOptionsAndDNSUncertaintySeparate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name              string
		rootless, dnsOnly bool
		want              model.CapabilityState
	}{
		{"valid", true, false, model.StateSupported},
		{"malformed source", true, false, model.StateMisconfigured},
		{"incomplete parse", true, false, model.StateUnknown},
		{"invalid DNS", true, true, model.StateMisconfigured},
		{"unmodeled DNS", true, true, model.StateUnknown},
		{"inherited DNS", true, true, model.StateUnknown},
		{"active slirp option", true, false, model.StateMisconfigured},
		{"inactive slirp option", true, false, model.StateSupported},
		{"rootful slirp option", false, false, model.StateSupported},
		{"DNS ignores slirp option", true, true, model.StateSupported},
		{"invalid rootless choice", true, false, model.StateMisconfigured},
		{"rootful ignores rootless choice", false, false, model.StateSupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := networkMappingObservations()[2].Configuration
			n := c.Network
			n.Strings[networkRootlessKey] = model.ConfigString{Value: slirpName}
			n.Lists = map[string]model.ConfigList{}
			switch test.name {
			case "malformed source":
				c.Sources = []model.ConfigurationSource{{Selected: true, Status: "malformed"}}
			case "incomplete parse":
				c.ParseComplete = false
			case "invalid DNS":
				n.Lists["containers.dns_servers"] = model.ConfigList{Values: []string{""}, InvalidIndices: []int{0}}
			case "unmodeled DNS":
				n.Lists["containers.dns_options"] = model.ConfigList{Values: []string{""}, UnmodeledIndices: []int{0}}
			case "inherited DNS":
				n.Lists["containers.dns_options"] = model.ConfigList{InheritedDefault: true}
			case "invalid rootless choice", "rootful ignores rootless choice":
				n.Strings[networkRootlessKey] = model.ConfigString{Invalid: true}
			default:
				if test.name != "valid" {
					n.Lists["engine.network_cmd_options"] = model.ConfigList{Values: []string{""}, InvalidIndices: []int{0}}
				}
				if test.name == "inactive slirp option" {
					n.Strings[networkRootlessKey] = model.ConfigString{Value: pastaName}
				}
			}
			if state := networkParsedState(c, test.rootless, test.dnsOnly); state != test.want {
				t.Fatalf("state=%s want=%s", state, test.want)
			}
		})
	}
}

func TestNetworkRootlessChoicesDefaultsAndRuntimeOverride(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		choice   string
		rootless bool
		want     model.CapabilityState
	}{
		{"", true, model.StateSupported}, {slirpName, true, model.StateSupported}, {pastaName, true, model.StateUnsupported},
		{slirpName, false, model.StateUnsupported},
	} {
		t.Run(test.choice+string(test.want), func(t *testing.T) {
			t.Parallel()
			c := networkMappingObservations()[2].Configuration
			c.Network.Strings[networkRootlessKey] = model.ConfigString{Value: test.choice}
			if state := networkChoice(c, RootlessSlirpID, test.rootless); state != test.want {
				t.Fatal("lost explicit empty legacy alias or target identity")
			}
		})
	}
	observations := networkMappingObservations()
	scope, at := observations[0].Scope, observations[0].Timestamp
	observations[2].Configuration.Network.Strings[networkRootlessKey] = model.ConfigString{Value: slirpName, SourceID: observations[0].ID}
	yes, command := true, pastaName
	observations = append(observations, model.Observation{ID: "effective-command", ProbeID: "effective-command", Scope: scope, Timestamp: at,
		Completeness: model.Complete, Podman: &model.PodmanInfo{Available: &yes, RootlessNetworkCmd: &command}})
	for _, d := range NetworkDefinitions(true) {
		if d.ID != RootlessPastaID {
			continue
		}
		result := Resolve(scope, d.ID, at, d.Evaluate(scope, observations))
		if result.Capability.State != model.StateSupported || len(result.Superseded) != 1 {
			t.Fatal("runtime choice did not outrank a sourced default")
		}
	}
}
