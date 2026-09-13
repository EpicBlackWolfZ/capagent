package capability

import "github.com/EpicBlackWolfZ/capagent/internal/model"

const (
	QuadletGeneratorID        model.CapabilityID = "runtime.podman.quadlet.generator"
	PodmanCgroupV2ID          model.CapabilityID = "runtime.podman.cgroup_v2"
	QuadletManagerID          model.CapabilityID = "runtime.podman.quadlet.manager"
	QuadletRuntimeDirectoryID model.CapabilityID = "runtime.podman.quadlet.runtime_directory"
	QuadletLingerID           model.CapabilityID = "runtime.podman.quadlet.linger"
)

func QuadletDefinitions(rootless bool) []Definition {
	definitions := []Definition{
		{ID: QuadletGeneratorID, Description: "Selected user/system Quadlet generator has suitable executable metadata; generation unverified",
			Evaluate: func(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
				return quadletGeneratorEvidence(scope, observations, rootless)
			}},
		{ID: PodmanCgroupV2ID, Description: "Host exposes unified cgroup v2 required by Quadlet; controller delegation unverified",
			Evaluate: cgroupV2Evidence},
	}
	for _, id := range []model.CapabilityID{QuadletManagerID, QuadletRuntimeDirectoryID, QuadletLingerID} {
		definitions = append(definitions, Definition{ID: id, Description: sessionDescription(id, rootless),
			Evaluate: func(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
				return sessionEvidence(scope, observations, id, rootless)
			}})
	}
	return definitions
}

func quadletGeneratorEvidence(scope model.EvaluationScope, observations []model.Observation, rootless bool) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	source := "systemd_system_generators"
	if rootless {
		source = "systemd_user_generators"
	}
	var evidence []model.Evidence
	for _, obs := range observations {
		x := obs.Executable
		if obs.Scope != scope || x == nil || x.Role != "quadlet" || x.Source != source {
			continue
		}
		state := model.StateUnsupported
		for _, c := range x.Candidates {
			if c.Path == x.SelectedPath {
				state = executableState(c, true)
				break
			}
			if c.Present == nil || *c.Present {
				state = model.StateUnknown
			}
		}
		if len(x.Candidates) == 0 {
			state = model.StateUnknown
		}
		evidence = append(evidence, prerequisiteEvidence(obs, QuadletGeneratorID, state,
			"systemd generator precedence and masking; metadata does not verify functional unit generation"))
	}
	return evidence
}

func cgroupV2State(mode string) model.CapabilityState {
	switch mode {
	case "v2":
		return model.StateSupported
	case "v1", "mixed":
		return model.StateUnsupported
	case "unavailable":
		return model.StateUnavailable
	default:
		return model.StateUnknown
	}
}

func cgroupV2Evidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	var evidence []model.Evidence
	for _, obs := range observations {
		if obs.Scope != scope {
			continue
		}
		if obs.Host != nil && obs.Host.Cgroups != nil {
			evidence = append(evidence, prerequisiteEvidence(obs, PodmanCgroupV2ID, cgroupV2State(obs.Host.Cgroups.Mode),
				"live cgroup hierarchy; controller delegation and workload execution remain unverified"))
		}
		if p := obs.Podman; p != nil && p.Available != nil && *p.Available && p.CgroupVersion != nil {
			evidence = append(evidence, runtimeEvidence(obs, PodmanCgroupV2ID, cgroupV2State(*p.CgroupVersion)))
		}
	}
	return evidence
}

func sessionEvidence(scope model.EvaluationScope, observations []model.Observation, id model.CapabilityID, rootless bool) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	var evidence []model.Evidence
	for _, obs := range observations {
		if obs.Scope != scope {
			continue
		}
		state := model.StateUnknown
		if !rootless {
			if obs.Host == nil || obs.Host.Systemd == nil {
				continue
			}
			state = model.StateUnsupported
			if id == QuadletManagerID {
				state = booleanState(obs.Host.Systemd.Running, model.StateUnavailable)
			}
		} else {
			if obs.UserContext == nil {
				continue
			}
			u := obs.UserContext
			switch id {
			case QuadletManagerID:
				state = userManagerState(u)
			case QuadletRuntimeDirectoryID:
				state = booleanState(u.Runtime.Valid, model.StateMisconfigured)
			case QuadletLingerID:
				state = booleanState(u.LingerEnabled, model.StateUnsupported)
			}
		}
		evidence = append(evidence, prerequisiteEvidence(obs, id, state, sessionDescription(id, rootless)))
	}
	return evidence
}

func booleanState(value *bool, negative model.CapabilityState) model.CapabilityState {
	if value == nil {
		return model.StateUnknown
	}
	if *value {
		return model.StateSupported
	}
	return negative
}

func userManagerState(u *model.UserContextObservation) model.CapabilityState {
	if u.Accessible != nil {
		return booleanState(u.Accessible, model.StateUnavailable)
	}
	if u.Runtime.Valid != nil && !*u.Runtime.Valid {
		return model.StateMisconfigured
	}
	if u.SocketPresent != nil && !*u.SocketPresent {
		return model.StateUnavailable
	}
	if u.SocketValid != nil && !*u.SocketValid {
		return model.StateMisconfigured
	}
	return model.StateUnknown
}

func sessionDescription(id model.CapabilityID, rootless bool) string {
	if !rootless && id != QuadletManagerID {
		return "user-session prerequisite does not apply to rootful system services"
	}
	switch id {
	case QuadletRuntimeDirectoryID:
		return "rootless services require a target-owned mode 0700 runtime directory"
	case QuadletLingerID:
		return "linger marker supports user-manager startup without login; running services and boot persistence are unverified"
	default:
		if rootless {
			return "target user-manager accessibility requires the explicit active query; socket presence alone is insufficient"
		}
		return "rootful Quadlet requires the system manager to be running; unit activation is unverified"
	}
}
