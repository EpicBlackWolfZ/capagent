package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const activeInfoJSON = `{"version":{"Version":"5.8.4"},"host":{"networkBackend":"netavark",` +
	`"cgroupVersion":"v2","cgroupManager":"systemd","security":{"rootless":false},` +
	`"networkBackendInfo":{"path":"/usr/libexec/podman/netavark"}},` +
	`"store":{"graphDriverName":"overlay","graphRoot":"/data","runRoot":"/run/storage"}}`

func activeServices(t *testing.T) (currentServices, *platform.FakeCommandRunner) {
	t.Helper()
	mem := platform.NewMemPlatformReader()
	for _, dir := range []string{"/usr", "/usr/bin", "/home", "/home/test", "/usr/libexec", "/usr/libexec/podman"} {
		mem.AddDir(dir, 0o755)
	}
	mem.AddFile("/usr/bin/podman", nil, 0o755)
	mem.AddFile("/usr/libexec/podman/netavark", nil, 0o755)
	files := platform.NewScopedMemReader("/", mem)
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Error(err)
		}
	})
	policy, err := platform.NewEnvPolicy(nil, map[string]string{"HOME": "/home/test"})
	if err != nil {
		t.Fatal(err)
	}
	runner := platform.NewFakeCommandRunner()
	commands, err := podman.PrepareInspection(t.Context(), files, model.UserIdentity{}, "/usr/bin/podman", policy)
	if err != nil {
		t.Fatal(err)
	}
	runner.Register(commands.Version, platform.ExecResult{Stdout: []byte("podman version 5.8.4\n")})
	runner.Register(commands.Info, platform.ExecResult{Stdout: []byte(activeInfoJSON)})
	return currentServices{files: files, credentials: model.CurrentCredentials{GroupsKnown: true},
		now:     func() time.Time { return time.Unix(1, 0) },
		capture: func() (platform.EnvPolicy, error) { return policy, nil },
		runner:  func() platform.CommandRunner { return runner }}, runner
}

func TestActiveInspectionPipeline(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"success", "info failure", "version failure", "missing field", "version conflict"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			services, runner := activeServices(t)
			policy, err := services.capture()
			if err != nil {
				t.Fatal(err)
			}
			commands, err := podman.PrepareInspection(t.Context(), services.files, model.UserIdentity{}, "/usr/bin/podman", policy)
			if err != nil {
				t.Fatal(err)
			}
			want, count := "SATISFIED", 2
			switch scenario {
			case "info failure":
				runner.Register(commands.Info, platform.ExecResult{ExitCode: 125})
				want = testIndeterminate
			case "version failure":
				runner.Register(commands.Version, platform.ExecResult{ExitCode: 125})
				want, count = testIndeterminate, 1
			case "missing field":
				raw := strings.ReplaceAll(activeInfoJSON, `"graphRoot":"/data",`, "")
				runner.Register(commands.Info, platform.ExecResult{Stdout: []byte(raw)})
				want = testIndeterminate
			case "version conflict":
				runner.Register(commands.Info, platform.ExecResult{Stdout: []byte(strings.ReplaceAll(activeInfoJSON, testInspectionVersion, "4.9.4"))})
				want = testIndeterminate
			}
			report, err := evaluateCurrentServices(t.Context(), Options{Runtime: "podman", Active: true}, services)
			if err != nil {
				t.Fatal(err)
			}
			if report.Evaluation.Requirement.State != want || len(runner.Calls()) != count || report.Evaluation.Collection != "active" {
				t.Fatal("wrong inspection result or dispatch", report, len(runner.Calls()))
			}
			if scenario != "version failure" && report.Runtimes["podman"].Version != testInspectionVersion {
				t.Fatal("info overwrote selected CLI version")
			}
			if scenario == "info failure" && report.Capabilities["runtime.podman.info"].State != "unavailable" {
				t.Fatal("completed info failure did not retain unavailability")
			}
			if scenario == "success" {
				runtime := report.Runtimes["podman"]
				if runtime.Rootless == nil || *runtime.Rootless || runtime.CgroupVersion == nil || *runtime.CgroupVersion != "v2" ||
					runtime.GraphRoot == nil || *runtime.GraphRoot != "/data" || report.Capabilities["runtime.podman.netavark"].State != "supported" {
					t.Fatal("lost effective facts or helper evidence", runtime)
				}
			}
		})
	}
}

func TestPassiveAndInvalidContextNeverConstructRunner(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"passive", "credentials", "groups", "environment", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			services, _ := activeServices(t)
			services.runner = func() platform.CommandRunner { t.Fatal("constructed unauthorized runner"); return nil }
			opts := Options{Runtime: "podman", Active: scenario != "passive"}
			ctx := t.Context()
			switch scenario {
			case "passive":
				services.capture = func() (platform.EnvPolicy, error) {
					t.Fatal("passive environment capture")
					return platform.EnvPolicy{}, nil
				}
			case "credentials":
				services.credentials.UID = 1000
			case "groups":
				services.credentials.GroupsKnown = false
			case "environment":
				services.capture = func() (platform.EnvPolicy, error) { return platform.EnvPolicy{}, errors.New("private error") }
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := evaluateCurrentServices(ctx, opts, services); err != nil {
				t.Fatal(err)
			}
		})
	}
}

const testInspectionVersion = "5.8.4"
