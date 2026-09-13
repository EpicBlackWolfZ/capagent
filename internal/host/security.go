package host

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

var namespaceKinds = []string{"user", "pid", "net", "mnt", "ipc", "uts", "cgroup"}

type NamespaceProbe struct{ Now func() time.Time }

func (NamespaceProbe) ID() string             { return "host.namespaces" }
func (NamespaceProbe) Dependencies() []string { return nil }
func (p NamespaceProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	obs.Host.Namespaces = &model.NamespaceObservation{Namespaces: []model.Namespace{}}
	for _, kind := range namespaceKinds {
		source := "proc/self/ns/" + kind
		if ctx.Err() != nil {
			recordSource(&obs, source, ctx.Err())
			break
		}
		link, err := env.Files().Readlink(source)
		if err == nil && (!namespacePattern.MatchString(link) || !strings.HasPrefix(link, kind+":")) {
			err = platform.ErrMalformed
		}
		recordSource(&obs, source, err)
		if err == nil {
			obs.Host.Namespaces.Namespaces = append(obs.Host.Namespaces.Namespaces, model.Namespace{Kind: kind, ID: link})
		}
	}
	finishSource(&obs, ctx.Err())
	return obs, ctx.Err()
}

type SecurityProbe struct{ Now func() time.Time }

func (SecurityProbe) ID() string             { return "host.security" }
func (SecurityProbe) Dependencies() []string { return nil }
func (p SecurityProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	state := &model.SecurityObservation{SELinux: "unknown", LSMs: []string{}, AppArmorProfiles: []model.AppArmorProfile{},
		SeccompActions: []string{}, ProcessScope: "process-leader"}
	obs.Host.Security = state
	lsm, lsmErr := env.Files().ReadFile(ctx, "sys/kernel/security/lsm")
	recordSource(&obs, "sys/kernel/security/lsm", lsmErr)
	if lsmErr == nil {
		names := strings.Split(strings.TrimSpace(string(lsm)), ",")
		for _, name := range names {
			if !osIDPattern.MatchString(name) {
				lsmErr = platform.ErrMalformed
			}
		}
		if lsmErr == nil {
			state.LSMs = names
		} else {
			finishSource(&obs, lsmErr)
		}
	}
	enforce, enforceErr := env.Files().ReadFile(ctx, "sys/fs/selinux/enforce")
	if enforceErr == nil {
		switch strings.TrimSpace(string(enforce)) {
		case "0":
			state.SELinux = "permissive"
		case "1":
			state.SELinux = "enforcing"
		default:
			enforceErr = platform.ErrMalformed
		}
	} else if errors.Is(enforceErr, fs.ErrNotExist) && len(state.LSMs) > 0 && !slices.Contains(state.LSMs, "selinux") {
		state.SELinux = "disabled"
		enforceErr = nil
	}
	recordSource(&obs, "sys/fs/selinux/enforce", enforceErr)
	readAppArmor(ctx, env, &obs, state)
	status, statusErr := env.Files().ReadFile(ctx, "proc/self/status")
	recordSource(&obs, "proc/self/status", statusErr)
	lines, parseErr := hostLines(status, statusErr)
	finishSource(&obs, parseErr)
	for _, line := range lines {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch key {
		case "Seccomp":
			n, e := strconv.ParseUint(strings.TrimSpace(value), 10, versionNumberBits)
			if e == nil && n <= 2 {
				state.SeccompMode = ptr(uint32(n))
			} else {
				finishSource(&obs, platform.ErrMalformed)
			}
		case "NoNewPrivs":
			n, e := strconv.ParseUint(strings.TrimSpace(value), 10, versionNumberBits)
			if e == nil && n <= 1 {
				state.NoNewPrivileges = ptr(n == 1)
			} else {
				finishSource(&obs, platform.ErrMalformed)
			}
		}
	}
	if state.SeccompMode == nil || state.NoNewPrivileges == nil {
		finishSource(&obs, platform.ErrIncomplete)
	}
	actions, actionsErr := env.Files().ReadFile(ctx, "proc/sys/kernel/seccomp/actions_avail")
	recordSource(&obs, "proc/sys/kernel/seccomp/actions_avail", actionsErr)
	state.SeccompActions, parseErr = platform.ParseControllerList(ctx, actions, actionsErr)
	finishSource(&obs, parseErr)
	state.UnprivilegedUserNSClone = readBoolSysctl(ctx, env, &obs, "proc/sys/kernel/unprivileged_userns_clone", true)
	maximum, maxErr := env.Files().ReadFile(ctx, "proc/sys/user/max_user_namespaces")
	recordSource(&obs, "proc/sys/user/max_user_namespaces", maxErr)
	if maxErr == nil {
		n, e := strconv.ParseUint(strings.TrimSpace(string(maximum)), 10, 64)
		if e == nil {
			state.MaxUserNamespaces = &n
		} else {
			finishSource(&obs, platform.ErrMalformed)
		}
	}
	return obs, ctx.Err()
}

func readAppArmor(ctx context.Context, env platform.Environment, obs *model.Observation, state *model.SecurityObservation) {
	data, err := env.Files().ReadFile(ctx, "sys/module/apparmor/parameters/enabled")
	if err == nil {
		switch strings.TrimSpace(string(data)) {
		case "Y":
			state.AppArmorEnabled = ptr(true)
		case "N":
			state.AppArmorEnabled = ptr(false)
		default:
			err = platform.ErrMalformed
		}
	}
	if errors.Is(err, fs.ErrNotExist) && len(state.LSMs) > 0 {
		state.AppArmorEnabled = ptr(slices.Contains(state.LSMs, "apparmor"))
		err = nil
	}
	recordSource(obs, "sys/module/apparmor/parameters/enabled", err)
	if state.AppArmorEnabled != nil && !*state.AppArmorEnabled {
		return
	}
	data, err = env.Files().ReadFile(ctx, "sys/kernel/security/apparmor/profiles")
	recordSource(obs, "sys/kernel/security/apparmor/profiles", err)
	lines, parseErr := hostLines(data, err)
	finishSource(obs, parseErr)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, mode, ok := strings.Cut(line, " (")
		mode = strings.TrimSuffix(mode, ")")
		if !ok || !strings.HasSuffix(line, ")") || !validHostText(name) ||
			!slices.Contains([]string{"enforce", "complain", "kill", "unconfined"}, mode) {
			finishSource(obs, platform.ErrMalformed)
			continue
		}
		state.AppArmorProfiles = append(state.AppArmorProfiles, model.AppArmorProfile{Name: name, Mode: mode})
	}
}
func readBoolSysctl(ctx context.Context, env platform.Environment, obs *model.Observation, source string, optional bool) *bool {
	data, err := env.Files().ReadFile(ctx, source)
	if optional && errors.Is(err, fs.ErrNotExist) {
		recordSource(obs, source, nil)
		return nil
	}
	recordSource(obs, source, err)
	if err != nil {
		return nil
	}
	switch strings.TrimSpace(string(data)) {
	case "0":
		return ptr(false)
	case "1":
		return ptr(true)
	default:
		finishSource(obs, platform.ErrMalformed)
		return nil
	}
}
