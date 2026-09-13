package podman_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const completeInfo = `{"version":{"Version":"5.8.4"},"host":{"networkBackend":"netavark",` +
	`"networkBackendInfo":{"path":"/usr/libexec/podman/netavark"},"cgroupVersion":"v2",` +
	`"cgroupManager":"systemd","security":{"rootless":false}},` +
	`"store":{"graphDriverName":"overlay","graphRoot":"/var/lib/containers/storage","runRoot":"/run/containers/storage"}}`

func TestInfoCompleteFieldsAndPresence(t *testing.T) {
	t.Parallel()
	payload, err := podman.ParseInfo([]byte(completeInfo))
	if err != nil || payload.GraphRoot == nil || *payload.GraphRoot != "/var/lib/containers/storage" ||
		payload.RunRoot == nil || *payload.RunRoot != "/run/containers/storage" || payload.Rootless == nil || *payload.Rootless {
		t.Fatal("lost effective fields or explicit false", payload, err)
	}
	for _, raw := range []string{`{"store":{"graphRoot":null,"runRoot":""}}`, `{"host":{"networkBackend":"future_backend"}}`} {
		if _, err := podman.ParseInfo([]byte(raw)); err != nil {
			t.Fatal("rejected unknown or absent field", err)
		}
	}
	for _, raw := range []string{`{"store":{"graphRoot":4}}`, `{"host":{"security":{"rootless":"false"}}}`, `[]`} {
		if _, err := podman.ParseInfo([]byte(raw)); err == nil {
			t.Fatal("accepted wrong field type", raw)
		}
	}
}

func TestHelperSourceCancellationAndUnreadableMetadata(t *testing.T) {
	t.Parallel()
	available := true
	input := model.Observation{ID: "podman.info", Podman: &model.PodmanInfo{
		Path: testPodmanPath, Available: &available, HelperPath: "/helper"}}
	mem := platform.NewMemPlatformReader()
	mem.AddError("/helper", fs.ErrPermission)
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files)
	for _, source := range []model.Observation{{}, {Scope: model.EvaluationScope{ContextID: "other"}}, input} {
		obs, err := podman.ObserveHelper(t.Context(), env, source, time.Unix(1, 0))
		if err == nil || obs.Completeness != model.Partial {
			t.Fatal("invalid or unreadable helper became complete")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := podman.ObserveHelper(ctx, env, input, time.Unix(1, 0)); !errors.Is(err, context.Canceled) {
		t.Fatal("lost helper cancellation", err)
	}
}

func TestInfoRejectsRemoteAndUnsafeStorageOrVersion(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		strings.ReplaceAll(completeInfo, `"cgroupVersion":"v2"`, `"serviceIsRemote":true,"cgroupVersion":"v2"`),
		strings.ReplaceAll(completeInfo, "/var/lib/containers/storage", "relative"),
		strings.ReplaceAll(completeInfo, testVersion, "not a version"),
	} {
		runner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) {
			return platform.ExecResult{Stdout: []byte(raw)}, nil
		})
		obs, err := (podman.InfoProbe{Command: inspectionSpec()}).Run(t.Context(), platform.NewEnvironment(nil, nil, nil, runner))
		if err == nil || obs.Completeness != model.Partial || obs.Podman.Available != nil {
			t.Fatal("unsafe or remote payload asserted successful local inspection", obs, err)
		}
	}
}

func TestInfoCollectionIsIndependentOfHelper(t *testing.T) {
	t.Parallel()
	now := time.Unix(10, 0)
	runner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) {
		now = now.Add(time.Second)
		return platform.ExecResult{Stdout: []byte(completeInfo)}, nil
	})
	p := podman.InfoProbe{Command: inspectionSpec(), Now: func() time.Time { return now }, AfterVersion: true}
	if !reflect.DeepEqual(p.Dependencies(), []string{"podman.version"}) {
		t.Fatal("missing version dependency")
	}
	obs, err := p.Run(t.Context(), platform.NewEnvironment(nil, nil, nil, runner))
	if err != nil || obs.Completeness != model.Complete || obs.Podman == nil || obs.Podman.Available == nil ||
		!*obs.Podman.Available || obs.PodmanHelper != nil || !obs.Timestamp.Equal(now) || obs.Podman.Path != p.Command.Path {
		t.Fatal("collection needs helper service or lost selection/time", obs, err)
	}
	if !obs.Facts[0].Timestamp.Equal(now) {
		t.Fatal("fact precedes collection")
	}
}

func TestInfoFailureCannotAssertAccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, raw string
		result    platform.ExecResult
		err       error
		absent    bool
	}{
		{name: "empty", raw: `{}`},
		{name: "null", raw: `null`},
		{name: "truncated stdout", raw: completeInfo, result: platform.ExecResult{StdoutTruncated: true}},
		{name: "truncated stderr", raw: completeInfo, result: platform.ExecResult{StderrTruncated: true}},
		{name: "timeout", raw: completeInfo, result: platform.ExecResult{TimedOut: true}, err: context.DeadlineExceeded},
		{name: "cancel", raw: completeInfo, err: context.Canceled},
		{name: "nonzero", raw: completeInfo, result: platform.ExecResult{ExitCode: 125}, absent: true},
		{name: "start", err: errors.New("PRIVATE raw command error"), absent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) {
				result := tt.result
				result.Stdout = []byte(tt.raw)
				return result, tt.err
			})
			p := podman.InfoProbe{Command: inspectionSpec(), Timestamp: time.Unix(1, 0)}
			obs, err := p.Run(t.Context(), platform.NewEnvironment(nil, nil, nil, runner))
			if err == nil || (tt.err != nil && !errors.Is(err, tt.err)) || len(obs.Diagnostics) == 0 {
				t.Fatal("lost failure", obs, err)
			}
			if obs.Podman != nil && obs.Podman.Available != nil && *obs.Podman.Available {
				t.Fatal("failure asserted engine access")
			}
			if tt.absent && (obs.Podman == nil || obs.Podman.Available == nil || *obs.Podman.Available) {
				t.Fatal("lost completed unavailability")
			}
			for _, d := range obs.Diagnostics {
				if strings.Contains(d.Message, "PRIVATE") {
					t.Fatal("raw failure leaked")
				}
			}
		})
	}
}

