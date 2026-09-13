package podman

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// LocalInfoArgs uses a local-only flag at its default value. Remote clients and
// configuration-selected tunnel mode reject --trace before transport setup.
// Keep this literal guard; never retry without it on an unknown-flag failure.
func LocalInfoArgs() []string {
	return []string{"--remote=false", "--trace=false", "info", "--format", "json"}
}

// InfoProbe collects command facts only. Helper metadata is observed separately
// after collection, so an unreadable helper does not erase engine information.
// LegacyInfo permits the original exact fixture invocation, only in offline wiring.
type InfoProbe struct {
	Command      platform.CommandSpec
	Timestamp    time.Time
	Now          func() time.Time
	AfterVersion bool
	LegacyInfo   bool
}

func (InfoProbe) ID() string { return "podman.info" }
func (p InfoProbe) Dependencies() []string {
	if p.AfterVersion {
		return []string{"podman.version"}
	}
	return nil
}

func (p InfoProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), p.at())
	obs.Summary = "Podman effective runtime information"
	obs.Podman = &model.PodmanInfo{Path: p.Command.Path}
	args := LocalInfoArgs()
	if p.LegacyInfo {
		args = []string{"--remote=false", "info", "--format", "json"}
	}
	if p.Command.Path == "" || ValidateExecutablePath(p.Command.Path) != nil || !slices.Equal(p.Command.Args, args) {
		return failedRuntimeObservation(obs, "invalid_info_command", errors.New("expected reviewed Podman info command"))
	}
	if env.Runner() == nil {
		return failedRuntimeObservation(obs, "missing_command_service", errors.New("missing Podman command service"))
	}
	result, runErr := env.Runner().Run(ctx, p.Command)
	obs.Timestamp = p.at()
	obs.Facts[0].Timestamp, obs.Facts[0].RawData = obs.Timestamp, result.Stdout
	if code := versionFailure(result, runErr); code != "" {
		code = strings.Replace(code, "version_", "info_", 1)
		if runErr == nil {
			runErr = errors.New("runtime inspection did not complete successfully")
		}
		if code == "info_start_failed" || code == "info_exit_failed" {
			available := false
			obs.Podman.Available = &available
			obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: code, Message: "runtime inspection did not succeed"})
			return obs, runErr
		}
		return failedRuntimeObservation(obs, code, runErr)
	}
	payload, err := ParseInfo(result.Stdout)
	if err != nil || !recognizableInfo(payload) {
		return failedRuntimeObservation(obs, "invalid_runtime_json", errors.New("unrecognized Podman info document"))
	}
	payload.Path = p.Command.Path
	if !normalizeInfo(&payload) {
		obs.Podman = &payload
		return failedRuntimeObservation(obs, "invalid_runtime_value", errors.New("runtime information contains unsafe values"))
	}
	available := true
	payload.Available = &available
	obs.Podman = &payload
	if payload.ServiceIsRemote != nil && *payload.ServiceIsRemote {
		payload.Available = nil
		obs.Podman = &payload
		return failedRuntimeObservation(obs, "unexpected_remote_info", errors.New("local inspection returned remote information"))
	}
	obs.Diagnostics = missingInfoDiagnostics(payload)
	return obs, nil
}

func missingInfoDiagnostics(p model.PodmanInfo) []model.Diagnostic {
	var diagnostics []model.Diagnostic
	fields := []struct {
		name    string
		missing bool
	}{{"version", p.VersionParts == nil}, {"rootless", p.Rootless == nil}}
	for _, field := range []struct {
		name  string
		value *string
	}{{"network_backend", p.NetworkBackend}, {"storage_driver", p.StorageDriver}, {"cgroup_version", p.CgroupVersion},
		{"cgroup_manager", p.CgroupManager}, {"graph_root", p.GraphRoot}, {"run_root", p.RunRoot}} {
		fields = append(fields, struct {
			name    string
			missing bool
		}{field.name, field.value == nil || *field.value == ""})
	}
	for _, field := range fields {
		if field.missing {
			diagnostics = append(diagnostics, model.Diagnostic{Code: "info_missing_" + field.name,
				Message: "requested runtime field is absent or empty"})
		}
	}
	return diagnostics
}

func (p InfoProbe) at() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	if !p.Timestamp.IsZero() {
		return p.Timestamp
	}
	return time.Now()
}

func recognizableInfo(p model.PodmanInfo) bool {
	return p.Version != "" || p.NetworkBackend != nil || p.StorageDriver != nil || p.CgroupVersion != nil ||
		p.CgroupManager != nil || p.Rootless != nil || p.GraphRoot != nil || p.RunRoot != nil
}

func incompleteObservation(obs model.Observation, code string) model.Observation {
	obs.Completeness = model.Partial
	obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: code, Message: "runtime observation is incomplete"})
	return obs
}
