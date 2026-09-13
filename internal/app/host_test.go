package app

import (
	"bytes"
	json "encoding/json/v2"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
	"io/fs"
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
	report.Context.Completeness = string(model.Complete)
	if hostExit(report) != ExitSatisfied {
		t.Fatal("complete host collection failed")
	}
	if report.Host.OS != "debian" {
		t.Fatal("lost collected OS")
	}
}

func TestHostCollectionRejectsRequirementsAndScopeLeakage(t *testing.T) {
	t.Parallel()
	scope := model.EvaluationScope{RunID: "host-test", ContextID: "current"}
	input := Input{Scope: scope, Context: model.EvaluationContext{ID: "current"}, At: time.Unix(1, 0), Mode: "live", Provenance: "live"}
	env := platform.NewEnvironment(nil, nil, nil, nil).WithScope(scope)
	input.Requirement = &requirement.Node{Capability: "runtime.podman"}
	if _, err := Evaluate(t.Context(), input, env, nil); err == nil {
		t.Fatal("host collector accepted deployment evaluation")
	}
	input.Requirement = nil
	report, err := Evaluate(t.Context(), input, env, nil)
	if err != nil {
		t.Fatal(err)
	}
	report.Runtimes["podman"] = output.RuntimeInfo{}
	if report.Validate() == nil {
		t.Fatal("host scope accepted runtime data")
	}
	report.Runtimes = map[string]output.RuntimeInfo{}
	report.Evaluation.Requirement = output.RequirementResult{State: "SATISFIED"}
	if report.Validate() == nil {
		t.Fatal("host scope accepted requirement verdict")
	}
	if executionMessage(platform.ErrSymlinkUnsupported) == executionMessage(fs.ErrPermission) {
		t.Fatal("confinement failures collapsed")
	}
}
