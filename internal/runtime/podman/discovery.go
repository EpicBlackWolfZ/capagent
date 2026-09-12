package podman

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

type DiscoveryProbe struct {
	Path string
	Now  func() time.Time
}

func (DiscoveryProbe) ID() string             { return "podman.discovery" }
func (DiscoveryProbe) Dependencies() []string { return nil }

func ValidateExecutablePath(value string) error {
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "/") || path.Clean(value) != value ||
		platform.ValidateSubpath(strings.TrimPrefix(value, "/")) != nil ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 ||
		strings.TrimPrefix(path.Base(value), "-") == "podmansh" {
		return errors.New("invalid Podman executable path")
	}
	return nil
}

func (p DiscoveryProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(p.Now))
	obs.Discovery = &model.RuntimeDiscovery{}
	if err := ValidateExecutablePath(p.Path); err != nil {
		return failedRuntimeObservation(obs, "invalid_executable_path", err)
	}
	if env.Files() == nil {
		return failedRuntimeObservation(obs, "missing_file_service", errors.New("missing scoped file service"))
	}
	paths := []string{"/usr/bin/podman", "/usr/local/bin/podman", "/bin/podman"}
	if p.Path != "" {
		paths = []string{p.Path}
		obs.Discovery.Path = p.Path
	}
	uncertain := false
	for index, candidate := range paths {
		if err := ctx.Err(); err != nil {
			return failedRuntimeObservation(obs, "discovery_cancelled", err)
		}
		info, err := env.Files().Stat(strings.TrimPrefix(candidate, "/"))
		obs.Facts = append(obs.Facts, model.Fact{ID: obs.ID + ".path" + strconv.Itoa(index), Scope: obs.Scope,
			Timestamp: obs.Timestamp, Source: "filesystem.stat", Text: candidate, Completeness: model.Complete})
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			uncertain = true
			obs = incompleteObservation(obs, "executable_lookup_unavailable")
			obs.Facts[len(obs.Facts)-1].Completeness = model.Partial
			continue
		}
		obs.Discovery.Path = candidate
		metadata := &model.ExecutableMetadata{Regular: info.Mode().IsRegular(),
			ExecutableBits: info.Mode().Perm()&executableBits != 0, Mode: uint32(info.Mode().Perm())}
		if owner, known := platform.OwnershipOf(info); known {
			metadata.UID, metadata.GID = &owner.UID, &owner.GID
		}
		obs.Discovery.File = metadata
		if !metadata.Regular {
			return incompleteObservation(obs, "executable_not_regular"), nil
		}
		present := true
		obs.Discovery.Installed = &present
		if !metadata.ExecutableBits {
			obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: "executable_bits_missing",
				Message: "selected file has no executable permission bits"})
		}
		return obs, nil
	}
	if !uncertain {
		absent := false
		obs.Discovery.Installed = &absent
		obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: "podman_not_found", Message: "no selected Podman executable found"})
	}
	return obs, nil
}

func measurementTime(now func() time.Time) time.Time {
	if now == nil {
		return time.Now()
	}
	return now()
}

func runtimeObservation(id string, scope model.EvaluationScope, at time.Time) model.Observation {
	return model.Observation{ID: id, ProbeID: id, Scope: scope, Timestamp: at, Completeness: model.Complete,
		Facts: []model.Fact{{ID: id + ".measurement", Scope: scope, Timestamp: at, Source: id, Completeness: model.Complete}}}
}

func failedRuntimeObservation(obs model.Observation, code string, err error) (model.Observation, error) {
	obs.Facts[0].Completeness = model.Partial
	return incompleteObservation(obs, code), err
}
