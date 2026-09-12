package capability

import "github.com/EpicBlackWolfZ/capagent/internal/model"

const PodmanID model.CapabilityID = "runtime.podman"

func PodmanDefinition() Definition {
	return Definition{ID: PodmanID, Description: "Selected Podman CLI returned a recognizable version; engine access is unverified",
		Evaluate: podmanCLIEvidence}
}

func podmanCLIEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	if scope.Runtime != "podman" || scope.Endpoint != "local" {
		return nil
	}
	var evidence []model.Evidence
	for _, obs := range observations {
		if obs.Scope != scope {
			continue
		}
		state, precedence, measured := podmanCLIState(obs)
		if !measured {
			continue
		}
		confidence := model.ConfidenceDerived
		if state == model.StateUnknown {
			confidence = model.ConfidenceUnknown
		}
		evidence = append(evidence, model.Evidence{ID: obs.ID + ".cli", Source: "Podman CLI observation", Claim: string(PodmanID),
			Scope: scope, Precedence: precedence, State: state, Confidence: confidence, Completeness: obs.Completeness,
			Timestamp: obs.Timestamp, Observations: []model.ObservationRef{{ID: obs.ID, ProbeID: obs.ProbeID}}})
	}
	return evidence
}

func podmanCLIState(obs model.Observation) (model.CapabilityState, model.EvidencePrecedence, bool) {
	if d := obs.Discovery; d != nil {
		if d.Installed != nil && !*d.Installed {
			return model.StateUnsupported, model.PrecedenceLive, true
		}
		if d.File != nil && (!d.File.Regular || !d.File.ExecutableBits) {
			return model.StateUnavailable, model.PrecedenceLive, true
		}
	}
	// Positive file presence does not make a CLI-execution claim. In particular,
	// it must not outrank the command outcome merely because stat is live data.
	if v := obs.Version; v != nil {
		if v.Runnable != nil {
			if !*v.Runnable {
				return model.StateUnavailable, model.PrecedenceRuntime, true
			}
			if v.Version != nil {
				return model.StateSupported, model.PrecedenceRuntime, true
			}
		}
		return model.StateUnknown, model.PrecedenceRuntime, true
	}
	return model.StateUnknown, model.PrecedenceRuntime, false
}
