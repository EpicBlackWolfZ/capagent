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

const (
	MaxStoragePaths     = 32
	maxStorageAncestors = 32
)

type StoragePathProbe struct {
	Source   model.Observation
	Writable bool
	Now      func() time.Time
}

func (p StoragePathProbe) ID() string {
	kind := sourceConfiguration
	if p.Source.Podman != nil {
		kind = sourceRuntime
	}
	group := "additional"
	if p.Writable {
		group = "roots"
	}
	return "podman.storage.paths." + kind + "." + group
}
func (StoragePathProbe) Dependencies() []string { return nil }

func (p StoragePathProbe) Run(ctx context.Context, env platform.Environment) (model.Observation, error) {
	obs := runtimeObservation(p.ID(), env.Scope(), measurementTime(p.Now))
	data := &model.StoragePathObservation{Source: sourceConfiguration,
		SourceID: p.Source.ID,
		Writable: p.Writable,
		Paths:    []model.StoragePathMetadata{}}
	obs.StoragePaths = data
	if err := ctx.Err(); err != nil {
		return failedRuntimeObservation(obs, "storage_path_metadata_incomplete", err)
	}
	names, known := p.paths(data)
	if !known || p.Source.Scope != env.Scope() || p.Source.Completeness != model.Complete || env.Files() == nil {
		return failedRuntimeObservation(obs, "storage_path_selection_incomplete", platform.ErrIncomplete)
	}
	if len(names) > MaxStoragePaths {
		names = names[:MaxStoragePaths]
		obs, _ = failedRuntimeObservation(obs, "storage_path_limit", platform.ErrIncomplete)
	}
	for _, name := range names {
		measured, err := observeStoragePath(ctx, env.Files(), name, p.Writable)
		data.Paths = append(data.Paths, measured)
		if err != nil {
			obs, _ = failedRuntimeObservation(obs, "storage_path_metadata_incomplete", err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return obs, ctx.Err()
}

func (p StoragePathProbe) paths(out *model.StoragePathObservation) ([]model.StoragePathMetadata, bool) {
	var roots [3]string
	var additional [2]*model.ConfigList
	if info := p.Source.Podman; info != nil {
		out.Source, out.RuntimePath = sourceRuntime, info.Path
		if !p.Writable || info.Available == nil || !*info.Available || info.ServiceIsRemote != nil && *info.ServiceIsRemote ||
			info.GraphRoot == nil || info.RunRoot == nil {
			return nil, false
		}
		roots[0], roots[1] = *info.GraphRoot, *info.RunRoot
	} else {
		c := p.Source.Configuration
		if c != nil {
			out.RuntimePath = c.RuntimePath
		}
		if c == nil || c.Family != "storage" || !c.SelectionComplete || !c.ParseComplete || c.Storage == nil {
			return nil, false
		}
		out.RuntimePath = c.RuntimePath
		for i, value := range []*model.ConfigString{c.Storage.GraphRoot, c.Storage.RunRoot, c.Storage.ImageStore} {
			if value != nil {
				roots[i] = value.Value
			}
		}
		additional = [2]*model.ConfigList{c.Storage.AdditionalImageStores, c.Storage.AdditionalLayerStores}
	}
	names := []model.StoragePathMetadata{}
	if p.Writable {
		for i, role := range []string{"graphroot", "runroot", "imagestore"} {
			if roots[i] != "" {
				names = append(names, model.StoragePathMetadata{Role: role, Path: roots[i]})
			}
		}
		return names, roots[0] != "" && roots[1] != ""
	}
	return additionalStoragePaths(additional), true
}

func additionalStoragePaths(additional [2]*model.ConfigList) []model.StoragePathMetadata {
	names := []model.StoragePathMetadata{}
	for i, list := range additional {
		if list == nil {
			continue
		}
		role := "additionalimagestore"
		if i != 0 {
			role = "additionallayerstore"
		}
		for _, value := range list.Values {
			for name := range strings.SplitSeq(value, ",") {
				if i != 0 {
					name = strings.TrimSuffix(name, ":ref")
				}
				names = append(names, model.StoragePathMetadata{Role: role, Path: path.Clean(name)})
				if len(names) > MaxStoragePaths {
					return names
				}
			}
		}
	}
	return names
}

func observeStoragePath(ctx context.Context,
	files platform.ScopedView,
	metadata model.StoragePathMetadata,
	writable bool) (model.StoragePathMetadata,
	error) {
	if !safeInfoPath(metadata.Path) {
		return metadata, platform.ErrMalformed
	}
	checked := metadata.Path
	var info fs.FileInfo
	for attempt := 0; attempt < maxStorageAncestors; attempt++ {
		if err := ctx.Err(); err != nil {
			return metadata, err
		}
		var err error
		info, err = files.Stat(strings.TrimPrefix(checked, "/"))
		if err == nil {
			if checked == metadata.Path {
				metadata.Present, metadata.Directory = boolPointer(true), boolPointer(info.IsDir())
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return metadata, err
		}
		metadata.Present = boolPointer(false)
		if checked == "/" {
			return metadata, platform.ErrIncomplete
		}
		checked = path.Dir(checked)
		metadata.Ancestor = true
	}
	if info == nil {
		return metadata, platform.ErrIncomplete
	}
	metadata.CheckedPath = checked
	mode := uint32(info.Mode().Perm())
	metadata.CheckedMode = &mode
	if owner, known := platform.OwnershipOf(info); known {
		metadata.CheckedUID, metadata.CheckedGID = &owner.UID, &owner.GID
	}
	if !info.IsDir() {
		metadata.Accessible = boolPointer(false)
		return metadata, nil
	}
	allowed, accessErr := files.DirectoryAccess(ctx, strings.TrimPrefix(checked, "/"), writable)
	if accessErr == nil {
		metadata.Accessible = &allowed
	}
	filesystem, fsErr := files.StatFS(ctx, strings.TrimPrefix(checked, "/"))
	if fsErr == nil {
		metadata.Filesystem = &model.StorageFilesystem{Type: filesystem.Type}
		if filesystem.FlagsKnown {
			metadata.Filesystem.ReadOnly,
				metadata.Filesystem.NoSUID,
				metadata.Filesystem.NoExec = &filesystem.ReadOnly,
				&filesystem.NoSUID,
				&filesystem.NoExec
		}
	}
	return metadata, errors.Join(accessErr, fsErr)
}
