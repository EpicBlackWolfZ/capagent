package app

import (
	"slices"
	"sort"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

func projectReport(input Input, observations []model.Observation, evaluation capability.Evaluation,
	decision requirement.Result, results []probe.ProbeResult) *output.Report {
	runtimes := make(map[string]output.RuntimeInfo)
	runtime := output.RuntimeInfo{Completeness: string(model.Unobserved)}
	for _, obs := range observations {
		if obs.Podman == nil {
			continue
		}
		runtime.Completeness = string(obs.Completeness)
		runtime.Accessible = copyValue(obs.Podman.Available)
		runtime.Version = obs.Podman.Version
		if obs.Podman.NetworkBackend != nil {
			runtime.NetworkBackend = *obs.Podman.NetworkBackend
		}
		if obs.Podman.StorageDriver != nil {
			runtime.StorageDriver = *obs.Podman.StorageDriver
		}
	}
	runtimes[input.Scope.Runtime] = runtime
	report := output.NewReportFromModel(input.Context, runtimes, evaluation.Candidate.Capabilities)
	report.Context.Completeness = string(model.Unobserved)
	if input.Context.Identity.Current != nil || input.Context.Identity.Target != nil {
		report.Context.Completeness = string(model.Partial)
	}
	report.Host.Completeness = string(model.Unobserved)
	if input.Context.Host != (model.HostContext{}) {
		report.Host.Completeness = string(model.Partial)
	}
	if report.Host.OS == "" {
		report.Host.OS = "unknown"
	}
	if report.Host.CgroupVersion == "" {
		report.Host.CgroupVersion = "unknown"
	}
	trace := output.NewEvaluationTrace(input.Scope, input.At, input.Provenance)
	trace.Current = projectIdentity(input.Context.Identity.Current)
	trace.Target = projectIdentity(input.Context.Identity.Target)
	for _, ns := range input.Context.Namespaces {
		trace.Namespaces = append(trace.Namespaces, output.Namespace{Kind: ns.Kind, ID: ns.ID})
	}
	sort.Slice(trace.Namespaces, func(i, j int) bool { return trace.Namespaces[i].Kind < trace.Namespaces[j].Kind })
	for _, obs := range observations {
		record := output.ObservationRecord{ID: obs.ID, ProbeID: obs.ProbeID, Scope: output.ProjectScope(obs.Scope), Timestamp: obs.Timestamp,
			Completeness: string(obs.Completeness), Facts: []output.FactRecord{}, Diagnostics: projectDiagnostics(obs.Diagnostics)}
		for _, fact := range obs.Facts {
			record.Facts = append(record.Facts, output.FactRecord{ID: fact.ID, Source: fact.Source,
				Timestamp: fact.Timestamp, Completeness: string(fact.Completeness)})
		}
		sort.Slice(record.Facts, func(i, j int) bool { return record.Facts[i].ID < record.Facts[j].ID })
		trace.Observations = append(trace.Observations, record)
		trace.Diagnostics = append(trace.Diagnostics, record.Diagnostics...)
	}
	sort.Slice(trace.Observations, func(i, j int) bool { return trace.Observations[i].ID < trace.Observations[j].ID })
	for _, ev := range evaluation.Candidate.Evidence {
		trace.Evidence = append(trace.Evidence, projectEvidence(ev))
	}
	sort.Slice(trace.Evidence, func(i, j int) bool { return trace.Evidence[i].ID < trace.Evidence[j].ID })
	for _, resolution := range evaluation.Resolutions {
		key := string(resolution.Capability.ID)
		capReport := report.Capabilities[key]
		for _, ref := range resolution.Superseded {
			capReport.Superseded = append(capReport.Superseded, ref.ID)
		}
		report.Capabilities[key] = capReport
	}
	trace.Requirement = projectRequirement(decision)
	trace.Diagnostics = append(trace.Diagnostics, projectDiagnostics(evaluation.Diagnostics)...)
	trace.Diagnostics = append(trace.Diagnostics, trace.Requirement.Diagnostics...)
	for _, result := range results {
		if result.Status != probe.ProbeSucceeded {
			trace.Diagnostics = append(trace.Diagnostics, output.Diagnostic{Code: "probe_" + result.Status.String(),
				Message: "probe did not complete successfully", Reference: result.ProbeID})
		}
	}
	report.Evaluation = trace
	return report
}

func projectEvidence(ev model.Evidence) output.EvidenceRecord {
	record := output.EvidenceRecord{ID: ev.ID, Source: ev.Source, Claim: ev.Claim, Scope: output.ProjectScope(ev.Scope),
		Timestamp:    ev.Timestamp,
		Completeness: string(ev.Completeness), Precedence: ev.Precedence.String(), State: ev.State.String(), Confidence: ev.Confidence.String(),
		Observations: []string{}, DependsOn: []string{}}
	for _, ref := range ev.Observations {
		record.Observations = append(record.Observations, ref.ID)
	}
	for _, ref := range ev.DependsOn {
		record.DependsOn = append(record.DependsOn, ref.ID)
	}
	slices.Sort(record.Observations)
	slices.Sort(record.DependsOn)
	return record
}
func projectRequirement(result requirement.Result) output.RequirementResult {
	out := output.RequirementResult{State: result.State.String(), Reason: result.Reason, Diagnostics: projectDiagnostics(result.Diagnostics),
		Children: []output.RequirementResult{}}
	if result.Scope != nil {
		scope := output.ProjectScope(*result.Scope)
		out.Scope = &scope
	}
	for _, child := range result.Children {
		out.Children = append(out.Children, projectRequirement(child))
	}
	return out
}
func projectDiagnostics(input []model.Diagnostic) []output.Diagnostic {
	out := make([]output.Diagnostic, 0, len(input))
	for _, d := range input {
		out = append(out, output.Diagnostic{Code: d.Code, Message: d.Message, Reference: d.Reference})
	}
	return out
}
func projectIdentity(input *model.UserIdentity) *output.Identity {
	if input == nil {
		return nil
	}
	return &output.Identity{UID: input.UID, GID: input.GID, GroupsKnown: input.GroupsKnown,
		SupplementaryGroups: append([]uint32{}, input.SupplementaryGroups...)}
}
func snapshotContext(input model.EvaluationContext) model.EvaluationContext {
	out := input
	out.Identity.Current = copyIdentity(input.Identity.Current)
	out.Identity.Target = copyIdentity(input.Identity.Target)
	out.Identity.IsRootless = copyValue(input.Identity.IsRootless)
	out.Identity.HasUserSystemd = copyValue(input.Identity.HasUserSystemd)
	out.Identity.InContainer = copyValue(input.Identity.InContainer)
	out.Host.SystemdActive = copyValue(input.Host.SystemdActive)
	out.Identity.SubUIDRanges = slices.Clone(input.Identity.SubUIDRanges)
	out.Identity.SubGIDRanges = slices.Clone(input.Identity.SubGIDRanges)
	out.Namespaces = slices.Clone(input.Namespaces)
	out.Runtime.ActiveRuntimes = slices.Clone(input.Runtime.ActiveRuntimes)
	out.Configuration.SearchPaths = slices.Clone(input.Configuration.SearchPaths)
	return out
}
func copyIdentity(input *model.UserIdentity) *model.UserIdentity {
	out := copyValue(input)
	if out != nil {
		out.SupplementaryGroups = slices.Clone(input.SupplementaryGroups)
	}
	return out
}
func copyValue[T any](input *T) *T {
	if input == nil {
		return nil
	}
	out := *input
	return &out
}
