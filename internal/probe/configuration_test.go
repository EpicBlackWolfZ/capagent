package probe_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

const originalConfigPath = "/original"
const snapshotRuntimeName = "crun"
const changedConfigValue = "changed"

const snapshotSourceID = "source"

func TestConfigurationSnapshotOwnership(t *testing.T) {
	t.Parallel()
	flag := true
	list := model.ConfigList{Values: []string{originalConfigPath}, Origins: []string{snapshotSourceID}, Append: &flag}
	engine := &model.EngineConfiguration{Runtime: &model.ConfigString{Value: snapshotRuntimeName},
		CgroupManager: &model.ConfigString{Value: "systemd"},
		Runtimes:      map[string]model.ConfigList{snapshotRuntimeName: list}, ConmonPath: &list, HelperBinariesDir: &list, RuntimePath: &list,
		Environment: &model.ConfigRedactedList{Count: 1, Append: &flag}}
	obs := model.Observation{Configuration: &model.ConfigurationObservation{References: []string{"reference"}, Engine: engine,
		Sources: []model.ConfigurationSource{{Path: "/source", Engine: engine}}}}
	copy := probe.SnapshotObservation(obs)
	copy.Configuration.References[0] = changedConfigValue
	copy.Configuration.Sources[0].Path = "/changed"
	for _, c := range []*model.EngineConfiguration{copy.Configuration.Engine, copy.Configuration.Sources[0].Engine} {
		c.Runtime.Value, c.CgroupManager.Value = changedConfigValue, changedConfigValue
		for _, p := range []*model.ConfigList{c.ConmonPath, c.HelperBinariesDir, c.RuntimePath} {
			p.Values[0], p.Origins[0], *p.Append = "/changed", changedConfigValue, false
		}
		c.Runtimes[snapshotRuntimeName].Values[0], *c.Runtimes[snapshotRuntimeName].Append = "/changed", false
		c.Environment.Count, *c.Environment.Append = 9, false
	}
	if engine.Runtime.Value != snapshotRuntimeName || engine.CgroupManager.Value != "systemd" ||
		engine.ConmonPath.Values[0] != originalConfigPath ||
		engine.ConmonPath.Origins[0] != snapshotSourceID || engine.Runtimes[snapshotRuntimeName].Values[0] != originalConfigPath || !flag ||
		engine.Environment.Count != 1 || obs.Configuration.References[0] != "reference" || obs.Configuration.Sources[0].Path != "/source" {
		t.Fatal("configuration snapshot retained mutable aliases")
	}
}

func TestStorageConfigurationSnapshotOwnership(t *testing.T) {
	t.Parallel()
	value := &model.ConfigString{Value: "original", SourceID: snapshotSourceID}
	list := &model.ConfigList{Values: []string{originalConfigPath}, Origins: []string{snapshotSourceID}}
	storage := &model.StorageConfiguration{Driver: value, GraphRoot: value, RunRoot: value, RootlessStoragePath: value, ImageStore: value,
		MountProgram: value, DriverPriority: list, AdditionalImageStores: list, AdditionalLayerStores: list,
		TransientStore: &model.ConfigBool{Value: true}, Options: map[string]model.ConfigString{"mount_program": *value}}
	obs := model.Observation{Configuration: &model.ConfigurationObservation{Storage: storage,
		Sources: []model.ConfigurationSource{{Storage: storage}}}}
	snapshot := probe.SnapshotObservation(obs)
	for _, copy := range []*model.StorageConfiguration{snapshot.Configuration.Storage, snapshot.Configuration.Sources[0].Storage} {
		for _, field := range []*model.ConfigString{copy.Driver, copy.GraphRoot, copy.RunRoot, copy.RootlessStoragePath,
			copy.ImageStore, copy.MountProgram} {
			field.Value = changedConfigValue
		}
		for _, field := range []*model.ConfigList{copy.DriverPriority, copy.AdditionalImageStores, copy.AdditionalLayerStores} {
			field.Values[0], field.Origins[0] = changedConfigValue, changedConfigValue
		}
		copy.TransientStore.Value = false
		copy.Options["mount_program"] = model.ConfigString{Value: changedConfigValue}
	}
	if value.Value != "original" || list.Values[0] != originalConfigPath || list.Origins[0] != snapshotSourceID ||
		!storage.TransientStore.Value ||
		storage.Options["mount_program"].Value != "original" {
		t.Fatal("storage snapshot retained borrowed mutable state")
	}
}

func TestStoragePathSnapshotOwnership(t *testing.T) {
	t.Parallel()
	flag := true
	number := uint32(1001)
	metadata := model.StoragePathMetadata{Present: &flag, Directory: &flag, CheckedUID: &number, CheckedGID: &number, CheckedMode: &number,
		Accessible: &flag, Filesystem: &model.StorageFilesystem{Type: 1, ReadOnly: &flag, NoSUID: &flag, NoExec: &flag}}
	obs := model.Observation{StoragePaths: &model.StoragePathObservation{Paths: []model.StoragePathMetadata{metadata}}}
	snapshot := probe.SnapshotObservation(obs)
	copied := &snapshot.StoragePaths.Paths[0]
	for _, value := range []*bool{copied.Present,
		copied.Directory,
		copied.Accessible,
		copied.Filesystem.ReadOnly,
		copied.Filesystem.NoSUID,
		copied.Filesystem.NoExec} {
		*value = false
	}
	for _, value := range []*uint32{copied.CheckedUID, copied.CheckedGID, copied.CheckedMode} {
		*value = 0
	}
	copied.Filesystem.Type = 2
	if !flag || number != 1001 || metadata.Filesystem.Type != 1 {
		t.Fatal("storage path snapshot retained shared pointers")
	}
}
