package probe

import (
	"maps"
	"slices"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func snapshotConfiguration(input *model.ConfigurationObservation) *model.ConfigurationObservation {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	out.References = slices.Clone(out.References)
	out.Engine = snapshotEngine(out.Engine)
	out.Storage = snapshotStorage(out.Storage)
	out.Sources = slices.Clone(out.Sources)
	for i := range out.Sources {
		out.Sources[i].Engine = snapshotEngine(out.Sources[i].Engine)
		out.Sources[i].Storage = snapshotStorage(out.Sources[i].Storage)
	}
	return out
}

func snapshotEngine(input *model.EngineConfiguration) *model.EngineConfiguration {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	out.Runtime, out.CgroupManager = copyValue(out.Runtime), copyValue(out.CgroupManager)
	out.ConmonPath, out.HelperBinariesDir = snapshotConfigList(out.ConmonPath), snapshotConfigList(out.HelperBinariesDir)
	out.RuntimePath = snapshotConfigList(out.RuntimePath)
	if out.Runtimes != nil {
		out.Runtimes = map[string]model.ConfigList{}
		for name, list := range input.Runtimes {
			out.Runtimes[name] = *snapshotConfigList(&list)
		}
	}
	out.Environment = copyValue(out.Environment)
	if out.Environment != nil {
		out.Environment.Append = copyValue(out.Environment.Append)
	}
	return out
}

func snapshotConfigList(input *model.ConfigList) *model.ConfigList {
	out := copyValue(input)
	if out != nil {
		out.Values, out.Origins, out.Append = slices.Clone(out.Values), slices.Clone(out.Origins), copyValue(out.Append)
	}
	return out
}

func snapshotStorage(input *model.StorageConfiguration) *model.StorageConfiguration {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	out.Driver, out.RunRoot, out.GraphRoot = copyValue(out.Driver), copyValue(out.RunRoot), copyValue(out.GraphRoot)
	out.RootlessStoragePath, out.ImageStore, out.MountProgram = copyValue(out.RootlessStoragePath),
		copyValue(out.ImageStore), copyValue(out.MountProgram)
	out.DriverPriority = snapshotConfigList(out.DriverPriority)
	out.AdditionalImageStores, out.AdditionalLayerStores = snapshotConfigList(out.AdditionalImageStores),
		snapshotConfigList(out.AdditionalLayerStores)
	out.TransientStore = copyValue(out.TransientStore)
	out.Options = maps.Clone(out.Options)
	out.Problems = slices.Clone(out.Problems)
	return out
}
