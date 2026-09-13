package podman

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// ObserveHelper borrows the file service after info collection. It only stats
// the reported path and never executes the helper or tests container networking.
func ObserveHelper(ctx context.Context, env platform.Environment, info model.Observation, at time.Time) (model.Observation, error) {
	obs := runtimeObservation("podman.network-helper", env.Scope(), at)
	if info.Scope != env.Scope() || info.Podman == nil || info.Podman.Available == nil || !*info.Podman.Available {
		return failedRuntimeObservation(obs, "invalid_helper_source", errors.New("helper requires matching successful info"))
	}
	p := info.Podman
	obs.PodmanHelper = &model.PodmanHelper{Path: p.Path, InfoID: info.ID, HelperPath: p.HelperPath}
	if !safeInfoPath(p.HelperPath) || ValidateExecutablePath(p.HelperPath) != nil || env.Files() == nil {
		return failedRuntimeObservation(obs, "missing_helper_path", errors.New("missing valid helper path or file service"))
	}
	if err := ctx.Err(); err != nil {
		return failedRuntimeObservation(obs, "helper_cancelled", err)
	}
	metadata, err := env.Files().Stat(strings.TrimPrefix(p.HelperPath, "/"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return failedRuntimeObservation(obs, "helper_unreadable", err)
	}
	present := err == nil && metadata.Mode().IsRegular() && metadata.Mode().Perm()&executableBits != 0
	obs.PodmanHelper.Present = &present
	obs.Facts[0].Source = "podman.network-helper"
	obs.Facts[0].Text = "absent or not executable"
	if present {
		obs.Facts[0].Text = "regular executable metadata observed"
	}
	return obs, nil
}
