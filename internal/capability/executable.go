package capability

import "github.com/EpicBlackWolfZ/capagent/internal/model"

const conmonName = "conmon"

const netavarkName = "netavark"

// HelperDefinitions expose installed inventory and target metadata separately
// from runtime-selected prerequisites. None establish successful execution.
func HelperDefinitions() []Definition {
	var definitions []Definition
	for _, role := range []string{"crun", "runc", conmonName, netavarkName, "aardvark_dns", "pasta", "slirp4netns", "fuse_overlayfs"} {
		for _, access := range []bool{false, true} {
			suffix := "installed"
			if access {
				suffix = "executable"
			}
			id := model.CapabilityID("runtime.podman.helper." + role + "." + suffix)
			definitions = append(definitions, Definition{ID: id,
				Description: "Bounded trusted helper inventory; executable additionally requires target kernel access",
				Evaluate: func(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
					return inventoryEvidence(scope, observations, role, id, access)
				}})
		}
	}
	for _, role := range []string{"oci_runtime", conmonName, netavarkName, "aardvark_dns", "pasta", "slirp4netns"} {
		definitions = append(definitions, selectedExecutableDefinition(role))
	}
	return definitions
}

func inventoryEvidence(scope model.EvaluationScope, observations []model.Observation,
	role string, id model.CapabilityID, access bool,
) []model.Evidence {
	var evidence []model.Evidence
	if !localPodman(scope) {
		return nil
	}
	for _, obs := range observations {
		x := obs.Executable
		if obs.Scope != scope || x == nil || x.Role != role || x.Source != "trusted_candidates" {
			continue
		}
		state := model.StateUnsupported
		for _, c := range x.Candidates {
			next := executableState(c, access)
			if next == model.StateSupported {
				state = next
				break
			}
			if next == model.StateUnknown || state == model.StateUnsupported {
				state = next
			}
		}
		if len(x.Candidates) == 0 {
			state = model.StateUnknown
		}
		evidence = append(evidence, prerequisiteEvidence(obs, id, state, "trusted candidate metadata; custom paths require selection evidence"))
	}
	return evidence
}

func executableState(c model.ExecutableCandidate, access bool) model.CapabilityState {
	if c.Present == nil {
		return model.StateUnknown
	}
	if !*c.Present {
		return model.StateUnsupported
	}
	if c.Masked {
		return model.StateMisconfigured
	}
	if c.File == nil {
		return model.StateUnknown
	}
	if !c.File.Regular {
		return model.StateMisconfigured
	}
	if !access {
		return model.StateSupported
	}
	if c.Executable == nil {
		return model.StateUnknown
	}
	if !c.File.ExecutableBits || !*c.Executable {
		return model.StateMisconfigured
	}
	return model.StateSupported
}

func selectedExecutableDefinition(role string) Definition {
	id := model.CapabilityID("runtime.podman." + role + ".executable")
	return Definition{ID: id,
		Description: "Runtime-effective or configured helper selection has executable metadata and target access; execution unverified",
		Evaluate: func(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
			var evidence []model.Evidence
			if !localPodman(scope) {
				return nil
			}
			evidence = append(evidence, configuredExecutableEvidence(scope, observations, role, id)...)
			for _, obs := range observations {
				x := obs.Executable
				if obs.Scope != scope || x == nil || x.Role != role || x.Source != "runtime" ||
					len(x.Candidates) != 1 || x.SelectedPath == "" || x.SelectedPath != x.Candidates[0].Path {
					continue
				}
				for _, source := range observations {
					if source.Scope != scope || source.ID != x.SourceID || source.Podman == nil || source.Podman.Path != x.RuntimePath ||
						source.Podman.Available == nil || !*source.Podman.Available || selectedHelperPath(source.Podman, role) != x.SelectedPath {
						continue
					}
					state := executableState(x.Candidates[0], true)
					if state == model.StateUnsupported {
						state = model.StateMisconfigured
					}
					ev := prerequisiteEvidence(obs, id, state, "runtime-selected path and target executable metadata; execution unverified")
					// Selection is a runtime claim. The access measurement does not turn
					// a runtime-selected path into a higher-ranked selection source.
					ev.Precedence = model.PrecedenceRuntime
					includeObservation(&ev, source)
					evidence = append(evidence, ev)
				}
			}
			return evidence
		}}
}

func selectedHelperPath(p *model.PodmanInfo, role string) string {
	switch role {
	case "oci_runtime":
		if p.OCIRuntime != nil {
			return p.OCIRuntime.Path
		}
	case conmonName:
		return p.ConmonPath
	case netavarkName:
		if p.NetworkBackend != nil && *p.NetworkBackend == netavarkName {
			return p.HelperPath
		}
	case "aardvark_dns":
		return p.AardvarkPath
	case "pasta":
		return p.PastaPath
	case "slirp4netns":
		return p.SlirpPath
	}
	return ""
}

func prerequisiteEvidence(obs model.Observation, id model.CapabilityID, state model.CapabilityState, source string) model.Evidence {
	ev := runtimeEvidence(obs, id, state)
	ev.Precedence, ev.Source = model.PrecedenceLive, source
	return ev
}

func localPodman(scope model.EvaluationScope) bool {
	return scope.Runtime == "podman" && scope.Endpoint == "local"
}
