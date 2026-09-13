package app

import (
	"maps"
	"slices"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func explainOtherConfigurationFindings(w *explanationWriter, report *output.Report, requested []string) {
	seen := map[string]bool{}
	for _, id := range requested {
		seen[id] = true
	}
	for _, id := range slices.Sorted(maps.Keys(report.Capabilities)) {
		state := report.Capabilities[id].State
		incomplete := strings.HasPrefix(id, "runtime.podman.config.") && (state == unknownValue || state == "unavailable")
		finding := state == "misconfigured" || incomplete
		if seen[id] || !finding {
			continue
		}
		w.linef("Additional finding outside this requirement:")
		explainCapability(w, report, id)
	}
}

func explainConfiguration(w *explanationWriter, c *model.ConfigurationObservation) {
	w.linef("      %s configuration: profile=%s selection_complete=%t parse_complete=%t",
		explanationText(c.Family), explanationText(c.Profile), c.SelectionComplete, c.ParseComplete)
	for _, source := range c.Sources {
		if source.Kind != "file" && (source.Status == "listed" || source.Status == "absent") {
			continue
		}
		w.linef("        configuration source #%d %s: status=%s selected=%t applied=%t", source.Order,
			explanationText(source.Path), explanationText(source.Status), source.Selected, source.Applied)
		if source.SHA256 != "" {
			w.linef("          sha256=%s source=%s", explanationText(source.SHA256), explanationText(source.ID))
		}
		if source.Field != "" || source.Problem != "" {
			w.linef("          field=%s problem=%s", explanationText(source.Field), explanationText(source.Problem))
		}
	}
	if e := c.Engine; e != nil {
		explainConfigValue(w, "engine.runtime", e.Runtime)
		explainConfigValue(w, "engine.cgroup_manager", e.CgroupManager)
		explainConfigList(w, "engine.helper_binaries_dir", e.HelperBinariesDir)
		if e.Environment != nil {
			w.linef("        engine environment: %d redacted entries; inherited_defaults=%t", e.Environment.Count, e.Environment.InheritedDefault)
		}
	}
	if n := c.Network; n != nil {
		for _, key := range slices.Sorted(maps.Keys(n.Strings)) {
			value := n.Strings[key]
			explainConfigValue(w, key, &value)
		}
		for _, key := range slices.Sorted(maps.Keys(n.Lists)) {
			value := n.Lists[key]
			explainConfigList(w, key, &value)
		}
		if n.PastaOptions != nil {
			w.linef("        pasta options: %d redacted entries; flag compatibility unverified", n.PastaOptions.Count)
		}
	}
	if s := c.Storage; s != nil {
		explainConfigValue(w, "storage.driver", s.Driver)
		explainConfigValue(w, "storage.graphroot", s.GraphRoot)
		explainConfigValue(w, "storage.runroot", s.RunRoot)
		explainConfigValue(w, "storage.mount_program", s.MountProgram)
		for _, key := range slices.Sorted(maps.Keys(s.Options)) {
			value := s.Options[key]
			explainConfigValue(w, key, &value)
		}
		for _, problem := range s.Problems {
			w.linef("        storage problem: %s", explanationText(problem))
		}
	}
}

func explainConfigValue(w *explanationWriter, key string, value *model.ConfigString) {
	if value == nil {
		w.linef("        %s: unmeasured default", explanationText(key))
		return
	}
	if value.Invalid {
		w.linef("        %s: invalid selected value (redacted); source=%s", explanationText(key), explanationText(value.SourceID))
		return
	}
	w.linef("        %s=%s; source=%s", explanationText(key), explanationText(value.Value), explanationText(value.SourceID))
}

func explainConfigList(w *explanationWriter, key string, value *model.ConfigList) {
	if value == nil {
		w.linef("        %s: unmeasured defaults", explanationText(key))
		return
	}
	w.linef("        %s: %d values; inherited_defaults=%t invalid=%d unmodeled=%d; source=%s", explanationText(key),
		len(value.Values), value.InheritedDefault, len(value.InvalidIndices), len(value.UnmodeledIndices), explanationText(value.SourceID))
}
