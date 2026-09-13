package podman

import (
	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func expandStorageRoots(storage *model.StorageConfiguration, values map[string]string, uid uint32) error {
	for _, field := range []**model.ConfigString{&storage.RunRoot, &storage.GraphRoot, &storage.RootlessStoragePath} {
		if !nonemptyConfig(*field) {
			if field == &storage.RootlessStoragePath {
				continue
			}
			if uid != 0 {
				return config.ErrConfigFieldUnsupported
			}
			problem := "graphroot_required"
			if field == &storage.RunRoot {
				problem = "runroot_required"
			}
			storage.Problems = append(storage.Problems, problem)
			continue
		}
		expanded, err := config.ExpandStoragePath((*field).Value, values, uid)
		if err != nil {
			return err
		}
		*field = &model.ConfigString{Value: expanded, SourceID: (*field).SourceID}
	}
	if storage.Driver != nil && storage.Driver.Value == storageDriverOverlay2 {
		storage.Driver = &model.ConfigString{Value: storageDriverOverlay, SourceID: storage.Driver.SourceID}
	}
	if nonemptyConfig(storage.ImageStore) && nonemptyConfig(storage.GraphRoot) && storage.ImageStore.Value == storage.GraphRoot.Value {
		storage.Problems = append(storage.Problems, "imagestore_equals_graphroot")
	}
	return nil
}
