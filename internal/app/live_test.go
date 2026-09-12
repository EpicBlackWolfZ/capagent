package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const testPodmanRuntime = "podman"
const testLiveMode = "live"

func TestLiveCredentialMismatchStaysUnobserved(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	report, err := evaluateCurrent(t.Context(), Options{Runtime: testPodmanRuntime}, files,
		model.CurrentCredentials{UID: 1000, EUID: 0}, nil, func() time.Time { return time.Unix(1, 0) })
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Evaluation.Observations) != 1 || report.Runtimes[testPodmanRuntime].Installed != nil {
		t.Fatal("collected runtime under unsupported credential context")
	}
}

func TestLiveDiscoveryPipeline(t *testing.T) {
	t.Parallel()
	for _, installed := range []bool{true, false} {
		t.Run(map[bool]string{true: "present", false: "absent"}[installed], func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			for _, dir := range []string{"/usr", "/usr/bin", "/usr/local", "/usr/local/bin", "/bin"} {
				mem.AddDir(dir, 0o755)
			}
			if installed {
				mem.AddFile("/usr/bin/podman", nil, 0o755)
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			now := func() time.Time { return time.Unix(1, 0) }
			report, err := evaluateCurrent(t.Context(), Options{Runtime: testPodmanRuntime}, files,
				model.CurrentCredentials{GroupsKnown: true}, nil, now)
			if err != nil {
				t.Fatal(err)
			}
			if report.Evaluation.Mode != testLiveMode || report.Evaluation.Provenance != testLiveMode ||
				report.Context.UID == nil || *report.Context.UID != 0 {
				t.Fatal("lost live context")
			}
			runtime := report.Runtimes["podman"]
			if runtime.Installed == nil || *runtime.Installed != installed || runtime.Accessible != nil ||
				runtime.CLIRunnable != nil || runtime.Version != "" {
				t.Fatal("overstated discovery")
			}
			want := "UNSATISFIED"
			if installed {
				want = "INDETERMINATE"
			}
			if report.Evaluation.Requirement.State != want {
				t.Fatal("wrong decision", report.Evaluation.Requirement)
			}
			if len(report.Evaluation.Observations) != 2 {
				t.Fatal("missing identity or discovery provenance")
			}
		})
	}
}

func TestLiveUsageRejectsUnsupportedSelection(t *testing.T) {
	t.Parallel()
	for _, opts := range []Options{{Runtime: "docker"}, {Runtime: testPodmanRuntime, Context: "uid:1"},
		{Runtime: testPodmanRuntime, Fixture: "ignored"},
		{Runtime: testPodmanRuntime, PodmanPath: "relative"}, {Runtime: testPodmanRuntime, Active: true}, {PodmanPath: "/usr/bin/podman"}} {
		var out, err bytes.Buffer
		if code := Execute(t.Context(), opts, &out, &err); code != ExitUsage || out.Len() != 0 || err.Len() == 0 {
			t.Fatal("invalid live selection", code)
		}
	}
}
