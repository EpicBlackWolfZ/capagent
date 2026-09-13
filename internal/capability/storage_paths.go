package capability

import (
	"path"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

const (
	maxSelectedStoragePaths                    = 32
	StorageRootsID          model.CapabilityID = "runtime.podman.storage.roots.accessible"
	StorageImageStoreID     model.CapabilityID = "runtime.podman.storage.image_store.accessible"
	StorageAdditionalID     model.CapabilityID = "runtime.podman.storage.additional_stores.accessible"
)

func storagePathDefinitions() []Definition {
	var result []Definition
	for _, row := range []struct {
		id          model.CapabilityID
		description string
	}{
		{StorageRootsID, "Selected graph/run roots exist and permit target write/search access; storage writes remain unverified"},
		{StorageImageStoreID, "Configured separate writable image store is absent from selection or has target directory access"},
		{StorageAdditionalID, "Configured additional read-only stores exist and permit target read/search access"},
	} {
		result = append(result, Definition{ID: row.id, Description: row.description,
			Evaluate: func(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
				return storagePathEvidence(scope, observations, row.id)
			}})
	}
	return result
}

func storagePathEvidence(scope model.EvaluationScope, observations []model.Observation, id model.CapabilityID) []model.Evidence {
	if !localPodman(scope) {
		return nil
	}
	var result []model.Evidence
	for _, source := range observations {
		if source.Scope != scope {
			continue
		}
		expected, known := expectedStoragePaths(source, id)
		if !known {
			continue
		}
		if len(expected) == 0 {
			ev := configurationEvidence(source, id, model.StateSupported, "no additional path is selected by the qualified storage configuration")
			bindConfigurationVersion(&ev, source.Configuration, observations)
			result = append(result, ev)
			continue
		}
		for _, obs := range observations {
			measured := obs.StoragePaths
			if obs.Scope != scope || measured == nil || measured.SourceID != source.ID || measured.Writable != (id != StorageAdditionalID) {
				continue
			}
			state := model.StateUnknown
			runtime := source.Podman != nil
			matches := runtime && measured.Source == "runtime" && measured.RuntimePath == source.Podman.Path ||
				!runtime && measured.Source == configurationSourceName && measured.RuntimePath == source.Configuration.RuntimePath
			if matches {
				state = selectedStoragePathsState(expected, measured.Paths, id)
			}
			ev := runtimeEvidence(obs, id, state)
			ev.Source = "selected paths and target kernel access; ancestor access cannot prove target creation or storage operation"
			if !runtime {
				ev.Precedence = model.PrecedenceConfig
				bindConfigurationVersion(&ev, source.Configuration, observations)
			}
			includeObservation(&ev, source)
			result = append(result, ev)
		}
	}
	return result
}

func expectedStoragePaths(source model.Observation, id model.CapabilityID) ([]model.StoragePathMetadata, bool) {
	var roots [3]string
	var additional [2]*model.ConfigList
	if info := source.Podman; info != nil {
		if id != StorageRootsID || !localStorageReport(info) || info.GraphRoot == nil || info.RunRoot == nil {
			return nil, false
		}
		roots[0], roots[1] = *info.GraphRoot, *info.RunRoot
	} else {
		c := source.Configuration
		if c == nil || c.Family != "storage" || storageParsedState(c) != model.StateSupported {
			return nil, false
		}
		for i, value := range []*model.ConfigString{c.Storage.GraphRoot, c.Storage.RunRoot, c.Storage.ImageStore} {
			if value != nil {
				roots[i] = value.Value
			}
		}
		additional = [2]*model.ConfigList{c.Storage.AdditionalImageStores, c.Storage.AdditionalLayerStores}
	}
	switch id {
	case StorageRootsID:
		return []model.StoragePathMetadata{{Role: "graphroot",
				Path: roots[0]},
				{Role: "runroot",
					Path: roots[1]}},
			roots[0] != "" && roots[1] != ""
	case StorageImageStoreID:
		if roots[2] == "" {
			return nil, true
		}
		return []model.StoragePathMetadata{{Role: "imagestore", Path: roots[2]}}, true
	default:
		var expected []model.StoragePathMetadata
		for i, list := range additional {
			if list == nil {
				continue
			}
			role := "additionalimagestore"
			if i != 0 {
				role = "additionallayerstore"
			}
			for _, value := range list.Values {
				for name := range strings.SplitSeq(value, ",") {
					if i != 0 {
						name = strings.TrimSuffix(name, ":ref")
					}
					if len(expected) == maxSelectedStoragePaths {
						return nil, false
					}
					expected = append(expected, model.StoragePathMetadata{Role: role, Path: path.Clean(name)})
				}
			}
		}
		return expected, true
	}
}

func selectedStoragePathsState(expected, measured []model.StoragePathMetadata, id model.CapabilityID) model.CapabilityState {
	var selected []model.StoragePathMetadata
	for _, item := range measured {
		include := id == StorageRootsID && (item.Role == "graphroot" || item.Role == "runroot") ||
			id == StorageImageStoreID && item.Role == "imagestore" || id == StorageAdditionalID
		if include {
			selected = append(selected, item)
		}
	}
	if len(selected) != len(expected) {
		return model.StateUnknown
	}
	state := model.StateSupported
	for i, item := range selected {
		if item.Path != expected[i].Path || item.Role != expected[i].Role {
			return model.StateUnknown
		}
		candidate := storageDirectoryState(item, id)
		if candidate == model.StateMisconfigured {
			state = candidate
		} else if candidate == model.StateUnknown && state == model.StateSupported {
			state = candidate
		}
	}
	return state
}

func storageDirectoryState(item model.StoragePathMetadata, id model.CapabilityID) model.CapabilityState {
	if item.Present == nil {
		return model.StateUnknown
	}
	if !*item.Present {
		if id == StorageAdditionalID {
			return model.StateMisconfigured
		}
		return model.StateUnknown
	}
	if item.Ancestor || item.CheckedPath != item.Path {
		return model.StateUnknown
	}
	if item.Directory == nil {
		return model.StateUnknown
	}
	if !*item.Directory {
		return model.StateMisconfigured
	}
	if item.Accessible == nil {
		return model.StateUnknown
	}
	if !*item.Accessible {
		return model.StateMisconfigured
	}
	if id != StorageAdditionalID {
		if item.Filesystem == nil || item.Filesystem.ReadOnly == nil {
			return model.StateUnknown
		}
		if *item.Filesystem.ReadOnly {
			return model.StateMisconfigured
		}
	}
	return model.StateSupported
}
