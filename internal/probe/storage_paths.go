package probe

import (
	"slices"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func snapshotStoragePaths(input *model.StoragePathObservation) *model.StoragePathObservation {
	out := copyValue(input)
	if out == nil {
		return nil
	}
	out.Paths = slices.Clone(out.Paths)
	for i := range out.Paths {
		p := &out.Paths[i]
		p.Present, p.Directory, p.Accessible = copyValue(p.Present), copyValue(p.Directory), copyValue(p.Accessible)
		p.CheckedUID, p.CheckedGID, p.CheckedMode = copyValue(p.CheckedUID), copyValue(p.CheckedGID), copyValue(p.CheckedMode)
		p.Filesystem = copyValue(p.Filesystem)
		if p.Filesystem != nil {
			p.Filesystem.ReadOnly, p.Filesystem.NoSUID, p.Filesystem.NoExec = copyValue(p.Filesystem.ReadOnly),
				copyValue(p.Filesystem.NoSUID), copyValue(p.Filesystem.NoExec)
		}
	}
	return out
}
