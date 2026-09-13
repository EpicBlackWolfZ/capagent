package app

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func projectHost(context model.HostContext, observations []model.Observation) (model.HostContext, model.Completeness) {
	completeness := model.Unobserved
	for _, obs := range observations {
		h := obs.Host
		if h == nil {
			continue
		}
		if completeness == model.Unobserved {
			completeness = model.Complete
		}
		if obs.Completeness != model.Complete {
			completeness = model.Partial
		}
		if h.Cgroups != nil {
			context.CgroupVersion = h.Cgroups.Mode
		}
		if h.OS != nil {
			context.OS = h.OS.ID
			context.OSVersion = h.OS.VersionID
		}
		if h.Kernel != nil {
			context.Kernel = h.Kernel.Release
			context.Architecture = h.Kernel.Architecture
		}
		if h.Systemd != nil {
			context.SystemdActive = copyValue(h.Systemd.Running)
		}
	}
	return context, completeness
}

func hostExit(report *output.Report) int {
	if report.Host.Completeness == string(model.Complete) {
		return ExitSatisfied
	}
	return ExitIndeterminate
}
