package capability

import "github.com/EpicBlackWolfZ/capagent/internal/model"

const (
	StorageParsedID       model.CapabilityID = "runtime.podman.config.storage.parsed"
	StorageOverlayID      model.CapabilityID = "runtime.podman.storage.overlay"
	StorageMountProgramID model.CapabilityID = "runtime.podman.storage.mount_program.executable"
)

func StorageDefinitions() []Definition {
	return append([]Definition{
		{ID: StorageParsedID, Description: "Qualified storage sources and recognized settings are valid; unprojected options remain unvalidated",
			Evaluate: storageConfigurationEvidence},
		{ID: StorageOverlayID,
			Description: "Configured or runtime-reported storage driver is overlay; mounts and layer operations are unverified",
			Evaluate:    storageOverlayEvidence},
		{ID: StorageMountProgramID, Description: "Declared mount-program path has suitable executable metadata; storage execution is unverified",
			Evaluate: storageMountProgramEvidence},
	}, append(storagePathDefinitions(), storageKernelDefinitions()...)...)
}

func storageConfigurationEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	var result []model.Evidence
	for _, obs := range observations {
		c := obs.Configuration
		if obs.Scope != scope || c == nil || c.Family != "storage" {
			continue
		}
		ev := configurationEvidence(obs, StorageParsedID, storageParsedState(c),
			"selected storage source phases and recognized fields; replacement and root defaults are version-specific")
		bindConfigurationVersion(&ev, c, observations)
		result = append(result, ev)
	}
	return result
}

func storageParsedState(c *model.ConfigurationObservation) model.CapabilityState {
	if !c.SelectionComplete || c.Profile == "unqualified" {
		return model.StateUnknown
	}
	for _, source := range c.Sources {
		if source.Selected && (source.Status == "malformed" || source.Status == "invalid") {
			return model.StateMisconfigured
		}
	}
	if !c.ParseComplete || c.Storage == nil {
		return model.StateUnknown
	}
	if len(c.Storage.Problems) != 0 {
		return model.StateMisconfigured
	}
	return model.StateSupported
}

func storageOverlayEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	var result []model.Evidence
	for _, obs := range observations {
		if obs.Scope != scope {
			continue
		}
		if info := obs.Podman; localStorageReport(info) && info.StorageDriver != nil {
			result = append(result, runtimeEvidence(obs, StorageOverlayID, storageOverlayState(*info.StorageDriver)))
		}
		c := obs.Configuration
		if c == nil || c.Family != "storage" {
			continue
		}
		state := model.StateUnknown
		if storageParsedState(c) == model.StateSupported && c.Storage.Driver != nil {
			state = storageOverlayState(c.Storage.Driver.Value)
		}
		ev := configurationEvidence(obs, StorageOverlayID, state, "configured storage driver; runtime-effective driver takes precedence")
		bindConfigurationVersion(&ev, c, observations)
		result = append(result, ev)
	}
	return result
}

func storageOverlayState(driver string) model.CapabilityState {
	if driver == "" {
		return model.StateUnknown
	}
	if driver == "overlay" || driver == "overlay2" {
		return model.StateSupported
	}
	return model.StateUnsupported
}

func localStorageReport(info *model.PodmanInfo) bool {
	return info != nil && info.Available != nil && *info.Available && (info.ServiceIsRemote == nil || !*info.ServiceIsRemote)
}

func storageMountProgramEvidence(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	var result []model.Evidence
	for _, obs := range observations {
		x := obs.Executable
		if obs.Scope != scope || x == nil || x.Role != "storage_mount_program" || len(x.Candidates) != 1 || x.SelectedPath == "" ||
			x.Candidates[0].Path != x.SelectedPath {
			continue
		}
		for _, source := range observations {
			if source.Scope != scope || source.ID != x.SourceID {
				continue
			}
			state := executableState(x.Candidates[0], true)
			if state == model.StateUnsupported {
				state = model.StateMisconfigured
			}
			if x.Source == "runtime" {
				info := source.Podman
				if !localStorageReport(info) || info.Path != x.RuntimePath || info.StorageMountProgram != x.SelectedPath {
					continue
				}
				ev := runtimeEvidence(obs, StorageMountProgramID, state)
				includeObservation(&ev, source)
				result = append(result, ev)
			} else if x.Source == "configuration" {
				c := source.Configuration
				if c == nil || c.Family != "storage" || c.RuntimePath != x.RuntimePath {
					continue
				}
				if storageParsedState(c) != model.StateSupported || c.Storage.MountProgram == nil || c.Storage.MountProgram.Value != x.SelectedPath {
					state = model.StateUnknown
				}
				ev := configurationEvidence(obs,
					StorageMountProgramID,
					state,
					"configured mount-program path and target executable access; no storage test")
				includeObservation(&ev, source)
				bindConfigurationVersion(&ev, c, observations)
				result = append(result, ev)
			}
		}
	}
	return result
}
