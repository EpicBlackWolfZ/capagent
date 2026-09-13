package podman

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/knowledge"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const (
	EngineConfigID      = "podman.configuration.engine"
	MaxConfigSources    = 64
	MaxConfigTotalBytes = 1024 * 1024
	engineSystemConfig  = "/etc/containers/containers.conf"
)

type EngineProbe struct {
	Target           model.UserIdentity
	Environment      platform.EnvPolicy
	EnvironmentError error
	Version          model.Observation
	Path             string
	Now              func() time.Time
}

func (EngineProbe) ID() string             { return EngineConfigID }
func (EngineProbe) Dependencies() []string { return nil }

func (p EngineProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(p.Now))
	profile := p.profile(env.Scope())
	c := &model.ConfigurationObservation{Family: "engine", RuntimePath: p.Path, Profile: profile,
		References: []string{knowledge.Containers493Source, knowledge.Containers584Source}, Sources: []model.ConfigurationSource{},
		SelectionComplete: profile != knowledge.ContainersUnqualified, ParseComplete: true}
	obs.Configuration = c
	if c.SelectionComplete {
		c.VersionSourceID = p.Version.ID
		c.Engine = &model.EngineConfiguration{Runtimes: map[string]model.ConfigList{}}
	}
	if p.EnvironmentError != nil || env.Files() == nil {
		c.SelectionComplete, c.ParseComplete = false, false
		return failedRuntimeObservation(obs, "config_environment_unavailable", platform.ErrIncomplete)
	}
	policy, err := TargetEnvironment(p.Target, p.Environment)
	if err != nil {
		c.SelectionComplete, c.ParseComplete = false, false
		return failedRuntimeObservation(obs, "config_environment_unavailable", err)
	}
	values := directoryValues(policy)
	configHome := values["XDG_CONFIG_HOME"]
	if configHome == "" {
		configHome = values["HOME"] + "/.config"
	}
	collector := engineCollector{ctx: ctx, files: env.Files(), observation: &obs, complete: true, applicable: c.SelectionComplete}
	for _, source := range engineSourcePlan(profile, p.Target.UID, configHome) {
		if ctx.Err() != nil || collector.limit {
			c.SelectionComplete, c.ParseComplete = false, false
			collector.complete = false
			break
		}
		if source.directory {
			collector.directory(source.name)
		} else {
			collector.file(source.name, profile == knowledge.Containers493)
		}
	}
	if !collector.complete || profile == knowledge.ContainersUnqualified {
		obs, _ = failedRuntimeObservation(obs, "config_evidence_incomplete", platform.ErrIncomplete)
	}
	return obs, ctx.Err()
}

func (p EngineProbe) profile(scope model.EvaluationScope) string {
	v := p.Version.Version
	if p.Version.ID == "" || p.Version.Scope != scope || p.Version.Completeness != model.Complete || v == nil ||
		v.Path != p.Path || v.Runnable == nil || !*v.Runnable || v.Version == nil {
		return knowledge.ContainersUnqualified
	}
	return knowledge.ContainersSourceProfile(*v.Version)
}

type engineSource struct {
	name      string
	directory bool
}

// The two qualified upstream revisions differ in global rootless sources and
// rootful user configuration. An unqualified version gets candidate reads only.
func engineSourcePlan(profile string, uid uint32, configHome string) []engineSource {
	sources := []engineSource{{"/usr/share/containers/containers.conf", false}, {engineSystemConfig, false},
		{engineSystemConfig + ".d", true}}
	if profile != knowledge.Containers493 && uid != 0 {
		rootless := "/etc/containers/containers.rootless.conf"
		sources = append(sources, engineSource{rootless, false}, engineSource{rootless + ".d", true},
			engineSource{rootless + ".d/" + strconv.FormatUint(uint64(uid), 10), true})
	}
	if profile != knowledge.Containers493 || uid != 0 {
		user := path.Join(configHome, "containers/containers.conf")
		sources = append(sources, engineSource{user, false}, engineSource{user + ".d", true})
	}
	return sources
}

type engineCollector struct {
	ctx         context.Context
	files       platform.ScopedView
	observation *model.Observation
	fileCount   int
	totalBytes  int
	complete    bool
	applicable  bool
	limit       bool
}

func (c *engineCollector) source(name, kind string) model.ConfigurationSource {
	n := len(c.observation.Configuration.Sources)
	return model.ConfigurationSource{ID: EngineConfigID + ".source." + strconv.Itoa(n), Path: name,
		Kind: kind, Order: n, Selected: c.observation.Configuration.Profile != knowledge.ContainersUnqualified}
}

func (c *engineCollector) retain(source model.ConfigurationSource) {
	c.observation.Configuration.Sources = append(c.observation.Configuration.Sources, source)
	switch source.Status {
	case "parsed", "listed", configStatusAbsent, "symlink_skipped":
		return
	default:
		message := "selected configuration source is " + source.Status
		if source.Field != "" {
			message += " in " + source.Field
		}
		c.observation.Diagnostics = append(c.observation.Diagnostics, model.Diagnostic{
			Code: "config_source_" + source.Status, Message: message, Reference: source.ID})
	}
}

