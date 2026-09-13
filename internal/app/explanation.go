package app

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/capability"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
	"github.com/EpicBlackWolfZ/capagent/internal/requirement"
)

const maxExplanationBytes = 64 * 1024
const maxExplanationField = 512
const explanationTruncated = "Explanation truncated; inspect JSON for remaining evidence.\n"

type explanationWriter struct {
	text         strings.Builder
	full         bool
	observations map[string]bool
}

func (w *explanationWriter) linef(format string, args ...any) {
	if w.full {
		return
	}
	line := fmt.Sprintf(format, args...) + "\n"
	if w.text.Len()+len(line)+len(explanationTruncated) > maxExplanationBytes {
		w.text.WriteString(explanationTruncated)
		w.full = true
		return
	}
	w.text.WriteString(line)
}

// Keep terminal controls escaped even for a caller-provided report. Only
// selected typed fields are formatted; raw diagnostic messages are excluded.
func explanationText(value string) string {
	if len(value) > maxExplanationField {
		value = value[:maxExplanationField] + "..."
	}
	quoted := strconv.QuoteToASCII(value)
	return quoted[1 : len(quoted)-1]
}

func explanationBool(value *bool) string {
	if value == nil {
		return unknownValue
	}
	return strconv.FormatBool(*value)
}

func writeExplanation(report *output.Report, opts Options, destination io.Writer) error {
	w := &explanationWriter{}
	trace := report.Evaluation
	collection := trace.Collection
	if collection == "" {
		collection = "offline replay"
	}
	if trace.Target == nil {
		w.linef("Target unresolved; execution authority and deployment prerequisites are unmeasured.")
	} else {
		w.linef("Target uid=%d gid=%d rootless=%t user=%s; runtime=%s endpoint=%s; collection=%s",
			trace.Target.UID, trace.Target.GID, trace.Target.UID != 0, explanationText(report.Context.TargetUser),
			explanationText(trace.Scope.Runtime), explanationText(trace.Scope.Endpoint), explanationText(collection))
	}
	explainRuntime(w, report)
	if trace.Scope.Runtime == "" {
		w.linef("Host collection: %s; context: %s", explanationText(report.Host.Completeness), explanationText(report.Context.Completeness))
	} else {
		w.linef("Requirement: %s", explanationText(trace.Requirement.State))
		explainRequirementTree(w, trace.Requirement, 0)
	}
	for _, diagnostic := range trace.Diagnostics {
		if diagnostic.Code == "target_unavailable" || diagnostic.Code == "version_execution_deferred" ||
			diagnostic.Code == "inspection_environment_unavailable" {
			w.linef("Collection diagnostic: %s", explanationText(diagnostic.Code))
		}
	}
	ids := explanationCapabilities(opts, report)
	for _, supported := range []bool{false, true} {
		for _, id := range ids {
			if (report.Capabilities[id].State == "supported") == supported {
				explainCapability(w, report, id)
			}
		}
	}
	explainOtherConfigurationFindings(w, report, ids)
	w.linef("These are inspection/configuration prerequisites; workload deployment, DNS, storage writes and image pulls remain unverified.")
	text := w.text.String()
	n, err := io.WriteString(destination, text)
	if err == nil && n != len(text) {
		return io.ErrShortWrite
	}
	return err
}

func explainRequirementTree(w *explanationWriter, result output.RequirementResult, depth int) {
	if depth > requirement.MaxDepth+1 || w.full {
		return
	}
	w.linef("%s%s: %s", strings.Repeat("  ", depth+1), explanationText(result.Reason), explanationText(result.State))
	for _, child := range result.Children {
		explainRequirementTree(w, child, depth+1)
	}
}

func explanationCapabilities(opts Options, report *output.Report) []string {
	if opts.requirementNode == nil {
		if report.Evaluation.Scope.Runtime == "" {
			return nil
		}
		ids := []string{string(capability.PodmanID)}
		if report.Evaluation.Collection == "active" {
			ids = append(ids, string(capability.PodmanInfoID))
		}
		return ids
	}
	var ids []string
	seen, count := map[string]bool{}, 0
	var visit func(*requirement.Node, int)
	visit = func(node *requirement.Node, depth int) {
		if node == nil || depth > requirement.MaxDepth || count >= requirement.MaxNodes {
			return
		}
		count++
		if node.Capability != "" {
			id := string(node.Capability)
			if !seen[id] {
				ids, seen[id] = append(ids, id), true
			}
			return
		}
		visit(node.Not, depth+1)
		for _, nodes := range [][]*requirement.Node{node.All, node.Any} {
			for _, child := range nodes {
				visit(child, depth+1)
			}
		}
	}
	visit(opts.requirementNode, 1)
	return ids
}

