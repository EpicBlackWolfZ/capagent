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
		state := netavarkState(*obs.Podman)
		confidence := model.ConfidenceDerived
		if state == model.StateUnknown {
			confidence = model.ConfidenceUnknown
		}
		evidence = append(evidence, model.Evidence{ID: obs.ID + ".netavark", Source: "podman effective info and helper metadata",
			Claim: string(NetavarkID), Scope: scope, Precedence: model.PrecedenceRuntime, State: state, Confidence: confidence,
			Completeness: obs.Completeness, Timestamp: obs.Timestamp, Observations: []model.ObservationRef{{ID: obs.ID, ProbeID: obs.ProbeID}}})
	}
	return evidence
}

func netavarkState(info model.PodmanInfo) model.CapabilityState {
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
		if info.HelperPresent == nil {
			return model.StateUnknown
		}
		if !*info.HelperPresent {
			return model.StateMisconfigured
		}
		return model.StateSupported
	default:
		return model.StateUnknown
	}
}
