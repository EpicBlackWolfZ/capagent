package podman_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestVersionObservation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"success", "malformed", "nonzero", "start", "timeout", "cancelled", "truncated", "missing service"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			spec := platform.CommandSpec{Path: "/usr/bin/podman", Args: []string{"--version"}, Dir: "/", Timeout: time.Second}
			result := platform.ExecResult{Stdout: []byte("podman version 5.8.4\n")}
			var runErr error
			switch scenario {
			case "malformed":
				result.Stdout = []byte("secret invalid text")
			case "nonzero":
				result.ExitCode = 1
			case "start":
				runErr = errors.New("secret error")
			case "timeout":
				runErr = context.DeadlineExceeded
				result.TimedOut = true
			case "cancelled":
				runErr = context.Canceled
			case "truncated":
				result.StdoutTruncated = true
			}
			runner := platform.NewFakeCommandRunner()
			if err := runner.RegisterWithError(spec, result, runErr); err != nil {
				t.Fatal(err)
			}
			env := platform.NewEnvironment(nil, nil, nil, runner).WithScope(versionScope())
			if scenario == "missing service" {
				env = platform.NewEnvironment(nil, nil, nil, nil).WithScope(versionScope())
			}
			p := podman.VersionProbe{Command: spec, Now: func() time.Time { return time.Unix(1, 0) }}
			obs, err := p.Run(t.Context(), env)
			if (err == nil) != (scenario == "success") {
				t.Fatalf("error %v", err)
			}
			if len(obs.Facts) == 0 || obs.Version == nil {
				t.Fatal("lost partial observation")
			}
			if scenario == "success" {
				if obs.Version.Version == nil || obs.Version.Version.Canonical != "5.8.4" || obs.Version.Runnable == nil || !*obs.Version.Runnable {
					t.Fatal("lost parsed version")
				}
			} else if obs.Version.Version != nil || len(obs.Diagnostics) == 0 {
				t.Fatal("failed command asserted a version")
			}
		})
	}
}
