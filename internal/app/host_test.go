package app

import (
	"bytes"
	json "encoding/json/v2"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestHostReportHasNoDeploymentVerdict(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/etc", 0o755)
	mem.AddFile("/etc/os-release", []byte("ID=debian\nVERSION_ID=12\n"), 0o644)
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	scope := model.EvaluationScope{RunID: "host-run", ContextID: "current"}
	now := func() time.Time { return time.Unix(1, 0) }
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(scope)
	report, err := Evaluate(t.Context(), Input{Scope: scope, Context: model.EvaluationContext{ID: "current"}, At: now(),
		Mode: "live", Provenance: "live", Collection: "passive"}, env, host.Probes(now))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := writeReport(report, Options{}, &stdout, &stderr)
	if code != ExitIndeterminate {
		t.Fatalf("partial collection exit %d: %s", code, stdout.String())
	}
	var wire map[string]any
	if err = json.Unmarshal(stdout.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	trace := wire["evaluation"].(map[string]any)
	if _, ok := trace["requirement"]; ok {
		t.Fatal("invented requirement")
	}
	if len(wire["runtimes"].(map[string]any)) != 0 || len(wire["capabilities"].(map[string]any)) != 0 {
		t.Fatal("invented runtime or capability")
	}
	report.Host.Completeness = string(model.Complete)
	if hostExit(report) != ExitSatisfied {
		t.Fatal("complete host collection failed")
	}
	if report.Host.OS != "debian" {
		t.Fatal("lost collected OS")
	}
}
