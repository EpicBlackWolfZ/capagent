package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestEngineMergeProvenance(t *testing.T) {
	t.Parallel()
	var engine model.EngineConfiguration
	layers := []string{
		`[engine]
runtime = "crun"
cgroup_manager = "systemd"
conmon_path = ["/one"]
helper_binaries_dir = ["/helper", {append=true}]
env = ["SECRET=hidden"]
[engine.runtimes]
crun = ["/old"]
runc = ["/runc"]`,
		`[engine]
runtime = "/custom/runtime"
conmon_path = ["/two", {append=true}]
env = []
[engine.runtimes]
crun = ["/new"]`,
		`[engine]
conmon_path = ["/three"]
helper_binaries_dir = ["/fresh", {append=false}]`,
	}
	for i, text := range layers {
		layer, err := ParseEngine([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		engine = MergeEngine(engine, layer, []string{"vendor", "system", "user"}[i])
	}
	if engine.Runtime.Value != "/custom/runtime" || engine.Runtime.SourceID != "system" ||
		engine.CgroupManager.SourceID != "vendor" || engine.Environment.Count != 0 {
		t.Fatalf("scalar or redacted environment merge: %+v", engine)
	}
	if !reflect.DeepEqual(engine.ConmonPath.Values, []string{"/one", "/two", "/three"}) ||
		!reflect.DeepEqual(engine.ConmonPath.Origins, []string{"vendor", "system", "user"}) ||
		engine.ConmonPath.InheritedDefault || engine.ConmonPath.Append == nil || !*engine.ConmonPath.Append {
		t.Fatalf("sticky append/provenance: %+v", engine.ConmonPath)
	}
	if !reflect.DeepEqual(engine.HelperBinariesDir.Values, []string{"/fresh"}) || engine.HelperBinariesDir.InheritedDefault {
		t.Fatalf("append reset: %+v", engine.HelperBinariesDir)
	}
	if !reflect.DeepEqual(engine.Runtimes["crun"].Values, []string{"/new"}) || engine.Runtimes["runc"].SourceID != "vendor" {
		t.Fatalf("runtime map merge: %+v", engine.Runtimes)
	}
}

func TestEngineFieldSemantics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text, code string
	}{
		{"empty", "", ""},
		{"empty strings", "[engine]\nruntime=''\ncgroup_manager=''", ""},
		{"outside family", "[network]\nnetwork_backend='netavark'", ""},
		{"unknown values", "[engine]\nsecret_key='password'", ""},
		{"runtime type", "[engine]\nruntime=7", ErrConfigFieldInvalid.Error()},
		{"engine type", "engine=[]", ErrConfigFieldInvalid.Error()},
		{"array type", "[engine]\nconmon_path='/bin/conmon'", ErrConfigFieldInvalid.Error()},
		{"array item", "[engine]\nconmon_path=[1]", ErrConfigFieldInvalid.Error()},
		{"attribute type", "[engine]\nconmon_path=[{append='yes'}]", ErrConfigFieldInvalid.Error()},
		{"attribute key", "[engine]\nconmon_path=[{secret=true}]", ErrConfigFieldInvalid.Error()},
		{"empty attribute", "[engine]\nconmon_path=[{}]", ""},
		{"attribute anywhere", "[engine]\nconmon_path=[{append=true},'/a',{append=false},'/b']", ""},
		{"plain runtime arrays", "[engine.runtimes]\ncrun=['/bin/crun',{append=true}]", ErrConfigFieldInvalid.Error()},
		{"runtime map type", "[engine]\nruntimes=['crun']", ErrConfigFieldInvalid.Error()},
		{"unsafe runtime key", "[engine.runtimes]\n'bad key'=['/bin/crun']", ErrConfigFieldUnsupported.Error()},
		{"relative path", "[engine]\nconmon_path=['relative']", ErrConfigFieldUnsupported.Error()},
		{"bindir", "[engine]\nhelper_binaries_dir=['$BINDIR/../libexec']", ""},
		{"other expansion", "[engine]\nhelper_binaries_dir=['$HOME/bin']", ErrConfigFieldUnsupported.Error()},
		{"control", "[engine]\nruntime='''\u0001'''", ErrConfigMalformed.Error()},
		{"unknown manager retained for merge", "[engine]\ncgroup_manager='other'", ""},
		{"array limit", "[engine]\nconmon_path=[" + strings.Repeat("'/x',", MaxConfigList+1) + "]", ErrConfigLimit.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseEngine([]byte(tt.text))
			if tt.code == "" && err != nil || tt.code != "" && (err == nil || err.Error() != tt.code) {
				t.Fatalf("error=%v; want %q", err, tt.code)
			}
		})
	}
}

func TestEngineInvalidValueIsRedactedUntilMerge(t *testing.T) {
	t.Parallel()
	bad, err := ParseEngine([]byte("[engine]\ncgroup_manager='synthetic-secret'"))
	if err != nil {
		t.Fatal(err)
	}
	one := MergeEngine(model.EngineConfiguration{}, bad, "first")
	if one.CgroupManager.Value != "" || !one.CgroupManager.Invalid || one.CgroupManager.SourceID != "first" {
		t.Fatal("invalid value or provenance was lost or exposed")
	}
	good, err := ParseEngine([]byte("[engine]\ncgroup_manager='systemd'"))
	if err != nil {
		t.Fatal(err)
	}
	two := MergeEngine(one, good, "second")
	if two.CgroupManager.Invalid || two.CgroupManager.Value != "systemd" || !one.CgroupManager.Invalid {
		t.Fatal("later value did not replace the invalid marker independently")
	}
}

func TestEngineUnknownDefaultsAndOwnership(t *testing.T) {
	t.Parallel()
	layer, err := ParseEngine([]byte("[engine]\nconmon_path=['/x',{append=true}]\nenv=['TOKEN=secret',{append=true}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	one := MergeEngine(model.EngineConfiguration{}, layer, "one")
	two := MergeEngine(one, layer, "two")
	two.ConmonPath.Values[0] = "/changed"
	if !one.ConmonPath.InheritedDefault || one.ConmonPath.Values[0] != "/x" || two.Environment.Count != 2 ||
		!two.Environment.InheritedDefault {
		t.Fatal("unknown defaults or borrowed ownership lost")
	}
}
