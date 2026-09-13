package podman

import (
	"context"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

type NetworkHelperProbe struct {
	Source model.Observation
	Role   string
	Now    func() time.Time
}

func NetworkHelperProbes(source model.Observation, now func() time.Time) []NetworkHelperProbe {
	if source.Configuration == nil || source.Configuration.Family != "network" {
		return nil
	}
	var probes []NetworkHelperProbe
	for _, role := range []string{"netavark", "aardvark_dns", pastaName, slirpName} {
		probes = append(probes, NetworkHelperProbe{Source: source, Role: role, Now: now})
	}
	return probes
}

func (p NetworkHelperProbe) ID() string           { return "podman.executable.configuration." + p.Role }
func (NetworkHelperProbe) Dependencies() []string { return nil }

func (p NetworkHelperProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(p.Now))
	x := &model.ExecutableObservation{Role: p.Role, Source: sourceConfiguration, SourceID: p.Source.ID,
		Candidates: []model.ExecutableCandidate{}}
	obs.Executable = x
	if ctx.Err() != nil {
		return failedRuntimeObservation(obs, "configured_selection_incomplete", ctx.Err())
	}
	c := p.Source.Configuration
	if c != nil {
		x.RuntimePath = c.RuntimePath
	}
	if c == nil || c.Network == nil || !c.SelectionComplete || !c.ParseComplete || p.Source.Scope != env.Scope() ||
		p.Source.Completeness != model.Complete || env.Files() == nil {
		return failedRuntimeObservation(obs, "configured_selection_incomplete", platform.ErrIncomplete)
	}
	plan, known := c.Network.HelperPaths[p.Role]
	if !known || plan.InheritedDefault {
		return failedRuntimeObservation(obs, "configured_default_unobserved", platform.ErrIncomplete)
	}
	probe := ConfiguredHelperProbe{Role: p.Role}
	for _, name := range plan.Values {
		if probe.candidate(ctx, env.Files(), &obs, name, true) {
			break
		}
	}
	return obs, ctx.Err()
}