func (c *engineCollector) directory(name string) {
	source := c.source(name, "directory")
	if c.observation.Configuration.Profile == knowledge.Containers493 {
		_, err := c.files.Readlink(strings.TrimPrefix(name, "/"))
		if err == nil {
			source.Status = "symlink_skipped"
			c.retain(source)
			return
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.EINVAL) {
			source.Status = c.readFailure(err)
			c.observation.Configuration.SelectionComplete = false
			c.retain(source)
			return
		}
	}
	entries, err := c.files.ReadDir(c.ctx, strings.TrimPrefix(name, "/"))
	if errors.Is(err, fs.ErrNotExist) {
		source.Status = configStatusAbsent
		c.retain(source)
		return
	}
	if err != nil {
		source.Status = c.readFailure(err)
		c.observation.Configuration.SelectionComplete = false
		c.retain(source)
		return // Never promote a partial directory listing to a selected source set.
	}
	source.Status = "listed"
	c.retain(source)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".conf") {
			if !safeInfoPath(path.Join(name, entry.Name())) {
				c.observation.Configuration.SelectionComplete, c.observation.Configuration.ParseComplete = false, false
				c.complete, c.applicable = false, false
				c.observation.Diagnostics = append(c.observation.Diagnostics, model.Diagnostic{
					Code: "config_source_name_unsupported", Message: "drop-in directory contains an unsupported source name", Reference: source.ID})
				continue
			}
			c.file(path.Join(name, entry.Name()), false)
			if c.limit || c.ctx.Err() != nil {
				break
			}
		}
	}
}

func (c *engineCollector) file(name string, optionalStat bool) {
	source := c.source(name, "file")
	c.fileCount++
	if c.fileCount > MaxConfigSources {
		source.Status = c.readFailure(config.ErrConfigLimit)
		c.observation.Configuration.SelectionComplete = false
		c.limit = true
		c.retain(source)
		return
	}
	if optionalStat {
		if _, err := c.files.Stat(strings.TrimPrefix(name, "/")); err != nil {
			source.Selected, source.Status = false, configStatusAbsent
			if !errors.Is(err, fs.ErrNotExist) {
				// 4.9.3 skips optional main files after Stat errors. Preserve that
				// selection decision while retaining measurement uncertainty.
				source.Status, c.complete = "stat_incomplete", false
				if errors.Is(err, fs.ErrPermission) {
					source.Status = "stat_denied"
				}
			}
			c.retain(source)
			return
		}
	}
	data, err := c.files.ReadFile(c.ctx, strings.TrimPrefix(name, "/"))
	if errors.Is(err, fs.ErrNotExist) && c.observation.Configuration.Profile != knowledge.Containers493 {
		source.Status = configStatusAbsent
		c.retain(source)
		return
	}
	if err != nil {
		source.Status = c.readFailure(err)
		c.retain(source)
		return // Partial bytes are never parsed or hashed as a complete document.
	}
	c.totalBytes += len(data)
	if c.totalBytes > MaxConfigTotalBytes {
		source.Status = c.readFailure(config.ErrConfigLimit)
		c.observation.Configuration.SelectionComplete = false
		c.limit = true
		c.retain(source)
		return
	}
	digest := sha256.Sum256(data)
	source.SHA256 = hex.EncodeToString(digest[:])
	layer, err := config.ParseEngine(data)
	if err != nil {
		source.Problem = err.Error() // All config parser errors are fixed, redacted codes.
		var fieldError *config.FieldError
		if errors.As(err, &fieldError) {
			source.Field = fieldError.Field
		}
		c.observation.Configuration.ParseComplete, c.applicable = false, false
		switch {
		case errors.Is(err, config.ErrConfigMalformed):
			source.Status = "malformed"
		case errors.Is(err, config.ErrConfigFieldInvalid):
			source.Status = "invalid"
		default:
			source.Status = c.readFailure(err)
		}
	} else {
		source.Status = "parsed"
		projection := config.MergeEngine(model.EngineConfiguration{}, layer, source.ID)
		source.Engine = &projection
		if c.applicable {
			merged := config.MergeEngine(*c.observation.Configuration.Engine, layer, source.ID)
			c.observation.Configuration.Engine = &merged
			source.Applied = true
		}
	}
	c.retain(source)
}

func (c *engineCollector) readFailure(err error) string {
	c.complete, c.applicable, c.observation.Configuration.ParseComplete = false, false, false
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "denied"
	case errors.Is(err, config.ErrConfigFieldUnsupported), errors.Is(err, config.ErrConfigSyntaxUnsupported):
		return "unsupported"
	case errors.Is(err, config.ErrConfigLimit), errors.Is(err, platform.ErrLimitExceeded):
		return "limit"
	default:
		return "incomplete"
	}
}
