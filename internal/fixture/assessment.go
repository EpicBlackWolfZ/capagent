package fixture

import (
	"errors"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
	"slices"
	"strings"
)

const assessmentProbe = "assessment"
const maxAssessmentCommands = 4

func validateAssessment(d *Document) error {
	if d.Context.Identity.Execution == nil || d.Context.Identity.Target == nil || d.Context.Identity.Current == nil ||
		len(d.Commands) > maxAssessmentCommands || d.Host == nil || d.PodmanPath == "" || podman.ValidateExecutablePath(d.PodmanPath) != nil ||
		d.UserQuery && !d.Active {
		return errors.New("invalid assessment fixture")
	}
	// Reuse the host data/failure bounds without granting extra command authority.
	host := *d
	host.Probe, host.Runtime, host.Endpoint, host.Requirement, host.Commands, host.UserQuery = hostProbe, "", "", nil, nil, false
	if err := validateHost(&host); err != nil {
		return err
	}
	_, err := assessmentPolicy(d)
	return err
}

func assessmentPolicy(d *Document) (platform.EnvPolicy, error) {
	policy, err := platform.NewEnvPolicy(nil, d.Environment)
	if err != nil {
		return platform.EnvPolicy{}, err
	}
	narrow, err := podman.TargetEnvironment(*d.Context.Identity.Target, policy)
	if err != nil {
		return platform.EnvPolicy{}, err
	}
	if !slices.Equal(policy.Variables(), narrow.Variables()) {
		return platform.EnvPolicy{}, errors.New("assessment requires explicit directory policy")
	}
	return narrow, nil
}

func validateAssessmentCommands(d *Document, specs []platform.CommandSpec, policy platform.EnvPolicy) error {
	var allowed []platform.CommandSpec
	if d.Active {
		allowed = append(allowed, platform.CommandSpec{Path: d.PodmanPath, Args: []string{"--version"},
			Env: policy, Dir: "/", Timeout: podman.VersionTimeout},
			platform.CommandSpec{Path: d.PodmanPath, Args: podman.LocalInfoArgs(), Env: policy, Dir: "/", Timeout: podman.InfoTimeout})
	}
	for _, executable := range []string{"/usr/bin/systemctl", "/bin/systemctl"} {
		allowed = append(allowed, platform.CommandSpec{Path: executable, Args: []string{"--version"},
			Dir: "/", Timeout: platform.HostVersionTimeout})
		if d.UserQuery {
			runtimeDir := ""
			for _, entry := range policy.Variables() {
				if value, ok := strings.CutPrefix(entry, "XDG_RUNTIME_DIR="); ok {
					runtimeDir = value
				}
			}
			query, err := platform.UserManagerCommand(executable, runtimeDir)
			if err != nil {
				return err
			}
			allowed = append(allowed, query)
		}
	}
	for _, spec := range specs {
		matched := false
		for _, candidate := range allowed {
			matched = matched || sameCommand(spec, candidate)
		}
		if !matched {
			return errors.New("assessment fixture command not allowlisted")
		}
	}
	return nil
}
