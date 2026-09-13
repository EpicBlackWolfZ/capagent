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
	out.Network = snapshotNetwork(out.Network)
	out.Sources = slices.Clone(out.Sources)
	for i := range out.Sources {
		out.Sources[i].Engine = snapshotEngine(out.Sources[i].Engine)
		out.Sources[i].Storage = snapshotStorage(out.Sources[i].Storage)
		out.Sources[i].Network = snapshotNetwork(out.Sources[i].Network)
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
		out.InvalidIndices, out.UnmodeledIndices = slices.Clone(out.InvalidIndices), slices.Clone(out.UnmodeledIndices)
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

func snapshotNetwork(input *model.NetworkConfiguration) *model.NetworkConfiguration {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	out.Strings = maps.Clone(input.Strings)
	out.Lists = snapshotConfigLists(input.Lists)
	out.HelperPaths = snapshotConfigLists(input.HelperPaths)
	out.DNSBindPort = copyValue(input.DNSBindPort)
	out.PastaOptions = copyValue(input.PastaOptions)
	if out.PastaOptions != nil {
		out.PastaOptions.Append = copyValue(input.PastaOptions.Append)
	}
	return out
}

func snapshotConfigLists(input map[string]model.ConfigList) map[string]model.ConfigList {
	if input == nil {
		return nil
	}
	out := map[string]model.ConfigList{}
	for key, list := range input {
		out[key] = *snapshotConfigList(&list)
	}
	return out
}
