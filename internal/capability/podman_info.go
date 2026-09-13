package capability

import "github.com/EpicBlackWolfZ/capagent/internal/model"

const PodmanInfoID model.CapabilityID = "runtime.podman.info"

func PodmanInfoDefinition() Definition {
	return Definition{ID: PodmanInfoID, Description: "Selected local Podman returned complete effective runtime information",
		Evaluate: podmanInfoEvidence}
}

func podmanInfoEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	if scope.Runtime != "podman" || scope.Endpoint != "local" {
		return nil
	}
	var evidence []model.Evidence
	for _, obs := range observations {
		if obs.Scope != scope || obs.Podman == nil {
			continue
		}
		ev := runtimeEvidence(obs, PodmanInfoID, infoState(*obs.Podman))
		for _, other := range observations {
			if other.Scope != scope || other.Version == nil || other.Version.Path != obs.Podman.Path || other.Version.Version == nil {
				continue
			}
			if obs.Podman.VersionParts != nil && !sameVersion(obs.Podman.VersionParts, other.Version.Version) {
				ev.State, ev.Confidence = model.StateUnknown, model.ConfidenceUnknown
			}
			if other.ID != obs.ID {
				includeObservation(&ev, other)
			}
		}
		evidence = append(evidence, ev)
	}
	return evidence
}

func infoState(info model.PodmanInfo) model.CapabilityState {
	if info.Available == nil || (info.ServiceIsRemote != nil && *info.ServiceIsRemote) {
		return model.StateUnknown
	}
	if !*info.Available {
		return model.StateUnavailable
	}
	if info.VersionParts == nil || info.Rootless == nil {
		return model.StateUnknown
	}
	for _, value := range []*string{info.NetworkBackend, info.StorageDriver, info.CgroupVersion,
		info.CgroupManager, info.GraphRoot, info.RunRoot} {
		if value == nil || *value == "" {
			return model.StateUnknown
		}
	}
	return model.StateSupported
}

func sameVersion(a, b *model.PodmanVersion) bool {
	return a != nil && b != nil && a.Canonical == b.Canonical && a.Suffix == b.Suffix && a.Build == b.Build
}

func runtimeEvidence(obs model.Observation, id model.CapabilityID, state model.CapabilityState) model.Evidence {
	confidence := model.ConfidenceDerived
	if state == model.StateUnknown {
		confidence = model.ConfidenceUnknown
	}
	return model.Evidence{ID: obs.ID + "." + string(id), Source: "Podman effective runtime observation", Claim: string(id),
		Scope: obs.Scope, Timestamp: obs.Timestamp, Precedence: model.PrecedenceRuntime, State: state, Confidence: confidence,
		Completeness: obs.Completeness, Observations: []model.ObservationRef{{ID: obs.ID, ProbeID: obs.ProbeID}}}
}

func includeObservation(ev *model.Evidence, obs model.Observation) {
	ev.Observations = append(ev.Observations, model.ObservationRef{ID: obs.ID, ProbeID: obs.ProbeID})
	if obs.Timestamp.After(ev.Timestamp) {
		ev.Timestamp = obs.Timestamp
	}
	if obs.Completeness != model.Complete {
		ev.Completeness = model.Partial
	}
}
