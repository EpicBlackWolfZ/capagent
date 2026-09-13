package output_test

import (
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func TestRuntimeFieldPresenceAndOwnership(t *testing.T) {
	t.Parallel()
	rootless, graph, run, group, manager := false, "", "/run/storage", "v2", "systemd"
	command := "pasta"
	r := output.NewReportFromModel(model.EvaluationContext{}, map[string]output.RuntimeInfo{"podman": {
		RootlessNetworkCmd: &command, Rootless: &rootless, GraphRoot: &graph, RunRoot: &run,
		CgroupVersion: &group, CgroupManager: &manager}}, nil)
	rootless, graph, run, group, manager = true, "changed", "changed", "changed", "changed"
	command = "replaced"
	p := r.Runtimes["podman"]
	if p.RootlessNetworkCmd == nil || *p.RootlessNetworkCmd != "pasta" {
		t.Fatal("rootless network command lost pointer ownership")
	}
	if *p.Rootless || *p.GraphRoot != "" || *p.RunRoot != "/run/storage" || *p.CgroupVersion != "v2" || *p.CgroupManager != "systemd" {
		t.Fatal("runtime projection retained caller pointers")
	}
	raw, err := output.MarshalCompact(r)
	if err != nil || !strings.Contains(string(raw), `"rootless":false`) || !strings.Contains(string(raw), `"graph_root":""`) {
		t.Fatal("false or empty became absent", string(raw), err)
	}
}

func TestRuntimeMetadataValidation(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"relative", "/path/../elsewhere", "/bad\npath", strings.Repeat("/", 4097)} {
		r := output.NewReport()
		r.Host.OS, r.Host.CgroupVersion = "unknown", "unknown"
		r.Runtimes["podman"] = output.RuntimeInfo{GraphRoot: &value}
		if r.Validate() == nil {
			t.Fatal("invalid runtime metadata accepted")
		}
	}
}
