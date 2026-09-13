package app

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"slices"
)

func projectContextMeasurements(context model.EvaluationContext, observations []model.Observation) model.EvaluationContext {
	for _, obs := range observations {
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
