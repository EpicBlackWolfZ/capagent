package podman

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// VersionProbe is wired only to offline fixture services. Even --version can
// mutate rootless runtime directories during Podman startup. Live dispatch
// requires the future explicit active boundary, not an inferred safe version.
type VersionProbe struct {
	Command platform.CommandSpec
	Now     func() time.Time
}

func (VersionProbe) ID() string             { return "podman.version" }
func (VersionProbe) Dependencies() []string { return nil }

func (p VersionProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(p.Now))
	obs.Version = &model.PodmanVersionObservation{}
	if !slices.Equal(p.Command.Args, []string{"--version"}) || ValidateExecutablePath(p.Command.Path) != nil || p.Command.Path == "" {
		return failedRuntimeObservation(obs, "invalid_version_command", errors.New("expected explicit Podman --version command"))
	}
	obs.Version.Path = p.Command.Path
	if env.Runner() == nil {
		return failedRuntimeObservation(obs, "missing_command_service", errors.New("missing command service"))
	}
	result, runErr := env.Runner().Run(ctx, p.Command)
	obs.Timestamp = measurementTime(p.Now)
	obs.Facts[0].Timestamp = obs.Timestamp
	obs.Facts[0].RawData = result.Stdout
	code := versionFailure(result, runErr)
	if code != "" {
		if runErr == nil {
			runErr = errors.New("version command did not complete successfully")
		}
		if code == "version_start_failed" || code == "version_exit_failed" {
			available := false
			obs.Version.Runnable = &available
			obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: code, Message: "Podman version command did not succeed"})
			return obs, runErr
		}
		return failedRuntimeObservation(obs, code, runErr)
	}
	version, err := ParseVersion(result.Stdout)
	if err != nil {
		return failedRuntimeObservation(obs, "invalid_version_output", err)
	}
	runnable := true
	obs.Version.Version, obs.Version.Runnable = &version, &runnable
	return obs, nil
}

func versionFailure(result platform.ExecResult, err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "version_cancelled"
	case result.TimedOut || errors.Is(err, context.DeadlineExceeded):
		return "version_timeout"
	case result.StdoutTruncated || result.StderrTruncated:
		return "version_output_truncated"
	case result.ExitCode != 0:
		return "version_exit_failed"
	case err != nil:
		return "version_start_failed"
	default:
		return ""
	}
}
