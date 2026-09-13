package app

import (
	"slices"
	"strings"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/output"
)

func projectRuntime(observations []model.Observation, scope model.EvaluationScope) (output.RuntimeInfo, []output.Diagnostic) {
	ordered := slices.Clone(observations)
	slices.SortFunc(ordered, func(a, b model.Observation) int {
		if order := a.Timestamp.Compare(b.Timestamp); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	runtime := output.RuntimeInfo{Completeness: string(model.Unobserved)}
	var discovery *model.RuntimeDiscovery
	var version *model.PodmanVersionObservation
	var info *model.PodmanInfo
	for _, obs := range ordered {
		if obs.Scope != scope || (obs.Discovery == nil && obs.Version == nil && obs.Podman == nil) {
			continue
		}
		if runtime.Completeness == string(model.Unobserved) {
			runtime.Completeness = string(model.Complete)
		}
		if obs.Completeness != model.Complete {
			runtime.Completeness = string(model.Partial)
		}
		if obs.Discovery != nil {
			discovery = obs.Discovery
		}
		if obs.Version != nil {
			version = obs.Version
		}
		if obs.Podman != nil {
			info = obs.Podman
		}
	}
	if discovery != nil {
		runtime.Installed, runtime.Path = copyValue(discovery.Installed), discovery.Path
		if f := discovery.File; f != nil {
			runtime.File = &output.ExecutableInfo{Regular: f.Regular, ExecutableBits: f.ExecutableBits, Mode: f.Mode,
				UID: copyValue(f.UID), GID: copyValue(f.GID)}
		}
	}
	if info != nil {
		runtime.Accessible, runtime.Version, runtime.Path = copyValue(info.Available), info.Version, info.Path
		runtime.Rootless, runtime.CgroupVersion = copyValue(info.Rootless), copyValue(info.CgroupVersion)
		runtime.CgroupManager = copyValue(info.CgroupManager)
		runtime.GraphRoot, runtime.RunRoot = copyValue(info.GraphRoot), copyValue(info.RunRoot)
		if info.NetworkBackend != nil {
			runtime.NetworkBackend = *info.NetworkBackend
		}
		if info.StorageDriver != nil {
			runtime.StorageDriver = *info.StorageDriver
		}
	}
	if version != nil {
		runtime.CLIRunnable, runtime.Path = copyValue(version.Runnable), version.Path
		if v := version.Version; v != nil {
			runtime.Version = v.Canonical
			runtime.VersionDetails = &output.VersionInfo{Major: v.Major, Minor: v.Minor, Patch: v.Patch,
				Canonical: v.Canonical, Suffix: v.Suffix, Build: v.Build, Trailing: v.Trailing, Raw: v.Raw}
		}
	}
	if info != nil && version != nil && versionsConflict(info.VersionParts, version.Version) {
		runtime.Completeness = string(model.Partial)
		return runtime, []output.Diagnostic{{Code: "runtime_version_conflict", Message: "version and info observations disagree"}}
	}
	return runtime, nil
}

func versionsConflict(a, b *model.PodmanVersion) bool {
	return a != nil && b != nil && (a.Canonical != b.Canonical || a.Suffix != b.Suffix || a.Build != b.Build)
}
