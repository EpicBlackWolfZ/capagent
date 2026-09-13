package capability

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"path"
	"slices"
	"strings"
)

func networkEngineSource(source model.Observation, observations []model.Observation) *model.Observation {
	c := source.Configuration
	if c == nil || c.Network == nil {
		return nil
	}
	for _, engine := range observations {
		e := engine.Configuration
		if engine.ID != c.Network.EngineSourceID || engine.Scope != source.Scope || e == nil || e.Family != "engine" ||
			e.RuntimePath != c.RuntimePath || e.Profile != c.Profile || e.VersionSourceID != c.VersionSourceID ||
			engineParsedState(e) != model.StateSupported {
			continue
		}
		if env := e.Engine.Environment; env != nil && (env.Count != 0 || env.InheritedDefault) {
			return nil
		}
		return &engine
	}
	return nil
}

func networkPathsMatch(plan model.ConfigList, x *model.ExecutableObservation) bool {
	if plan.InheritedDefault || len(x.Candidates) > len(plan.Values) {
		return false
	}
	for i, candidate := range x.Candidates {
		if candidate.Path != plan.Values[i] {
			return false
		}
	}
	if x.SelectedPath == "" {
		return len(x.Candidates) == len(plan.Values)
	}
	return len(x.Candidates) != 0 && x.Candidates[len(x.Candidates)-1].Path == x.SelectedPath
}

func networkHelperState(c *model.ConfigurationObservation, x *model.ExecutableObservation, access bool) model.CapabilityState {
	if networkSourceState(c) != model.StateSupported {
		return model.StateUnknown
	}
	plan, known := c.Network.HelperPaths[x.Role]
	if !known || !networkPathsMatch(plan, x) {
		return model.StateUnknown
	}
	state := model.StateMisconfigured
	for _, candidate := range x.Candidates {
		if candidate.Present == nil {
			state = model.StateUnknown
		}
	}
	if x.SelectedPath != "" {
		candidate := x.Candidates[len(x.Candidates)-1]
		state = executableState(candidate, access)
		if !access && state == model.StateSupported && !candidate.File.ExecutableBits {
			state = model.StateMisconfigured
		}
	}
	if state == model.StateUnsupported {
		return model.StateMisconfigured
	}
	return state
}

func networkConfiguredExecutableEvidence(scope model.EvaluationScope, observations []model.Observation,
	role string, id model.CapabilityID) []model.Evidence {
	var evidence []model.Evidence
	for _, obs := range observations {
		x := obs.Executable
		if obs.Scope != scope || x == nil || x.Source != configurationSourceName || x.Role != role {
			continue
		}
		for _, source := range observations {
			c := source.Configuration
			if source.ID != x.SourceID || source.Scope != scope || c == nil || c.Family != "network" || c.RuntimePath != x.RuntimePath {
				continue
			}
			state := model.StateUnknown
			engine := networkEngineSource(source, observations)
			if engine != nil && networkPlanBound(engine.Configuration.Engine, c.Network, role) {
				state = networkHelperState(c, x, true)
			}
			ev := configurationEvidence(obs, id, state, "configured network helper search and target executable access; execution unverified")
			includeObservation(&ev, source)
			if engine != nil {
				includeObservation(&ev, *engine)
			}
			bindConfigurationVersion(&ev, c, observations)
			evidence = append(evidence, ev)
		}
	}
	return evidence
}

func configuredNetavarkEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	var evidence []model.Evidence
	for _, source := range observations {
		c := source.Configuration
		if source.Scope != scope || c == nil || c.Family != "network" {
			continue
		}
		state := networkChoice(c, NetworkBackendNetavarkID, true)
		if state == model.StateSupported {
			if selected := networkNetavarkHelperEvidence(source, observations); len(selected) != 0 {
				evidence = append(evidence, selected...)
				continue
			}
			state = model.StateUnknown
		}
		ev := configurationEvidence(source, NetavarkID, state, "configured backend and selected helper file metadata; networking unverified")
		bindConfigurationVersion(&ev, c, observations)
		evidence = append(evidence, ev)
	}
	return evidence
}

func networkNetavarkHelperEvidence(source model.Observation, observations []model.Observation) []model.Evidence {
	c := source.Configuration
	engine := networkEngineSource(source, observations)
	if engine == nil || !networkPlanBound(engine.Configuration.Engine, c.Network, netavarkName) {
		return nil
	}
	var evidence []model.Evidence
	for _, helper := range observations {
		x := helper.Executable
		if helper.Scope != source.Scope || x == nil || x.Source != configurationSourceName || x.Role != netavarkName ||
			x.SourceID != source.ID || x.RuntimePath != c.RuntimePath {
			continue
		}
		ev := configurationEvidence(source, NetavarkID, networkHelperState(c, x, false),
			"configured Netavark backend and selected helper executable file metadata; networking unverified")
		ev.ID += "." + helper.ID
		includeObservation(&ev, *engine)
		includeObservation(&ev, helper)
		bindConfigurationVersion(&ev, c, observations)
		evidence = append(evidence, ev)
	}
	return evidence
}

// Validate the projected search plan against the actual parent engine values,
// so a matching source ID cannot bless unrelated helper directories.
func networkPlanBound(engine *model.EngineConfiguration, network *model.NetworkConfiguration, role string) bool {
	plan, known := network.HelperPaths[role]
	if !known || plan.InheritedDefault {
		return false
	}
	if role == slirpName {
		if selected, ok := network.Strings["engine.network_cmd_path"]; ok && selected.Value != "" && !selected.Invalid {
			return slices.Equal(plan.Values, []string{selected.Value})
		}
	}
	switch role {
	case netavarkName, aardvarkName, pastaName, slirpName:
	default:
		return false
	}
	dirs := engine.HelperBinariesDir
	if dirs == nil || dirs.InheritedDefault {
		return false
	}
	var expected []string
	binary := role
	if role == aardvarkName {
		binary = "aardvark-dns"
	}
	for _, dir := range dirs.Values {
		if strings.Contains(dir, "$BINDIR") {
			return false
		}
		expected = append(expected, path.Join(dir, binary))
	}
	if role == pastaName || role == slirpName {
		expected = append(expected, "/usr/bin/"+role, "/bin/"+role)
	}
	return slices.Equal(plan.Values, expected)
}
