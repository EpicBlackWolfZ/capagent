package podman_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

const infoJSON = `{"version":{"Version":"5.8.1"},"host":{"networkBackend":"netavark","security":{"rootless":true},` +
	`"networkBackendInfo":{"path":"/usr/libexec/podman/netavark"}},"store":{"graphDriverName":"overlay"}}`

func TestInfoParser(t *testing.T) {
	t.Parallel()
	p, err := podman.ParseInfo([]byte(infoJSON))
	if err != nil {
		t.Fatal(err)
	}
	if p.NetworkBackend == nil || *p.NetworkBackend != "netavark" || p.Rootless == nil || !*p.Rootless || *p.StorageDriver != "overlay" {
		t.Fatalf("payload: %+v", p)
	}
	p, err = podman.ParseInfo([]byte(`{"host":{"security":{"rootless":false}}}`))
	if err != nil || p.Rootless == nil || *p.Rootless || p.NetworkBackend != nil {
		t.Fatalf("presence: %+v %v", p, err)
	}
	for _, raw := range []string{`null`, `{`, `{"host":false}`, `{"host":{},"host":{}}`} {
		if _, err := podman.ParseInfo([]byte(raw)); err == nil {
			t.Fatal("accepted malformed info", raw)
		}
	}
}

func TestInfoProbePreservesFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		result  platform.ExecResult
		err     error
		partial bool
	}{
		{"success", platform.ExecResult{Stdout: []byte(infoJSON)}, nil, false},
		{"exit", platform.ExecResult{ExitCode: 125}, errors.New("SECRET error"), false},
		{"timeout", platform.ExecResult{Stdout: []byte(infoJSON), TimedOut: true}, context.DeadlineExceeded, true},
		{"truncated", platform.ExecResult{Stdout: []byte(infoJSON), StdoutTruncated: true}, nil, true},
		{"missing field", platform.ExecResult{Stdout: []byte(`{}`)}, nil, true},
		{"invalid JSON", platform.ExecResult{Stdout: []byte(`{`)}, nil, true},
		{"exit without error", platform.ExecResult{ExitCode: 125}, nil, false},
		{"other backend", platform.ExecResult{Stdout: []byte(`{"host":{"networkBackend":"cni"}}`)}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			for _, dir := range []string{"/usr", "/usr/libexec", "/usr/libexec/podman"} {
				mem.AddDir(dir, 0o755)
			}
			mem.AddFile("/usr/libexec/podman/netavark", nil, 0o755)
			reader := platform.NewScopedMemReader("/", mem)
			defer reader.Close()
			spec := platform.CommandSpec{Path: "/usr/bin/podman", Args: []string{"info", "--format", "json"}}
			runner := platform.NewFakeCommandRunner()
			runner.RegisterWithError(spec, tt.result, tt.err)
			scope := model.EvaluationScope{RunID: "run", ContextID: "user", Runtime: "podman", Endpoint: "local"}
			env := platform.NewEnvironment(nil, nil, nil, runner).WithFiles(reader).WithScope(scope)
			probe := podman.InfoProbe{Command: spec, Timestamp: time.Unix(1, 0)}
			if probe.Dependencies() != nil {
				t.Fatal("unexpected probe dependency")
			}
			obs, err := probe.Run(t.Context(), env)
			if len(obs.Facts) == 0 || obs.Scope != env.Scope() || (obs.Completeness == model.Partial) != tt.partial {
				t.Fatalf("observation: %+v %v", obs, err)
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatal("lost original command error")
			}
			for _, d := range obs.Diagnostics {
				if d.Message == "SECRET error" {
					t.Fatal("raw error leaked")
				}
			}
		})
	}
}

type runnerFunc func(context.Context, platform.CommandSpec) (platform.ExecResult, error)

func (f runnerFunc) Run(ctx context.Context, spec platform.CommandSpec) (platform.ExecResult, error) {
	return f(ctx, spec)
}

func TestInfoProbeHelperUncertainty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, raw                        string
		missingFiles, permission, cancel bool
	}{
		{"no files", infoJSON, true, false, false},
		{"no helper path", `{"host":{"networkBackend":"netavark"}}`, false, false, false},
		{"permission denied", infoJSON, false, true, false},
		{"cancel after command", infoJSON, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			for _, dir := range []string{"/usr", "/usr/libexec", "/usr/libexec/podman"} {
				mem.AddDir(dir, 0o755)
			}
			if tt.permission {
				mem.AddError("/usr/libexec/podman/netavark", fs.ErrPermission)
			}
			reader := platform.NewScopedMemReader("/", mem)
			defer reader.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			runner := runnerFunc(func(context.Context, platform.CommandSpec) (platform.ExecResult, error) {
				if tt.cancel {
					cancel()
				}
				return platform.ExecResult{Stdout: []byte(tt.raw)}, nil
			})
			env := platform.NewEnvironment(nil, nil, nil, runner).WithFiles(reader)
			if tt.missingFiles {
				env = env.WithFiles(nil)
			}
			probe := podman.InfoProbe{Timestamp: time.Unix(1, 0)}
			obs, err := probe.Run(ctx, env)
			if obs.Completeness != model.Partial || len(obs.Diagnostics) == 0 {
				t.Fatal(obs, err)
			}
		})
	}
	probe := podman.InfoProbe{}
	if _, err := probe.Run(t.Context(), platform.Environment{}); err == nil {
		t.Fatal("missing command service accepted")
	}
}
