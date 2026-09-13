package podman

import (
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

// StorageHelperProbes measures a declared path, never an inventory substitute.
// An omitted runtime mount program does not prove that no helper is in use.
func StorageHelperProbes(source model.Observation, now func() time.Time) []ExecutableProbe {
	p := ExecutableProbe{Role: "storage_mount_program", SourceID: source.ID, Now: now}
	if source.Completeness != model.Complete {
		return nil
	}
	selected := ""
	if info := source.Podman; info != nil {
		if info.Available == nil || !*info.Available || info.ServiceIsRemote != nil && *info.ServiceIsRemote {
			return nil
		}
		p.Source, p.RuntimePath, selected = sourceRuntime, info.Path, info.StorageMountProgram
	} else {
		c := source.Configuration
		if c == nil || c.Family != "storage" || !c.SelectionComplete || !c.ParseComplete || c.Storage == nil || c.Storage.MountProgram == nil {
			return nil
		}
		p.Source, p.RuntimePath, selected = sourceConfiguration, c.RuntimePath, c.Storage.MountProgram.Value
	}
	if !safeInfoPath(selected) {
		return nil
	}
	p.Paths = []string{selected}
	return []ExecutableProbe{p}
}
