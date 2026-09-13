package app

import (
	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"slices"
	"strings"
)

func projectContextMeasurements(context model.EvaluationContext, observations []model.Observation) model.EvaluationContext {
	for _, obs := range observations {
		if u := obs.UserContext; u != nil {
			if u.Runtime.Valid != nil && *u.Runtime.Valid {
				context.Identity.XDGRuntimeDir = u.Runtime.Path
			}
			context.Identity.HasUserSystemd = copyValue(u.Accessible)
		}
		if s := obs.SubIDs; s != nil && s.Provider == "files" {
			if s.UID.Valid != nil && *s.UID.Valid && s.UID.Total != nil {
				context.Identity.SubUIDRanges = slices.Clone(s.UID.Ranges)
			}
			if s.GID.Valid != nil && *s.GID.Valid && s.GID.Total != nil {
				context.Identity.SubGIDRanges = slices.Clone(s.GID.Ranges)
			}
		}
	}
	return context
}

func currentUserProbe(opts Options, services currentServices, target model.UserIdentity) host.UserContextProbe {
	p := host.UserContextProbe{Target: target, Active: opts.Active, Now: services.now}
	if services.worker == nil && services.userEnvironment != nil {
		policy, err := services.userEnvironment()
		p.EnvironmentError = err
		for _, entry := range policy.Variables() {
			name, value, _ := strings.Cut(entry, "=")
			if name == "XDG_RUNTIME_DIR" {
				p.RuntimeDirectory = &value
			}
		}
	}
	return p
}
