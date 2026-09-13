package capability

import (
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const (
	NetworkParsedID          model.CapabilityID = "runtime.podman.config.network.parsed"
	DNSParsedID              model.CapabilityID = "runtime.podman.config.dns.parsed"
	NetworkBackendNetavarkID model.CapabilityID = "runtime.podman.network.backend.netavark"
	RootlessPastaID          model.CapabilityID = "runtime.podman.network.rootless.pasta"
	RootlessSlirpID          model.CapabilityID = "runtime.podman.network.rootless.slirp4netns"
	networkBackendKey                           = "network.network_backend"
	networkRootlessKey                          = "network.default_rootless_network_cmd"
)

func NetworkDefinitions(rootless bool) []Definition {
	var definitions []Definition
	for _, item := range []struct {
		id          model.CapabilityID
		description string
	}{
		{NetworkParsedID, "Qualified selected network settings are interpreted within documented bounds; networking unverified"},
		{DNSParsedID, "Selected container DNS projection is complete; resolution and reachability unverified"},
		{NetworkBackendNetavarkID, "Configured or runtime-effective backend choice is Netavark; helper prerequisites are separate"},
		{RootlessPastaID, "Rootless default command is pasta; helper execution and connectivity unverified"},
		{RootlessSlirpID, "Rootless default command is slirp4netns; helper execution and connectivity unverified"},
	} {
		definitions = append(definitions, Definition{ID: item.id, Description: item.description,
			Evaluate: func(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
				return networkConfigurationEvidence(scope, observations, item.id, rootless)
			}})
	}
	return definitions
}

func networkSourceState(c *model.ConfigurationObservation) model.CapabilityState {
	if !c.SelectionComplete || c.Profile == "unqualified" {
		return model.StateUnknown
	}
	for _, source := range c.Sources {
		if source.Selected && (source.Status == "malformed" || source.Status == "invalid") {
			return model.StateMisconfigured
		}
	}
	if !c.ParseComplete || c.Network == nil {
		return model.StateUnknown
	}
	return model.StateSupported
}

func networkParsedState(c *model.ConfigurationObservation, rootless, dnsOnly bool) model.CapabilityState {
	state := networkSourceState(c)
	if state != model.StateSupported {
		return state
	}
	n := c.Network
	if !dnsOnly && (n.Strings[networkBackendKey].Invalid || rootless && n.Strings[networkRootlessKey].Invalid) {
		return model.StateMisconfigured
	}
	for key, list := range n.Lists {
		if !strings.HasPrefix(key, "containers.dns_") {
			if dnsOnly || key != "engine.network_cmd_options" || !rootless || rootlessCommand(n) != slirpName {
				continue
			}
		}
		if len(list.InvalidIndices) != 0 {
			return model.StateMisconfigured
		}
		if list.InheritedDefault || len(list.UnmodeledIndices) != 0 {
			state = model.StateUnknown
		}
	}
	return state
}

func rootlessCommand(n *model.NetworkConfiguration) string {
	value, known := n.Strings[networkRootlessKey]
	if !known || value.Invalid {
		return ""
	}
	if value.Value == "" {
		return slirpName
	} // Both qualified revisions retain the explicit empty-string legacy alias.
	return value.Value
}

func networkChoice(c *model.ConfigurationObservation, id model.CapabilityID, rootless bool) model.CapabilityState {
	if networkSourceState(c) != model.StateSupported {
		return model.StateUnknown
	}
	key, wanted := networkBackendKey, netavarkName
	if id != NetworkBackendNetavarkID {
		if !rootless {
			return model.StateUnsupported
		}
		key, wanted = networkRootlessKey, pastaName
		if id == RootlessSlirpID {
			wanted = slirpName
		}
	}
	value, known := c.Network.Strings[key]
	if !known {
		return model.StateUnknown
	}
	if value.Invalid {
		return model.StateMisconfigured
	}
	choice := value.Value
	if key == networkRootlessKey {
		choice = rootlessCommand(c.Network)
	}
	return networkChoiceState(choice, wanted)
}

func networkChoiceState(choice, wanted string) model.CapabilityState {
	if choice == wanted {
		return model.StateSupported
	}
	switch choice {
	case netavarkName, "cni", pastaName, slirpName:
		return model.StateUnsupported
	}
	return model.StateUnknown
}

func networkConfigurationEvidence(scope model.EvaluationScope, observations []model.Observation,
	id model.CapabilityID, rootless bool) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	var evidence []model.Evidence
	for _, obs := range observations {
		if obs.Scope != scope {
			continue
		}
		if c := obs.Configuration; c != nil && c.Family == "network" {
			state := networkChoice(c, id, rootless)
			if id == NetworkParsedID || id == DNSParsedID {
				state = networkParsedState(c, rootless, id == DNSParsedID)
			}
			ev := configurationEvidence(obs, id, state,
				"selected network source projection; helper compatibility and workload networking remain unverified")
			if c.Network != nil && (id == RootlessPastaID || id == RootlessSlirpID) &&
				c.Network.Strings[networkRootlessKey].SourceID == c.VersionSourceID {
				ev.Precedence = model.PrecedenceKnowledge
			}
			bindConfigurationVersion(&ev, c, observations)
			evidence = append(evidence, ev)
		}
		p := obs.Podman
		if p == nil || p.Available == nil || !*p.Available {
			continue
		}
		if id == NetworkBackendNetavarkID && p.NetworkBackend != nil {
			evidence = append(evidence, runtimeEvidence(obs, id, networkChoiceState(*p.NetworkBackend, netavarkName)))
		}
		if (id == RootlessPastaID || id == RootlessSlirpID) && p.RootlessNetworkCmd != nil {
			state := model.StateUnsupported
			if rootless {
				wanted := pastaName
				if id == RootlessSlirpID {
					wanted = slirpName
				}
				state = networkChoiceState(*p.RootlessNetworkCmd, wanted)
			}
			evidence = append(evidence, runtimeEvidence(obs, id, state))
		}
	}
	return evidence
}
