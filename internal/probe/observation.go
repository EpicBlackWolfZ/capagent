package probe

import (
	"errors"
	"slices"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

var ErrObservationScope = errors.New("probe returned a different evaluation scope")

// retainObservation snapshots all mutable payloads before publishing a result.
// Probes must not mutate a returned value concurrently with this transfer.
func retainObservation(obs model.Observation, scope model.EvaluationScope) (model.Observation, error) {
	obs = SnapshotObservation(obs)
	var mismatch bool
	if obs.Scope == (model.EvaluationScope{}) {
		obs.Scope = scope
	}
	mismatch = obs.Scope != scope
	for i := range obs.Facts {
		fact := &obs.Facts[i]
		if fact.Scope == (model.EvaluationScope{}) {
			fact.Scope = scope
		}
		mismatch = mismatch || fact.Scope != scope
	}
	if mismatch {
		return obs, ErrObservationScope
	}
	return obs, nil
}

// SnapshotObservation copies all mutable payloads for transfer to a run owner.
// The caller must not mutate inputs concurrently with the copy.
func SnapshotObservation(obs model.Observation) model.Observation {
	obs.Facts = slices.Clone(obs.Facts)
	obs.Diagnostics = slices.Clone(obs.Diagnostics)
	obs.Podman = copyValue(obs.Podman)
	if p := obs.Podman; p != nil {
		p.NetworkBackend = copyValue(p.NetworkBackend)
		p.StorageDriver = copyValue(p.StorageDriver)
		p.CgroupVersion = copyValue(p.CgroupVersion)
		p.CgroupManager = copyValue(p.CgroupManager)
		p.Rootless = copyValue(p.Rootless)
		p.HelperPresent = copyValue(p.HelperPresent)
		p.Available = copyValue(p.Available)
	}
	obs.Discovery = copyValue(obs.Discovery)
	if d := obs.Discovery; d != nil {
		d.Installed = copyValue(d.Installed)
		d.File = copyValue(d.File)
		if f := d.File; f != nil {
			f.UID, f.GID = copyValue(f.UID), copyValue(f.GID)
		}
	}
	obs.Version = copyValue(obs.Version)
	if v := obs.Version; v != nil {
		v.Runnable, v.Version = copyValue(v.Runnable), copyValue(v.Version)
	}
	for i := range obs.Facts {
		obs.Facts[i].RawData = slices.Clone(obs.Facts[i].RawData)
	}
	return obs
}

func copyValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
