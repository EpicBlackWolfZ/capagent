package app

import (
	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
	"time"
)

func prerequisiteProbes(target model.UserIdentity, policy platform.EnvPolicy, policyErr error, now func() time.Time) []probe.Probe {
	probes := []probe.Probe{podman.QuadletGenerator(target.UID != 0, now),
		podman.QuadletLocations{Target: target, Environment: policy, EnvironmentError: policyErr, Now: now}}
	for _, p := range podman.HelperProbes(now) {
		probes = append(probes, p)
	}
	return probes
}

func addPrerequisites(input *Input, probes []probe.Probe, current model.EvaluationContext,
	policy platform.EnvPolicy, policyErr error, now func() time.Time,
) []probe.Probe {
	if !input.AssessPodman {
		return probes
	}
	input.Definitions = append(input.Definitions, capability.HelperDefinitions()...)
	rootless := current.Identity.Target != nil && current.Identity.Target.UID != 0
	input.Definitions = append(input.Definitions, capability.QuadletDefinitions(rootless)...)
	if current.Identity.Execution != nil {
		probes = append(probes, prerequisiteProbes(*current.Identity.Target, policy, policyErr, now)...)
	}
	return probes
}

func captureAssessmentEnvironment(opts Options, services currentServices, current model.EvaluationContext,
) (currentServices, platform.EnvPolicy, error) {
	var policy platform.EnvPolicy
	if opts.Runtime != "podman" || current.Identity.Execution == nil {
		return services, policy, nil
	}
	err := platform.ErrIncomplete
	if services.capture != nil {
		policy, err = services.capture()
	}
	// Passive source selection, user-manager context and both runtime commands
	// share this single named capture. Delegated workers supply target values.
	services.userEnvironment = func() (platform.EnvPolicy, error) { return policy, err }
	return services, policy, err
}
