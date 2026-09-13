package podman

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const quadletReference = "https://github.com/podman-container-tools/podman/blob/v5.8.4/pkg/systemd/quadlet/unitdirs.go"

// QuadletGenerator follows the standard systemd generator search hierarchy.
// Custom compiled generator directories are outside this bounded inventory.
func QuadletGenerator(rootless bool, now func() time.Time) ExecutableProbe {
	kind := "system"
	if rootless {
		kind = "user"
	}
	p := ExecutableProbe{Role: "quadlet", Source: "systemd_" + kind + "_generators", Generator: true, Now: now}
	for _, prefix := range []string{"/run/systemd", "/etc/systemd", "/usr/local/lib/systemd", "/usr/lib/systemd"} {
		p.Paths = append(p.Paths, prefix+"/"+kind+"-generators/podman-"+kind+"-generator")
	}
	return p
}

type QuadletLocations struct {
	Target           model.UserIdentity
	Environment      platform.EnvPolicy
	EnvironmentError error
	Now              func() time.Time
}

func (QuadletLocations) ID() string             { return "podman.quadlet.locations" }
func (QuadletLocations) Dependencies() []string { return nil }
func (p QuadletLocations) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(p.Now))
	obs.Quadlet = &model.QuadletObservation{Rootless: p.Target.UID != 0, Reference: quadletReference, Locations: []model.SearchLocation{}}
	paths := []string{"/run/containers/systemd", "/etc/containers/systemd", "/usr/share/containers/systemd"}
	if p.Target.UID != 0 {
		var err error
		paths, err = p.userLocations()
		if err != nil {
			return failedRuntimeObservation(obs, "quadlet_environment_unavailable", err)
		}
	}
	if env.Files() == nil {
		return failedRuntimeObservation(obs, "quadlet_locations_unavailable", platform.ErrIncomplete)
	}
	for _, name := range paths {
		location := model.SearchLocation{Path: name}
		if err := ctx.Err(); err != nil {
			return failedRuntimeObservation(obs, "quadlet_locations_cancelled", err)
		}
		info, err := env.Files().Stat(strings.TrimPrefix(name, "/"))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			location.Present = boolPointer(false)
		case err != nil:
			obs = incompleteObservation(obs, "quadlet_location_denied")
		default:
			location.Present, location.Directory = boolPointer(true), boolPointer(info.IsDir())
		}
		obs.Quadlet.Locations = append(obs.Quadlet.Locations, location)
	}
	obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: "quadlet_generation_unverified",
		Message:   "documented candidate input locations only; generator execution and working deployment have not been verified",
		Reference: quadletReference})
	return obs, nil
}

func (p QuadletLocations) userLocations() ([]string, error) {
	if p.EnvironmentError != nil {
		return nil, p.EnvironmentError
	}
	policy, err := TargetEnvironment(p.Target, p.Environment)
	if err != nil {
		return nil, err
	}
	values := directoryValues(policy)
	configHome := values["XDG_CONFIG_HOME"]
	if configHome == "" {
		configHome = values["HOME"] + "/.config"
	}
	var paths []string
	if runtimeDir := values["XDG_RUNTIME_DIR"]; runtimeDir != "" {
		paths = append(paths, runtimeDir+"/containers/systemd")
	}
	return append(paths, configHome+"/containers/systemd", "/etc/containers/systemd/users",
		"/etc/containers/systemd/users/"+strconv.FormatUint(uint64(p.Target.UID), 10)), nil
}
