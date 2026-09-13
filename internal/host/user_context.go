package host

import (
	"context"
	"errors"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

const userSocketSuffix = "/systemd/private"

type UserContextProbe struct {
	Target           model.UserIdentity
	RuntimeDirectory *string
	EnvironmentError error
	Active           bool
	Now              func() time.Time
}

func (UserContextProbe) ID() string             { return "context.user" }
func (UserContextProbe) Dependencies() []string { return nil }
func (p UserContextProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	obs.Host = nil
	state := &model.UserContextObservation{}
	obs.UserContext = state
	readLinger(ctx, env, &obs, p.Target.Username)
	name := "/run/user/" + strconv.FormatUint(uint64(p.Target.UID), 10)
	source := "default"
	if p.RuntimeDirectory != nil {
		name = *p.RuntimeDirectory
		source = "environment"
	}
	if p.EnvironmentError != nil {
		recordSource(&obs, "XDG_RUNTIME_DIR", p.EnvironmentError)
		state.Runtime.Source = source
		return obs, ctx.Err()
	}
	runtime, err := platform.InspectRuntimeDirectory(ctx, env.Files(), name, p.Target.UID)
	runtime.Source = source
	state.Runtime = runtime
	recordSource(&obs, "runtime directory", err)
	if runtime.Valid == nil || !*runtime.Valid {
		obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: "user_runtime_invalid",
			Message: "rootless user services require an existing runtime directory owned by target UID with mode 0700", Reference: p.ID()})
		return obs, ctx.Err()
	}
	socket, err := env.Files().Stat(name[1:] + userSocketSuffix)
	recordSource(&obs, "user manager private socket", missingIsKnown(err))
	if errors.Is(err, fs.ErrNotExist) {
		state.SocketPresent = ptr(false)
		return obs, ctx.Err()
	}
	if err != nil {
		return obs, ctx.Err()
	}
	state.SocketPresent = ptr(true)
	owner, known := platform.OwnershipOf(socket)
	if !known {
		finishSource(&obs, platform.ErrIncomplete)
		return obs, ctx.Err()
	}
	state.SocketValid = ptr(socket.Mode().Type() == fs.ModeSocket && owner.UID == p.Target.UID)
	if !*state.SocketValid {
		hostWarning(&obs, "user_socket_invalid")
		return obs, ctx.Err()
	}
	if !p.Active {
		hostWarning(&obs, "user_manager_query_deferred")
		return obs, ctx.Err()
	}
	queryUserManager(ctx, env, &obs, name)
	return obs, ctx.Err()
}
func readLinger(ctx context.Context, env platform.Environment, obs *model.Observation, username string) {
	if username == "" || (model.TargetSelector{Value: "user:" + username}).IsValid() != nil {
		finishSource(obs, platform.ErrIncomplete)
		hostWarning(obs, "linger_account_unresolved")
		return
	}
	if ctx.Err() != nil {
		finishSource(obs, ctx.Err())
		return
	}
	info, err := env.Files().Stat("var/lib/systemd/linger/" + username)
	recordSource(obs, "linger marker", missingIsKnown(err))
	if errors.Is(err, fs.ErrNotExist) {
		obs.UserContext.LingerEnabled = ptr(false)
	}
	if err == nil {
		obs.UserContext.LingerEnabled = ptr(info.Mode().IsRegular())
		if !info.Mode().IsRegular() {
			hostWarning(obs, "linger_marker_invalid")
		}
	}
}
func queryUserManager(ctx context.Context, env platform.Environment, obs *model.Observation, runtimeDir string) {
	executable := ""
	for _, name := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		info, err := env.Files().Stat(name[1:])
		recordSource(obs, name, missingIsKnown(err))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&mappingExecutableBits == 0 {
			return
		}
		executable = name
		break
	}
	if executable == "" {
		hostWarning(obs, "user_manager_utility_missing")
		return
	}
	state := obs.UserContext
	state.QueryAttempted = true
	result, err := env.UserManager().Query(ctx, executable, runtimeDir)
	if err != nil || result.TimedOut || result.StdoutTruncated || result.StderrTruncated {
		recordSource(obs, "user manager query", platform.ErrIncomplete)
		return
	}
	recordSource(obs, "user manager query", nil)
	if result.ExitCode != 0 {
		state.Accessible = ptr(false)
		hostWarning(obs, "user_manager_unavailable")
		return
	}
	version := strings.TrimSpace(string(result.Stdout))
	if !validHostText(version) || !systemdVersionPattern.MatchString("systemd "+version) {
		finishSource(obs, platform.ErrMalformed)
		return
	}
	state.Accessible = ptr(true)
	state.ManagerVersion = version
}
