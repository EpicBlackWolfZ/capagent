package capability

import (
	"path"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const (
	EngineParsedID         model.CapabilityID = "runtime.podman.config.engine.parsed"
	CgroupManagerSystemdID model.CapabilityID = "runtime.podman.cgroup_manager.systemd"
)

func EngineDefinitions() []Definition {
	return []Definition{
		{ID: EngineParsedID, Description: "Qualified engine sources and recognized fields parse completely; other fields unvalidated",
			Evaluate: engineParsedEvidence},
		{ID: CgroupManagerSystemdID, Description: "Selected cgroup manager is systemd; manager access and delegation are separate prerequisites",
			Evaluate: cgroupManagerEvidence},
	}
}

func engineParsedEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	var evidence []model.Evidence
	if !localPodman(scope) {
		return nil
	}
	for _, obs := range observations {
		if obs.Scope != scope || obs.Configuration == nil || obs.Configuration.Family != "engine" {
			continue
		}
		state := engineParsedState(obs.Configuration)
		ev := configurationEvidence(obs, EngineParsedID, state,
			"selected source order and recognized engine fields; absence retains unmeasured built-in defaults")
		bindConfigurationVersion(&ev, obs.Configuration, observations)
		evidence = append(evidence, ev)
	}
	return evidence
}

func engineParsedState(c *model.ConfigurationObservation) model.CapabilityState {
	if !c.SelectionComplete || c.Profile == "unqualified" {
		return model.StateUnknown
	}
	for _, source := range c.Sources {
		if source.Selected && (source.Status == "malformed" || source.Status == "invalid") {
			return model.StateMisconfigured
		}
	}
	if !c.ParseComplete || c.Engine == nil {
		return model.StateUnknown
	}
	if c.Engine.CgroupManager != nil && c.Engine.CgroupManager.Invalid {
		return model.StateMisconfigured
	}
	return model.StateSupported
}

func cgroupManagerEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	var evidence []model.Evidence
	if !localPodman(scope) {
		return nil
	}
	for _, obs := range observations {
		if obs.Scope != scope {
			continue
		}
		if p := obs.Podman; p != nil && p.Available != nil && *p.Available && p.CgroupManager != nil {
			evidence = append(evidence, runtimeEvidence(obs, CgroupManagerSystemdID, cgroupManagerState(*p.CgroupManager)))
		}
		c := obs.Configuration
		if c == nil || c.Family != "engine" {
			continue
		}
		state := model.StateUnknown
		if engineParsedState(c) == model.StateSupported && c.Engine.CgroupManager != nil {
			state = cgroupManagerState(c.Engine.CgroupManager.Value)
		}
		ev := configurationEvidence(obs, CgroupManagerSystemdID, state,
			"configured cgroup-manager choice; systemd/user-manager access and cgroup delegation remain separate")
		bindConfigurationVersion(&ev, c, observations)
		evidence = append(evidence, ev)
	}
	return evidence
}

func cgroupManagerState(value string) model.CapabilityState {
	switch value {
	case "systemd":
		return model.StateSupported
	case "cgroupfs":
		return model.StateUnsupported
	default:
		return model.StateUnknown
	}
}

func configuredExecutableEvidence(scope model.EvaluationScope, observations []model.Observation,
	role string, id model.CapabilityID,
) []model.Evidence {
	var evidence []model.Evidence
	for _, obs := range observations {
		x := obs.Executable
		if obs.Scope != scope || x == nil || x.Source != configurationSourceName || x.Role != role {
			continue
		}
		for _, source := range observations {
			c := source.Configuration
			if source.Scope != scope || source.ID != x.SourceID || c == nil || c.Family != "engine" || c.RuntimePath != x.RuntimePath {
				continue
			}
			state := model.StateUnknown
			if engineParsedState(c) == model.StateSupported && configuredPathsMatch(c.Engine, x) {
				state = model.StateMisconfigured
				for _, candidate := range x.Candidates {
					if candidate.Present == nil {
						state = model.StateUnknown
					}
				}
				if x.SelectedPath != "" {
					state = executableState(x.Candidates[len(x.Candidates)-1], true)
				}
				if state == model.StateUnsupported {
					state = model.StateMisconfigured
				}
			}
			ev := configurationEvidence(obs, id, state,
				"configured helper selection and target metadata; execution and built-in default selection remain unverified")
			includeObservation(&ev, source)
			bindConfigurationVersion(&ev, c, observations)
			evidence = append(evidence, ev)
		}
	}
	return evidence
}

func configuredPathsMatch(engine *model.EngineConfiguration, x *model.ExecutableObservation) bool {
	if len(x.Candidates) == 0 || engine.SelectionOverrides ||
		engine.Environment != nil && (engine.Environment.Count != 0 || engine.Environment.InheritedDefault) {
		return false
	}
	var names []string
	fallback := conmonName
	switch {
	case x.Role == conmonName:
		if engine.ConmonPath == nil || engine.ConmonPath.InheritedDefault {
			return false
		}
		names = append(names, engine.ConmonPath.Values...)
	case x.Role == "oci_runtime" && engine.Runtime != nil && engine.Runtime.Value != "":
		fallback = engine.Runtime.Value
		if path.IsAbs(fallback) {
			names = append(names, fallback)
		} else {
			list, ok := engine.Runtimes[fallback]
			if !ok || list.InheritedDefault {
				return false
			}
			names = append(names, list.Values...)
		}
	default:
		return false
	}
	if path.IsAbs(fallback) {
		names = append(names, fallback)
	} else {
		names = append(names, path.Join("/usr/bin", fallback), path.Join("/bin", fallback))
	}
	if len(x.Candidates) > len(names) {
		return false
	}
	for i, candidate := range x.Candidates {
		if candidate.Path != names[i] {
			return false
		}
	}
	if x.SelectedPath == "" {
		return len(x.Candidates) == len(names)
	}
	return x.Candidates[len(x.Candidates)-1].Path == x.SelectedPath
}

func configurationEvidence(obs model.Observation, id model.CapabilityID, state model.CapabilityState, description string) model.Evidence {
	ev := runtimeEvidence(obs, id, state)
	ev.Precedence, ev.Source = model.PrecedenceConfig, description
	return ev
}

func bindConfigurationVersion(ev *model.Evidence, c *model.ConfigurationObservation, observations []model.Observation) {
	for _, source := range observations {
		v := source.Version
		if source.ID != c.VersionSourceID || source.Scope != ev.Scope || v == nil || v.Path != c.RuntimePath ||
			v.Runnable == nil || !*v.Runnable || v.Version == nil || "podman-"+v.Version.Canonical != c.Profile {
			continue
		}
		includeObservation(ev, source)
		return
	}
	ev.State, ev.Confidence = model.StateUnknown, model.ConfidenceUnknown
}
