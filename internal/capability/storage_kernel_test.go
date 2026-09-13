package capability

import (
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestStorageKernelRegistrationUsesExistingHostFacts(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"registered", "unregistered", "unobserved", "incomplete"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			scope := model.EvaluationScope{RunID: "kernel", ContextID: storageTestTarget, Runtime: storageTestRuntime, Endpoint: storageTestEndpoint}
			at := time.Unix(1, 0)
			registered := state == "registered"
			facts := &model.FilesystemObservation{OverlayRegistered: &registered, FUSERegistered: &registered}
			obs := model.Observation{ID: "filesystems", ProbeID: "filesystems", Scope: scope, Timestamp: at, Completeness: model.Complete,
				Host: &model.HostObservation{Filesystems: facts}}
			want := model.StateUnsupported
			if registered {
				want = model.StateSupported
			}
			if state == "unobserved" {
				facts.OverlayRegistered, facts.FUSERegistered = nil, nil
				want = model.StateUnknown
			}
			if state == "incomplete" {
				obs.Completeness = model.Partial
				want = model.StateUnknown
			}
			for _, definition := range StorageDefinitions() {
				if definition.ID != StorageKernelOverlayID && definition.ID != StorageKernelFUSEID {
					continue
				}
				evidence := definition.Evaluate(scope, []model.Observation{obs})
				result := Resolve(scope, definition.ID, at, evidence)
				if result.Capability.State != want || len(evidence) != 1 || evidence[0].Precedence != model.PrecedenceLive {
					t.Fatal("lost live filesystem registration evidence", definition.ID, result.Capability.State)
				}
			}
		})
	}
}
