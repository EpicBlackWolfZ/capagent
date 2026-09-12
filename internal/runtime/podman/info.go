// Package podman parses Podman observations. The M1.2 application supplies only
// fixture services; it does not register a live runtime interrogation path.
package podman

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/config"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const maxInfoBytes = 1 << 20
const maxInfoDepth = 32
const executableBits = 0o111

type infoDocument struct {
	Version struct{ Version string }
	Host    struct {
		NetworkBackend     *string `json:"networkBackend"`
		NetworkBackendInfo struct {
			Path string `json:"path"`
		} `json:"networkBackendInfo"`
		CgroupVersion *string `json:"cgroupVersion"`
		CgroupManager *string `json:"cgroupManager"`
		Security      struct {
			Rootless *bool `json:"rootless"`
		} `json:"security"`
	} `json:"host"`
	Store struct {
		GraphDriverName *string `json:"graphDriverName"`
	} `json:"store"`
}

func ParseInfo(data []byte) (model.PodmanInfo, error) {
	if err := config.CheckJSON(data, maxInfoBytes, maxInfoDepth); err != nil {
		return model.PodmanInfo{}, err
	}
	var document *infoDocument
	if err := json.Unmarshal(data, &document, json.MatchCaseInsensitiveNames(true)); err != nil || document == nil {
		return model.PodmanInfo{}, errors.New("invalid Podman info document")
	}
	return model.PodmanInfo{Version: document.Version.Version, NetworkBackend: document.Host.NetworkBackend,
		HelperPath: document.Host.NetworkBackendInfo.Path, CgroupVersion: document.Host.CgroupVersion,
		CgroupManager: document.Host.CgroupManager, Rootless: document.Host.Security.Rootless,
		StorageDriver: document.Store.GraphDriverName}, nil
}

// InfoProbe accepts explicit services and command policy. Runtime initialization
// is not inherently passive: live application wiring requires the separate
// opt-in delivery work. Timestamp is injected by the evaluation owner.
type InfoProbe struct {
	Command   platform.CommandSpec
	Timestamp time.Time
}

func (InfoProbe) ID() string             { return "podman.info" }
func (InfoProbe) Dependencies() []string { return nil }

func (p InfoProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := model.Observation{ID: p.ID(), ProbeID: p.ID(), Scope: env.Scope(), Timestamp: p.Timestamp, Completeness: model.Complete,
		Summary: "Podman effective network backend prerequisites"}
	if env.Runner() == nil {
		return obs, errors.New("missing Podman command service")
	}
	result, runErr := env.Runner().Run(ctx, p.Command)
	obs.Facts = []model.Fact{{ID: p.ID() + ".stdout", Source: p.ID(), RawData: result.Stdout, Scope: env.Scope(),
		Timestamp: p.Timestamp, Completeness: model.Complete}}
	if result.TimedOut || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) ||
		result.StdoutTruncated || result.StderrTruncated {
		obs.Facts[0].Completeness = model.Partial
		obs = incompleteObservation(obs, "incomplete_command")
	}
	if runErr != nil || result.ExitCode != 0 {
		available := false
		obs.Podman = &model.PodmanInfo{Available: &available}
		obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: "runtime_unavailable", Message: "runtime inspection did not succeed"})
		if runErr == nil {
			runErr = errors.New("runtime inspection exited unsuccessfully")
		}
		return obs, runErr
	}
	payload, err := ParseInfo(result.Stdout)
	if err != nil {
		return incompleteObservation(obs, "invalid_runtime_json"), err
	}
	available := true
	payload.Available = &available
	obs.Podman = &payload
	if payload.NetworkBackend == nil || *payload.NetworkBackend == "" {
		return incompleteObservation(obs, "missing_network_backend"), nil
	}
	if *payload.NetworkBackend != "netavark" {
		return obs, nil
	}
	return observeHelper(ctx, env, obs)
}

func observeHelper(ctx context.Context, env platform.Environment, obs model.Observation) (model.Observation, error) {
	path := obs.Podman.HelperPath
	if !strings.HasPrefix(path, "/") || env.Files() == nil {
		return incompleteObservation(obs, "missing_helper_path"), nil
	}
	if err := ctx.Err(); err != nil {
		return incompleteObservation(obs, "cancelled"), err
	}
	info, err := env.Files().Stat(strings.TrimPrefix(path, "/"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return incompleteObservation(obs, "helper_unreadable"), err
	}
	present := err == nil && info.Mode().IsRegular() && info.Mode().Perm()&executableBits != 0
	obs.Podman.HelperPresent = &present
	text := "absent or not executable"
	if present {
		text = "regular executable metadata observed"
	}
	obs.Facts = append(obs.Facts, model.Fact{ID: obs.ID + ".helper", Scope: obs.Scope, Source: "podman.network-helper",
		Text: text, Timestamp: obs.Timestamp, Completeness: model.Complete})
	return obs, nil
}

func incompleteObservation(obs model.Observation, code string) model.Observation {
	obs.Completeness = model.Partial
	obs.Diagnostics = append(obs.Diagnostics, model.Diagnostic{Code: code, Message: "runtime observation is incomplete"})
	return obs
}
