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
	source, network, _ := p.Collect(ctx, env) // Fixed diagnostics and completeness retain every collection failure.
	observations = append(observations, source, network)
	for _, helper := range podman.NetworkHelperProbes(network, now) {
		obs, _ := helper.Run(ctx, env) // Candidate metadata and fixed diagnostics retain incomplete access.
		observations = append(observations, obs)
	}
	for _, helper := range podman.ConfiguredHelperProbes(source, now) {
		obs, _ := helper.Run(ctx, env) // Selected-source and metadata uncertainty remain explicit in the observation.
		observations = append(observations, obs)
	}
	return collectStorage(ctx, env, observations, p, source, now)
}

func collectStorage(ctx context.Context, env platform.Environment, observations []model.Observation,
	selection podman.EngineProbe, engine model.Observation, now func() time.Time) []model.Observation {
	source, _ := (podman.StorageProbe{Selection: selection, Engine: engine}).Run(ctx, env)
	// Fixed diagnostics, source status and completeness preserve collection errors.
	observations = append(observations, source)
	for _, writable := range []bool{true, false} {
		obs, _ := (podman.StoragePathProbe{Source: source, Writable: writable, Now: now}).Run(ctx, env)
		observations = append(observations, obs)
	}
	sources := []model.Observation{source}
	for _, obs := range observations {
		if obs.Podman != nil && obs.Podman.Available != nil && *obs.Podman.Available && obs.Completeness == model.Complete {
			sources = append(sources, obs)
			paths, _ := (podman.StoragePathProbe{Source: obs, Writable: true, Now: now}).Run(ctx, env)
			observations = append(observations, paths)
		}
	}
	for _, source := range sources {
		for _, helper := range podman.StorageHelperProbes(source, now) {
			obs, _ := helper.Run(ctx, env)
			observations = append(observations, obs)
		}
	}
	return observations
}
