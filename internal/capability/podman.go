package capability

import "github.com/EpicBlackWolfZ/capagent/internal/model"

const NetavarkID model.CapabilityID = "runtime.podman.netavark"

// NetavarkDefinition describes the configured backend plus helper metadata.
// It does not assert successful network creation or container DNS resolution.
func NetavarkDefinition() Definition {
	return Definition{ID: NetavarkID, Description: "Netavark is the configured backend and its helper has executable file metadata",
		Evaluate: netavarkEvidence}
}

func netavarkEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	if scope.Runtime != "podman" {
		return nil
	}
	var evidence []model.Evidence
	for _, obs := range observations {
		if obs.Scope != scope || obs.Podman == nil {
			continue
		}
		matched := false
		for _, helper := range observations {
			h := helper.PodmanHelper
			if h == nil || helper.Scope != scope || h.InfoID != obs.ID || h.Path != obs.Podman.Path || h.HelperPath != obs.Podman.HelperPath {
				continue
			}
			ev := runtimeEvidence(obs, NetavarkID, netavarkState(*obs.Podman, h.Present))
			ev.ID += "." + helper.ID
			includeObservation(&ev, helper)
			evidence = append(evidence, ev)
			matched = true
		}
		if !matched {
			evidence = append(evidence, runtimeEvidence(obs, NetavarkID, netavarkState(*obs.Podman, nil)))
		}
	}
	return evidence
}

func netavarkState(info model.PodmanInfo, helper *bool) model.CapabilityState {
	if info.Available == nil {
		return model.StateUnknown
	}
	if !*info.Available {
		return model.StateUnavailable
	}
	if info.NetworkBackend == nil {
		return model.StateUnknown
	}
	switch *info.NetworkBackend {
	case "cni":
		return model.StateUnsupported
	case "netavark":
		if helper == nil {
			return model.StateUnknown
		}
		if !*helper {
			return model.StateMisconfigured
		}
		return model.StateSupported
	default:
		return model.StateUnknown
	}
}