func explainCapability(w *explanationWriter, report *output.Report, id string) {
	c, found := report.Capabilities[id]
	if !found {
		w.linef("%s: unknown; this predicate was not evaluated for the selected target.", explanationText(id))
		return
	}
	w.linef("%s: %s (%s)", explanationText(id), explanationText(c.State), explanationText(c.Confidence))
	if c.State == unknownValue || c.State == "unavailable" {
		w.linef("  Missing complete usable evidence; this is not a definite prerequisite failure.")
	}
	selected, superseded := map[string]bool{}, map[string]bool{}
	for _, ref := range c.Evidence {
		selected[ref] = true
	}
	for _, ref := range c.Superseded {
		superseded[ref] = true
	}
	if w.observations == nil {
		w.observations = map[string]bool{}
	}
	for _, evidence := range report.Evaluation.Evidence {
		if evidence.Claim != id {
			continue
		}
		disposition := "excluded evidence"
		if selected[evidence.ID] {
			disposition = "selected evidence"
		} else if superseded[evidence.ID] {
			disposition = "superseded evidence"
		}
		w.linef("  %s %s: rank=%s state=%s completeness=%s", disposition, explanationText(evidence.ID),
			explanationText(evidence.Precedence), explanationText(evidence.State), explanationText(evidence.Completeness))
		for _, ref := range evidence.Observations {
			if w.observations[ref] {
				w.linef("    observation %s (details above)", explanationText(ref))
				continue
			}
			w.observations[ref] = true
			for _, observation := range report.Evaluation.Observations {
				if observation.ID == ref {
					explainObservation(w, observation)
				}
			}
		}
	}
}

func explainRuntime(w *explanationWriter, report *output.Report) {
	if runtime, ok := report.Runtimes[report.Evaluation.Scope.Runtime]; ok {
		backend := runtime.NetworkBackend
		if backend == "" {
			backend = "unmeasured"
		}
		w.linef("Runtime inspection: accessible=%s backend=%s rootless_command=%s cgroup_manager=%s",
			explanationBool(runtime.Accessible), explanationText(backend),
			explanationOptional(runtime.RootlessNetworkCmd), explanationOptional(runtime.CgroupManager))
	}
}

func explanationOptional(value *string) string {
	if value == nil {
		return "unmeasured"
	}
	return explanationText(*value)
}

func explainObservation(w *explanationWriter, obs output.ObservationRecord) {
	w.linef("    observation %s: %s", explanationText(obs.ID), explanationText(obs.Completeness))
	for _, diagnostic := range obs.Diagnostics {
		w.linef("      diagnostic: %s", explanationText(diagnostic.Code))
	}
	if c := obs.Configuration; c != nil {
		explainConfiguration(w, c)
	}
	if h := obs.Host; h != nil {
		if systemd := h.Systemd; systemd != nil {
			w.linef("      system manager: running=%s installed=%s accessible=%s", explanationBool(systemd.Running),
				explanationBool(systemd.Installed), explanationBool(systemd.Accessible))
		}
		if cgroup := h.Cgroups; cgroup != nil {
			w.linef("      cgroup mode=%s; controller delegation remains unverified", explanationText(cgroup.Mode))
		}
	}
	if x := obs.Executable; x != nil {
		w.linef("      helper %s: selected=%s source=%s", explanationText(x.Role), explanationText(x.SelectedPath), explanationText(x.SourceID))
		for _, candidate := range x.Candidates {
			w.linef("        %s present=%s executable_access=%s masked=%t", explanationText(candidate.Path),
				explanationBool(candidate.Present), explanationBool(candidate.Executable), candidate.Masked)
			if file := candidate.File; file != nil {
				w.linef("          regular=%t executable_bits=%t mode=%04o", file.Regular, file.ExecutableBits, file.Mode)
			}
		}
	}
	if user := obs.UserContext; user != nil {
		w.linef("      user manager: queried=%t accessible=%s; linger=%s", user.QueryAttempted,
			explanationBool(user.Accessible), explanationBool(user.LingerEnabled))
		w.linef("      runtime directory %s: present=%s valid=%s", explanationText(user.Runtime.Path),
			explanationBool(user.Runtime.Present), explanationBool(user.Runtime.Valid))
	}
	if storage := obs.StoragePaths; storage != nil {
		for _, metadata := range storage.Paths {
			w.linef("      storage %s %s: present=%s accessible=%s checked=%s ancestor=%t", explanationText(metadata.Role),
				explanationText(metadata.Path), explanationBool(metadata.Present), explanationBool(metadata.Accessible),
				explanationText(metadata.CheckedPath), metadata.Ancestor)
		}
	}
}
