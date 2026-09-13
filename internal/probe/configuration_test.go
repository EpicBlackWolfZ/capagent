package probe_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

const snapshotRuntimeName = "crun"
const changedConfigValue = "changed"

func TestConfigurationSnapshotOwnership(t *testing.T) {
	t.Parallel()
	flag := true
	list := model.ConfigList{Values: []string{"/original"}, Origins: []string{"source"}, Append: &flag}
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
	if engine.Runtime.Value != snapshotRuntimeName || engine.CgroupManager.Value != "systemd" || engine.ConmonPath.Values[0] != "/original" ||
		engine.ConmonPath.Origins[0] != "source" || engine.Runtimes[snapshotRuntimeName].Values[0] != "/original" || !flag ||
		engine.Environment.Count != 1 || obs.Configuration.References[0] != "reference" || obs.Configuration.Sources[0].Path != "/source" {
		t.Fatal("configuration snapshot retained mutable aliases")
	}
}
