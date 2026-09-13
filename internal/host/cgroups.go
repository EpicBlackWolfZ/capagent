package host

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const cgroupV1Magic = 0x27e0eb
const cgroupV2Magic = 0x63677270

type CgroupProbe struct{ Now func() time.Time }

func (CgroupProbe) ID() string             { return "host.cgroups" }
func (CgroupProbe) Dependencies() []string { return nil }
func (p CgroupProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := newHostObservation(p.ID(), env.Scope(), p.Now)
	state := &model.CgroupObservation{Mode: "unknown", Memberships: []model.CgroupMembership{}, Mounts: []model.CgroupMount{}}
	obs.Host.Cgroups = state
	proc := platform.NewHostProcfsReader(env.Files())
	mounts, mountErr := proc.Mounts(ctx)
	recordSource(&obs, "proc/self/mountinfo", mountErr)
	groups, groupErr := proc.Cgroups(ctx)
	recordSource(&obs, "proc/self/cgroup", groupErr)
	for _, group := range groups {
		if !validHostText(group.Path) {
			finishSource(&obs, platform.ErrMalformed)
			continue
		}
		state.Memberships = append(state.Memberships, model.CgroupMembership{Hierarchy: group.HierarchyID,
			Controllers: splitNonempty(group.Controllers, ","), Path: group.Path})
	}
	var v1, v2 bool
	topologyErr := mountErr
	for _, mount := range mounts {
		if mount.FSType != "cgroup" && mount.FSType != "cgroup2" {
			continue
		}
		path, pathErr := safeMountPath(mount.MountPoint)
		root, rootErr := safeMountPath(mount.Root)
		if pathErr != nil || rootErr != nil {
			topologyErr = platform.ErrMalformed
			finishSource(&obs, topologyErr)
			continue
		}
		info, statErr := env.Files().StatFS(ctx, relativeHostPath(path))
		recordSource(&obs, "statfs.cgroup_mount", statErr)
		expected := int64(cgroupV1Magic)
		if mount.FSType == "cgroup2" {
			expected = cgroupV2Magic
		}
		if statErr != nil || info.Type != expected {
			topologyErr = errors.Join(statErr, platform.ErrIncomplete)
			finishSource(&obs, topologyErr)
			continue
		}
		observed := model.CgroupMount{Path: path, Root: root, Type: mount.FSType, Controllers: []string{}, SubtreeControl: []string{}}
		if mount.FSType == "cgroup" {
			v1 = true
		} else {
			v2 = true
			readCurrentControllers(ctx, env, &obs, &observed, groups)
		}
		state.Mounts = append(state.Mounts, observed)
	}
	if topologyErr == nil {
		switch {
		case v1 && v2:
			state.Mode = "mixed"
		case v1:
			state.Mode = "v1"
		case v2:
			state.Mode = "v2"
		default:
			state.Mode = "unavailable"
		}
	}
	// Memberships contradicting the visible topology are not silently reconciled.
	for _, group := range groups {
		if (group.IsUnified() && !v2) || (!group.IsUnified() && !v1) {
			state.Mode = "unknown"
			finishSource(&obs, platform.ErrIncomplete)
		}
	}
	return obs, ctx.Err()
}

func safeMountPath(raw string) (string, error) {
	decoded, err := platform.DecodeMountPath(raw)
	if err != nil || !strings.HasPrefix(decoded, "/") || !validHostText(decoded) ||
		platform.ValidateSubpath(relativeHostPath(decoded)) != nil {
		return "", platform.ErrMalformed
	}
	return decoded, nil
}
func relativeHostPath(path string) string {
	if path == "/" {
		return "."
	}
	return strings.TrimPrefix(path, "/")
}
func splitNonempty(text, separator string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(text, separator)
}
func readCurrentControllers(ctx context.Context, env platform.Environment, obs *model.Observation,
	mount *model.CgroupMount, groups []platform.CgroupEntry) {
	for _, group := range groups {
		if !group.IsUnified() {
			continue
		}
		if platform.ValidateSubpath(relativeHostPath(group.Path)) != nil {
			finishSource(obs, platform.ErrMalformed)
			return
		}
		suffix := group.Path
		if mount.Root != "/" {
			if group.Path != mount.Root && !strings.HasPrefix(group.Path, mount.Root+"/") {
				continue
			}
			suffix = strings.TrimPrefix(group.Path, mount.Root)
		}
		mount.ControllerPath = strings.TrimSuffix(mount.Path, "/") + "/" + strings.TrimPrefix(suffix, "/")
		for _, field := range []string{"cgroup.controllers", "cgroup.subtree_control"} {
			source := relativeHostPath(strings.TrimSuffix(mount.ControllerPath, "/") + "/" + field)
			data, err := env.Files().ReadFile(ctx, source)
			recordSource(obs, source, err)
			values, parseErr := platform.ParseControllerList(ctx, data, err)
			finishSource(obs, parseErr)
			if field == "cgroup.controllers" {
				mount.Controllers = values
			} else {
				mount.SubtreeControl = values
			}
		}
		return
	}
	finishSource(obs, platform.ErrIncomplete)
}
