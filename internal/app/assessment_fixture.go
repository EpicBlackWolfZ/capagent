package app

import (
	"context"
	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/fixture"
	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
	"strings"
	"time"
)

func evaluateAssessmentFixture(ctx context.Context, doc *fixture.Document, input Input,
	services *fixture.Services,
) (*output.Report, error) {
	now := func() time.Time { return doc.Timestamp }
	target := *doc.Context.Identity.Target
	input.AssessPodman = true
	input.PodmanEnvironment = services.Policy
	input.Definitions = []capability.Definition{capability.PodmanDefinition(),
		capability.PodmanInfoDefinition(), capability.NetavarkDefinition()}
	input.Definitions = append(input.Definitions, capability.HelperDefinitions()...)
	input.Definitions = append(input.Definitions, capability.EngineDefinitions()...)
	input.Definitions = append(input.Definitions, capability.NetworkDefinitions(target.UID != 0)...)
	input.Definitions = append(input.Definitions, capability.StorageDefinitions()...)
	input.Definitions = append(input.Definitions, capability.QuadletDefinitions(target.UID != 0)...)
	probes := host.Probes(now)
	user := host.UserContextProbe{Target: target, Active: doc.UserQuery, Now: now}
	for _, entry := range services.Policy.Variables() {
		if value, ok := strings.CutPrefix(entry, "XDG_RUNTIME_DIR="); ok {
			user.RuntimeDirectory = &value
		}
	}
	probes = append(probes, host.SubIDProbe{Target: target, Now: now}, user)
	probes = append(probes, prerequisiteProbes(target, services.Policy, nil, now)...)
	discovery, _ := (podman.DiscoveryProbe{Path: doc.PodmanPath, Now: now}).Run(ctx, services.Environment)
	// Discovery/setup failures retain missing inspection evidence; no substitute
	// command or successful runtime payload is manufactured during offline replay.
	input.Observations = []model.Observation{discovery}
	if d := discovery.Discovery; doc.Active && d != nil && d.File != nil && d.File.Regular && d.File.ExecutableBits {
		commands, err := podman.PrepareInspection(ctx, services.Environment.Files(), target, doc.PodmanPath, services.Policy)
		if err == nil {
			probes = append(probes, podman.VersionProbe{Command: commands.Version, Now: now},
				podman.InfoProbe{Command: commands.Info, Now: now, AfterVersion: true})
		}
	}
	return evaluateOwned(ctx, input, services.Environment, probes, services)
}
