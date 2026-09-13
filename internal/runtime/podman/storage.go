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

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/knowledge"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const (
	configStatusAbsent = "absent"
	StorageConfigID    = "podman.configuration.storage"
	storageVendorPath  = "/usr/share/containers/storage.conf"
	storageSystemPath  = "/etc/containers/storage.conf"
)

type StorageProbe struct {
	Selection EngineProbe
	Engine    model.Observation
}

func (StorageProbe) ID() string             { return StorageConfigID }
func (StorageProbe) Dependencies() []string { return nil }

func (p StorageProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	selection := p.Selection
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(selection.Now))
	profile := selection.profile(env.Scope())
	c := &model.ConfigurationObservation{Family: "storage", RuntimePath: selection.Path, Profile: profile,
		SelectionComplete: profile != knowledge.ContainersUnqualified, ParseComplete: true, Sources: []model.ConfigurationSource{},
		References: storageReferences(profile)}
	obs.Configuration = c
	if c.SelectionComplete {
		c.VersionSourceID = selection.Version.ID
	}
	policy, err := TargetEnvironment(selection.Target, selection.Environment)
	if selection.EnvironmentError != nil || err != nil || env.Files() == nil {
		c.SelectionComplete, c.ParseComplete = false, false
		return failedRuntimeObservation(obs, "config_environment_unavailable", platform.ErrIncomplete)
	}
	values := directoryValues(policy)
	userHome := values["XDG_CONFIG_HOME"]
	if userHome == "" {
		userHome = path.Join(values["HOME"], ".config")
	}
	userPath := path.Join(userHome, "containers/storage.conf")
	collector := storageCollector{ctx: ctx, files: env.Files(), obs: &obs, complete: true}
	if !c.SelectionComplete || !p.engineBound(env.Scope()) {
		c.SelectionComplete = false
		for _, name := range []string{storageVendorPath, storageSystemPath, userPath} {
			collector.read(name, "candidate", false)
		}
		obs, _ = failedRuntimeObservation(obs, "storage_selection_unqualified", platform.ErrIncomplete)
		return obs, ctx.Err()
	}
	collector.selectStorage(selection.Target.UID, values, userPath)
	if !collector.complete || ctx.Err() != nil {
		obs, _ = failedRuntimeObservation(obs, "config_evidence_incomplete", platform.ErrIncomplete)
	}
	return obs, ctx.Err()
}

func (p StorageProbe) engineBound(scope model.EvaluationScope) bool {
	c := p.Engine.Configuration
	return p.Engine.Scope == scope && p.Engine.Completeness == model.Complete && c != nil && c.Engine != nil &&
		c.ParseComplete && c.SelectionComplete &&
		c.Family == "engine" && c.Profile == p.Selection.profile(scope) &&
		c.RuntimePath == p.Selection.Path && c.VersionSourceID == p.Selection.Version.ID &&
		(c.Engine.Environment == nil || c.Engine.Environment.Count == 0 && !c.Engine.Environment.InheritedDefault)
}

func storageReferences(profile string) []string {
	if profile == knowledge.ContainersUnqualified {
		return append(storageReferences(knowledge.Containers493), storageReferences(knowledge.Containers584)...)
	}
	prefix := "https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/storage/"
	if profile == knowledge.Containers493 {
		prefix = "https://github.com/containers/podman/blob/v4.9.3/vendor/github.com/containers/storage/"
	}
	return []string{prefix + "types/options.go", prefix + "types/utils.go", prefix + "pkg/config/config.go"}
}

type storageCollector struct {
	ctx      context.Context
	files    platform.ScopedView
	obs      *model.Observation
	complete bool
}