func TestInfoRejectsUnreviewedCommands(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--remote=false", "info", "--format", "json"}, {"run", "image"},
		{"--remote=true", "--trace=false", "info", "--format", "json"}} {
		runner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) {
			t.Fatal("unreviewed command dispatched")
			return platform.ExecResult{}, nil
		})
		spec := inspectionSpec()
		spec.Args = args
		if _, err := (podman.InfoProbe{Command: spec}).Run(t.Context(), platform.NewEnvironment(nil, nil, nil, runner)); err == nil {
			t.Fatal("accepted unreviewed command")
		}
	}
}

func TestInfoHelperHasIndependentCompleteness(t *testing.T) {
	t.Parallel()
	payload, err := podman.ParseInfo([]byte(completeInfo))
	if err != nil {
		t.Fatal(err)
	}
	available := true
	payload.Available, payload.Path = &available, testPodmanPath
	input := model.Observation{ID: "podman.info", ProbeID: "podman.info", Podman: &payload, Completeness: model.Complete}
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/usr", 0o755)
	mem.AddDir("/usr/libexec", 0o755)
	mem.AddDir("/usr/libexec/podman", 0o755)
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files)
	obs, err := podman.ObserveHelper(t.Context(), env, input, time.Unix(2, 0))
	if err != nil || obs.PodmanHelper == nil || obs.PodmanHelper.Present == nil || *obs.PodmanHelper.Present ||
		obs.Completeness != model.Complete || obs.PodmanHelper.InfoID != input.ID {
		t.Fatal("lost observed helper absence", obs, err)
	}
	obs, err = podman.ObserveHelper(t.Context(), platform.Environment{}, input, time.Unix(2, 0))
	if err == nil || obs.Completeness != model.Partial || input.Completeness != model.Complete || !*input.Podman.Available {
		t.Fatal("helper failure corrupted info", obs, input, err)
	}
}

func TestInfoUnsafeValuesAreNotProjectedClaims(t *testing.T) {
	t.Parallel()
	raw := strings.ReplaceAll(completeInfo, `"netavark"`, `"secret\nbackend"`)
	runner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) {
		return platform.ExecResult{Stdout: []byte(raw)}, nil
	})
	obs, _ := (podman.InfoProbe{Command: inspectionSpec()}).Run(t.Context(), platform.NewEnvironment(nil, nil, nil, runner))
	if obs.Podman == nil || obs.Podman.NetworkBackend != nil || obs.Completeness != model.Partial || len(obs.Diagnostics) == 0 {
		t.Fatal("unsafe runtime field escaped normalization", obs)
	}
}

func TestUnsafeHelperDoesNotEraseEngineMeasurement(t *testing.T) {
	t.Parallel()
	raw := strings.ReplaceAll(completeInfo, "/usr/libexec/podman/netavark", "relative/helper")
	runner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) {
		return platform.ExecResult{Stdout: []byte(raw)}, nil
	})
	env := platform.NewEnvironment(nil, nil, nil, runner)
	obs, err := (podman.InfoProbe{Command: inspectionSpec()}).Run(t.Context(), env)
	if err != nil || obs.Podman == nil || obs.Podman.Available == nil || !*obs.Podman.Available || obs.Completeness != model.Complete {
		t.Fatal("unsafe helper erased engine info", err)
	}
	helper, err := podman.ObserveHelper(t.Context(), env, obs, obs.Timestamp)
	if err == nil || helper.Completeness != model.Partial {
		t.Fatal("unsafe helper was accepted")
	}
}

func inspectionSpec() platform.CommandSpec {
	return platform.CommandSpec{Path: testPodmanPath, Args: podman.LocalInfoArgs(), Timeout: 30 * time.Second}
}

func TestInfoMissingFieldsRetainOtherFacts(t *testing.T) {
	t.Parallel()
	runner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) {
		return platform.ExecResult{Stdout: []byte(`{"host":{"networkBackend":"cni","security":{"rootless":false}},` +
			`"store":{"graphRoot":"/","runRoot":""}}`)}, nil
	})
	obs, err := (podman.InfoProbe{Command: inspectionSpec()}).Run(t.Context(), platform.NewEnvironment(nil, nil, nil, runner))
	if err != nil || obs.Completeness != model.Complete || obs.Podman.Available == nil || !*obs.Podman.Available ||
		obs.Podman.GraphRoot == nil || *obs.Podman.GraphRoot != "/" || obs.Podman.Rootless == nil || *obs.Podman.Rootless {
		t.Fatal("missing fields erased known information", obs, err)
	}
	if len(obs.Diagnostics) != 5 {
		t.Fatal("missing fixed field diagnostics", obs.Diagnostics)
	}
}

const testPodmanPath = "/usr/bin/podman"
const testRuntimeDir = "/run/user/1000"
const testVersion = "5.8.4"
