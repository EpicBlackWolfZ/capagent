package podman

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

type ConfiguredHelperProbe struct {
	Source model.Observation
	Role   string
	Now    func() time.Time
}

func ConfiguredHelperProbes(source model.Observation, now func() time.Time) []ConfiguredHelperProbe {
	if source.Configuration == nil || source.Configuration.Family != "engine" {
		return nil
	}
	return []ConfiguredHelperProbe{{Source: source, Role: "oci_runtime", Now: now}, {Source: source, Role: conmonName, Now: now}}
}

func (p ConfiguredHelperProbe) ID() string           { return "podman.executable.configuration." + p.Role }
func (ConfiguredHelperProbe) Dependencies() []string { return nil }

func (p ConfiguredHelperProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(p.Now))
	x := &model.ExecutableObservation{Role: p.Role, Source: sourceConfiguration, SourceID: p.Source.ID,
		Candidates: []model.ExecutableCandidate{}}
	obs.Executable = x
	c := p.Source.Configuration
	if c != nil {
		x.RuntimePath = c.RuntimePath
	}
	if c == nil || c.Engine == nil || !c.SelectionComplete || !c.ParseComplete || p.Source.Scope != env.Scope() ||
		p.Source.Completeness != model.Complete || env.Files() == nil {
		return failedRuntimeObservation(obs, "configured_selection_incomplete", platform.ErrIncomplete)
	}
	if c.Engine.SelectionOverrides {
		return failedRuntimeObservation(obs, "configured_runtime_override_unmodeled", platform.ErrIncomplete)
	}
	if e := c.Engine.Environment; e != nil && (e.Count != 0 || e.InheritedDefault) {
		return failedRuntimeObservation(obs, "configured_environment_unmodeled", platform.ErrIncomplete)
	}
	paths, fallback, known := p.paths(c.Engine)
	if !known {
		return failedRuntimeObservation(obs, "configured_default_unobserved", platform.ErrIncomplete)
	}
	for _, name := range paths {
		stop := p.candidate(ctx, env.Files(), &obs, name, false)
		if stop {
			return obs, ctx.Err()
		}
	}
	for _, directory := range []string{"/usr/bin", "/bin"} {
		name := fallback
		if !path.IsAbs(name) {
			name = path.Join(directory, fallback)
		}
		if stop := p.candidate(ctx, env.Files(), &obs, name, true); stop {
			return obs, ctx.Err()
		}
		if path.IsAbs(fallback) {
			break
		}
	}
	return obs, ctx.Err()
}

func (p ConfiguredHelperProbe) paths(engine *model.EngineConfiguration) ([]string, string, bool) {
	if p.Role == conmonName {
		if engine.ConmonPath == nil || engine.ConmonPath.InheritedDefault {
			return nil, "", false
		}
		return engine.ConmonPath.Values, conmonName, true
	}
	if p.Role != "oci_runtime" || engine.Runtime == nil || engine.Runtime.Value == "" {
		return nil, "", false
	}
	name := engine.Runtime.Value
	if path.IsAbs(name) {
		return []string{name}, name, true
	}
	list, ok := engine.Runtimes[name]
	if !ok || list.InheritedDefault {
		return nil, "", false
	}
	return list.Values, name, true
}

func (p ConfiguredHelperProbe) candidate(ctx context.Context, files platform.ScopedView,
	obs *model.Observation, name string, fallback bool,
) bool {
	x := obs.Executable
	if len(x.Candidates) == maxExecutableCandidates || ctx.Err() != nil || !safeInfoPath(name) {
		*obs, _ = failedRuntimeObservation(*obs, "configured_candidates_incomplete", platform.ErrIncomplete)
		return true
	}
	info, err := files.Stat(strings.TrimPrefix(name, "/"))
	if err != nil {
		candidate := model.ExecutableCandidate{Path: name}
		if errors.Is(err, fs.ErrNotExist) {
			candidate.Present = boolPointer(false)
		} else {
			*obs, _ = failedRuntimeObservation(*obs, "configured_candidate_unreadable", err)
		}
		x.Candidates = append(x.Candidates, candidate)
		return !fallback && p.Role == "oci_runtime" && !errors.Is(err, fs.ErrNotExist)
	}
	candidate, accessErr := executableFromInfo(ctx, files, name, info, false)
	x.Candidates = append(x.Candidates, candidate)
	if accessErr != nil {
		*obs, _ = failedRuntimeObservation(*obs, "configured_candidate_access_unknown", accessErr)
	}

	if fallback && !info.IsDir() && !info.Mode().IsRegular() {
		// LookPath can select an executable special file. Our suitable-program
		// metadata does not measure that lookup access, so a later candidate
		// cannot be asserted as selected. Never execute or read the special file.
		*obs, _ = failedRuntimeObservation(*obs, "configured_special_file_selection_unknown", platform.ErrIncomplete)
		return true
	}
	// OCI candidate selection requires a regular file; conmon skips directories.
	// Both configured lists select before execution access is tested. PATH
	// fallback additionally needs executable access, matching exec.LookPath.
	selected := !info.IsDir() && (p.Role == conmonName || info.Mode().IsRegular())
	if fallback {
		selected = info.Mode().IsRegular() && candidate.Executable != nil && *candidate.Executable
	}
	if selected {
		x.SelectedPath = name
	}
	return selected
}
