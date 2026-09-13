package podman

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const (
	VersionTimeout    = 5 * time.Second
	InfoTimeout       = 30 * time.Second
	InspectionTimeout = 40 * time.Second
	privateAccess     = 0o700
)

func InspectionEnvNames() []string {
	return []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"}
}

type InspectionCommands struct {
	Version platform.CommandSpec
	Info    platform.CommandSpec
}

// PrepareInspection narrows an already captured environment to the current-user
// directory policy. It performs no execution and never creates missing paths.
func PrepareInspection(ctx context.Context, files platform.ScopedView, user model.UserIdentity, executable string,
	captured platform.EnvPolicy,
) (InspectionCommands, error) {
	if err := ctx.Err(); err != nil {
		return InspectionCommands{}, err
	}
	if files == nil || executable == "" || ValidateExecutablePath(executable) != nil {
		return InspectionCommands{}, errors.New("invalid inspection services or executable")
	}
	values := make(map[string]string)
	for _, entry := range captured.Variables() {
		name, value, _ := strings.Cut(entry, "=")
		switch name {
		case "HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR":
			if !safeInfoPath(value) {
				return InspectionCommands{}, errors.New("invalid inspection directory")
			}
			values[name] = value
		}
	}
	if _, present := values["HOME"]; !present {
		if !safeInfoPath(user.HomeDir) {
			return InspectionCommands{}, errors.New("current home directory is unavailable")
		}
		values["HOME"] = user.HomeDir
	}
	home, err := files.Stat(path.Clean(strings.TrimPrefix(values["HOME"], "/")))
	if err != nil || !home.IsDir() {
		return InspectionCommands{}, errors.New("current home directory is inaccessible")
	}
	runtimeDir, present := values["XDG_RUNTIME_DIR"]
	if user.UID != 0 && !present {
		return InspectionCommands{}, errors.New("rootless inspection requires an explicit runtime directory")
	}
	if present {
		if err := validateRuntimeDirectory(files, runtimeDir, user.UID); err != nil {
			return InspectionCommands{}, err
		}
	}
	policy, err := platform.NewEnvPolicy(nil, values)
	if err != nil {
		return InspectionCommands{}, err
	}
	return InspectionCommands{
		Version: platform.CommandSpec{Path: executable, Args: []string{"--version"}, Env: policy, Dir: "/", Timeout: VersionTimeout},
		Info:    platform.CommandSpec{Path: executable, Args: LocalInfoArgs(), Env: policy, Dir: "/", Timeout: InfoTimeout},
	}, nil
}

func validateRuntimeDirectory(files platform.ScopedView, name string, uid uint32) error {
	info, err := files.Stat(path.Clean(strings.TrimPrefix(name, "/")))
	if err != nil || !info.IsDir() || info.Mode().Perm()&privateAccess != privateAccess {
		return errors.New("runtime directory is inaccessible")
	}
	owner, known := platform.OwnershipOf(info)
	if !known || owner.UID != uid {
		return errors.New("runtime directory ownership does not match current user")
	}
	return nil
}
