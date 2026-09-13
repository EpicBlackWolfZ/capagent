package capability

import "github.com/EpicBlackWolfZ/capagent/internal/model"

const (
	StorageKernelOverlayID model.CapabilityID = "runtime.podman.storage.kernel.overlay"
	StorageKernelFUSEID    model.CapabilityID = "runtime.podman.storage.kernel.fuse"
)

func storageKernelDefinitions() []Definition {
	var result []Definition
	for _, id := range []model.CapabilityID{StorageKernelOverlayID, StorageKernelFUSEID} {
		result = append(result,
			Definition{ID: id,
				Description: "Kernel currently registers this filesystem; module loading, mounts and device access are unverified",
				Evaluate: func(scope model.EvaluationScope, observations []model.Observation) []model.Evidence {
					if !localPodman(scope) {
						return nil
					}
					var evidence []model.Evidence
					for _, obs := range observations {
						if obs.Scope != scope || obs.Host == nil || obs.Host.Filesystems == nil {
							continue
						}
						registered := obs.Host.Filesystems.OverlayRegistered
						if id == StorageKernelFUSEID {
							registered = obs.Host.Filesystems.FUSERegistered
						}
						evidence = append(evidence, prerequisiteEvidence(obs, id, booleanState(registered, model.StateUnsupported),
							"current kernel filesystem registration from host facts; this does not establish usable storage or device access"))
					}
					return evidence
				}})
	}
	return result
}