func (c *storageCollector) source(name, phase string, selected bool) model.ConfigurationSource {
	n := len(c.obs.Configuration.Sources)
	return model.ConfigurationSource{ID: StorageConfigID + ".source." + strconv.Itoa(n), Path: name, Kind: "file",
		Phase: phase, Order: n, Selected: selected}
}
func (c *storageCollector) exists(name string) bool {
	source := c.source(name, "selection", false)
	if c.ctx.Err() != nil {
		source.Status = c.failure(c.ctx.Err())
	} else {
		_, err := c.files.Stat(strings.TrimPrefix(name, "/"))
		switch {
		case err == nil:
			source.Status = "available"
		case errors.Is(err, fs.ErrNotExist):
			source.Status = configStatusAbsent
		default:
			source.Status = c.failure(err)
			c.obs.Configuration.SelectionComplete = false
		}
	}
	c.obs.Configuration.Sources = append(c.obs.Configuration.Sources, source)
	return source.Status == "available"
}
func (c *storageCollector) failure(err error) string {
	c.complete = false
	c.obs.Configuration.ParseComplete = false
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "denied"
	case errors.Is(err, config.ErrConfigLimit):
		return "limit"
	case errors.Is(err, config.ErrConfigFieldUnsupported), errors.Is(err, config.ErrConfigSyntaxUnsupported):
		return "unsupported"
	default:
		return "incomplete"
	}
}
func (c *storageCollector) read(name, phase string, selected bool) *model.StorageConfiguration {
	source := c.source(name, phase, selected)
	var projection *model.StorageConfiguration
	data, err := c.files.ReadFile(c.ctx, strings.TrimPrefix(name, "/"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		source.Status = configStatusAbsent
		projection = &model.StorageConfiguration{Options: map[string]model.ConfigString{}}
	case err != nil:
		source.Status = c.failure(err)
	default:
		digest := sha256.Sum256(data)
		source.SHA256 = hex.EncodeToString(digest[:])
		fields := config.StorageFields{Composefs: c.obs.Configuration.Profile != knowledge.Containers493}
		layer, parseErr := config.ParseStorageForFields(data, fields)
		if parseErr != nil {
			source.Problem = parseErr.Error()
			var field *config.FieldError
			if errors.As(parseErr, &field) {
				source.Field = field.Field
			}
			c.obs.Configuration.ParseComplete = false
			switch {
			case errors.Is(parseErr, config.ErrConfigMalformed):
				source.Status = "malformed"
			case errors.Is(parseErr, config.ErrConfigFieldInvalid):
				source.Status = "invalid"
			default:
				source.Status = c.failure(parseErr)
			}
		} else {
			source.Status = "parsed"
			value := config.ProjectStorage(layer, source.ID)
			source.Storage = &value
			// Reproject to keep the observed bytes distinct from subsequent root defaults.
			effective := config.ProjectStorage(layer, source.ID)
			projection = &effective
			source.Applied = selected && c.obs.Configuration.ParseComplete && c.obs.Configuration.SelectionComplete
		}
	}
	c.obs.Configuration.Sources = append(c.obs.Configuration.Sources, source)
	if source.Status != "parsed" && source.Status != configStatusAbsent {
		c.obs.Diagnostics = append(c.obs.Diagnostics, model.Diagnostic{Code: "config_source_" + source.Status,
			Message: "storage configuration source is " + source.Status, Reference: source.ID})
	}
	return projection
}

func nonemptyConfig(value *model.ConfigString) bool { return value != nil && value.Value != "" }
func storageFallback(value *model.ConfigString, fallback, source string) *model.ConfigString {
	if nonemptyConfig(value) {
		return value
	}
	return &model.ConfigString{Value: fallback, SourceID: source}
}
func storageFallbackValue(value, fallback *model.ConfigString) *model.ConfigString {
	if nonemptyConfig(value) {
		return value
	}
	return fallback
}
func rootlessStorage(system model.StorageConfiguration, values map[string]string, source string) model.StorageConfiguration {
	data := values["XDG_DATA_HOME"]
	if data == "" {
		data = path.Join(values["HOME"], ".local/share")
	}
	result := model.StorageConfiguration{GraphRoot: storageFallback(system.RootlessStoragePath, path.Join(data, "containers/storage"), source),
		RootlessStoragePath: system.RootlessStoragePath, Options: map[string]model.ConfigString{}}
	if runtime := values["XDG_RUNTIME_DIR"]; runtime != "" {
		result.RunRoot = &model.ConfigString{Value: path.Join(runtime, "containers"), SourceID: source}
	}
	if nonemptyConfig(system.Driver) {
		switch system.Driver.Value {
		case "overlay", "overlay2", "vfs", "btrfs":
			result.Driver = system.Driver
		}
	}
	if !nonemptyConfig(result.Driver) {
		result.DriverPriority = system.DriverPriority
	}
	if result.Driver != nil && (result.Driver.Value == "overlay" || result.Driver.Value == "overlay2") {
		for _, key := range []string{"ignore_chown_errors", "overlay.ignore_chown_errors"} {
			if value, ok := system.Options[key]; ok && value.Value != "" {
				result.Options["overlay.ignore_chown_errors"] = value
				break
			}
		}
	}
	return result
}
func storageMountProgram(storage model.StorageConfiguration) *model.ConfigString {
	if storage.Driver == nil || storage.Driver.Value != "overlay" && storage.Driver.Value != "overlay2" {
		return nil
	}
	for _, key := range []string{"overlay.mount_program", "mount_program"} {
		if value, ok := storage.Options[key]; ok && value.Value != "" {
			return &value
		}
	}
	return nil
}

func (c *storageCollector) selectStorage(uid uint32, values map[string]string, userPath string) {
	observation := c.obs.Configuration
	// DefaultConfigFile is called before default loading. Preserve its separate
	// decision: explicit XDG defaults do not replace this rootful system choice.
	finalPath := userPath
	systemExists := c.exists(storageSystemPath)
	if uid == 0 {
		finalPath = storageVendorPath
		if systemExists {
			finalPath = storageSystemPath
		}
	}
	baselinePath := storageVendorPath
	if systemExists {
		baselinePath = storageSystemPath
	}
	if values["XDG_CONFIG_HOME"] != "" && c.exists(userPath) {
		baselinePath = userPath
	}
	baseline := c.read(baselinePath, "baseline", true)
	if baseline != nil && observation.ParseComplete && observation.SelectionComplete {
		baseline.RunRoot = storageFallback(baseline.RunRoot, "/run/containers/storage", observation.VersionSourceID)
		baseline.GraphRoot = storageFallback(baseline.GraphRoot, "/var/lib/containers/storage", observation.VersionSourceID)
		selected := *baseline
		if uid != 0 {
			selected = rootlessStorage(*baseline, values, observation.VersionSourceID)
		}
		if c.exists(finalPath) {
			replacement := c.read(finalPath, "override", true)
			if replacement != nil {
				if uid != 0 || observation.Profile != knowledge.Containers493 {
					replacement.RunRoot = storageFallbackValue(replacement.RunRoot, selected.RunRoot)
					graphDefault := selected.GraphRoot
					if nonemptyConfig(replacement.RootlessStoragePath) {
						graphDefault = replacement.RootlessStoragePath
					}
					replacement.GraphRoot = storageFallbackValue(replacement.GraphRoot, graphDefault)
				}
				selected = *replacement
			}
		}
		if observation.ParseComplete && observation.SelectionComplete {
			if err := expandStorageRoots(&selected, values, uid); err != nil {
				observation.ParseComplete = false
				c.complete = false
				c.obs.Diagnostics = append(c.obs.Diagnostics, model.Diagnostic{Code: "storage_path_expansion_unsupported",
					Message: "storage roots require an unsupported path expansion"})
			} else {
				selected.MountProgram = storageMountProgram(selected)
				observation.Storage = &selected
			}
		}
	}

}
