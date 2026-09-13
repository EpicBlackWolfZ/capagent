package app

import (
	"context"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func collectConfiguration(ctx context.Context, env platform.Environment,
	observations []model.Observation, input Input,
) []model.Observation {
	if !input.AssessPodman || input.Context.Identity.Execution == nil {
		return observations
	}
	now := input.Now
	if now == nil {
		now = func() time.Time { return input.At }
	}
	p := podman.EngineProbe{Target: *input.Context.Identity.Target, Environment: input.PodmanEnvironment,
		EnvironmentError: input.PodmanEnvironmentError, Now: now}
	for _, obs := range observations {
		if obs.Discovery != nil {
			p.Path = obs.Discovery.Path
		}
	}
	for _, obs := range observations {
		if obs.Version != nil && obs.Version.Path == p.Path &&
			(p.Version.ID == "" || obs.Timestamp.After(p.Version.Timestamp)) {
			p.Version = obs
		}
	}
	source, _ := p.Run(ctx, env) // Fixed diagnostics and completeness retain every collection failure.
	observations = append(observations, source)
	for _, helper := range podman.ConfiguredHelperProbes(source, now) {
		obs, _ := helper.Run(ctx, env) // Selected-source and metadata uncertainty remain explicit in the observation.
		observations = append(observations, obs)
	}
	return observations
}
