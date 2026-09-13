package capability

import (
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
)

func TestExecutablePrerequisiteStates(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	regular := &model.ExecutableMetadata{Regular: true, ExecutableBits: true}
	for _, test := range []struct {
		name      string
		candidate model.ExecutableCandidate
		want      model.CapabilityState
	}{
		{"present", model.ExecutableCandidate{Present: &yes, File: regular, Executable: &yes}, model.StateSupported},
		{"absent", model.ExecutableCandidate{Present: &no}, model.StateUnsupported},
		{"denied", model.ExecutableCandidate{}, model.StateUnknown},
		{"blocked", model.ExecutableCandidate{Present: &yes, File: regular, Executable: &no}, model.StateMisconfigured},
		{"unknown access", model.ExecutableCandidate{Present: &yes, File: regular}, model.StateUnknown},
		{"mask", model.ExecutableCandidate{Present: &yes, Masked: true}, model.StateMisconfigured},
		{"directory", model.ExecutableCandidate{Present: &yes, File: &model.ExecutableMetadata{}}, model.StateMisconfigured},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := executableState(test.candidate, true); got != test.want {
				t.Fatalf("got %s want %s", got, test.want)
			}
		})
	}
}

func TestRuntimeSelectionCannotBeReplacedByInstalledCandidate(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	scope := model.EvaluationScope{RunID: "run", ContextID: "target", Runtime: "podman", Endpoint: "local"}
	at := time.Unix(1, 0)
	info := model.Observation{ID: "info", ProbeID: "podman.info", Scope: scope, Timestamp: at, Completeness: model.Complete,
		Podman: &model.PodmanInfo{Path: "/usr/bin/podman", Available: &yes,
			OCIRuntime: &model.SelectedOCIRuntime{Path: "/opt/runtime"}}}
	selected := model.Observation{ID: "selected", ProbeID: "selected", Scope: scope, Timestamp: at, Completeness: model.Complete,
		Executable: &model.ExecutableObservation{Role: "oci_runtime", Source: "runtime", SourceID: "info", RuntimePath: "/usr/bin/podman",
			SelectedPath: "/opt/runtime", Candidates: []model.ExecutableCandidate{{Path: "/opt/runtime", Present: &no}}}}
	installed := model.Observation{ID: "installed", ProbeID: "installed", Scope: scope, Timestamp: at, Completeness: model.Complete,
		Executable: &model.ExecutableObservation{Role: "crun", Source: "trusted_candidates", Candidates: []model.ExecutableCandidate{
			{Path: "/usr/bin/crun", Present: &yes, Executable: &yes, File: &model.ExecutableMetadata{Regular: true, ExecutableBits: true}}}}}
	def := selectedExecutableDefinition("oci_runtime")
	for _, scenario := range []string{"selected missing", "cross target", "incomplete info", "no selection"} {
		t.Run(scenario, func(t *testing.T) {
			observations := []model.Observation{info, selected, installed}
			want := model.StateMisconfigured
			switch scenario {
			case "cross target":
				observations[1].Scope.ContextID = "other"
				want = model.StateUnknown
			case "incomplete info":
				observations[0].Completeness = model.Partial
				want = model.StateUnknown
			case "no selection":
				observations = []model.Observation{installed}
				want = model.StateUnknown
			}
			r := Resolve(scope, def.ID, at, def.Evaluate(scope, observations))
			if r.Capability.State != want {
				t.Fatal(r.Capability)
			}
		})
	}
}

func TestQuadletSessionAndCgroupPrerequisites(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	for _, test := range []struct {
		name  string
		state *model.UserContextObservation
		want  model.CapabilityState
	}{
		{"running", &model.UserContextObservation{Accessible: &yes}, model.StateSupported},
		{"query failed", &model.UserContextObservation{Accessible: &no}, model.StateUnavailable},
		{"not queried", &model.UserContextObservation{SocketValid: &yes}, model.StateUnknown},
		{"bad runtime", &model.UserContextObservation{Runtime: model.RuntimeDirectoryObservation{Valid: &no}}, model.StateMisconfigured},
		{"socket absent", &model.UserContextObservation{SocketPresent: &no}, model.StateUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := userManagerState(test.state); got != test.want {
				t.Fatalf("%s != %s", got, test.want)
			}
		})
	}
	for _, test := range []struct {
		mode string
		want model.CapabilityState
	}{{"v2", model.StateSupported}, {"v1", model.StateUnsupported},
		{"mixed", model.StateUnsupported}, {"unavailable", model.StateUnavailable}, {"unknown", model.StateUnknown}} {
		if got := cgroupV2State(test.mode); got != test.want {
			t.Fatalf("%s: %s != %s", test.mode, got, test.want)
		}
	}
}

func TestCgroupLivePrecedenceAndRootfulManager(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	v2 := "v2"
	scope := model.EvaluationScope{RunID: "run", ContextID: "root", Runtime: "podman", Endpoint: "local"}
	at := time.Unix(1, 0)
	for _, partial := range []bool{false, true} {
		t.Run(string(model.Complete)+map[bool]string{true: "-partial"}[partial], func(t *testing.T) {
			t.Parallel()
			live := model.Observation{ID: "live", ProbeID: "host", Scope: scope, Timestamp: at, Completeness: model.Complete,
				Host: &model.HostObservation{Cgroups: &model.CgroupObservation{Mode: "v1"}, Systemd: &model.SystemdObservation{Running: &no}}}
			if partial {
				live.Completeness = model.Partial
			}
			info := model.Observation{ID: "info", ProbeID: "podman.info", Scope: scope, Timestamp: at, Completeness: model.Complete,
				Podman: &model.PodmanInfo{Available: &yes, CgroupVersion: &v2}}
			for _, def := range QuadletDefinitions(false) {
				resolution := Resolve(scope, def.ID, at, def.Evaluate(scope, []model.Observation{live, info}))
				want := model.StateUnknown
				if def.ID == PodmanCgroupV2ID {
					want = model.StateUnsupported
					if partial {
						want = model.StateSupported
					}
				}
				if !partial {
					switch def.ID {
					case QuadletManagerID:
						want = model.StateUnavailable
					case QuadletLingerID, QuadletRuntimeDirectoryID:
						want = model.StateUnsupported
					}
				}
				if resolution.Capability.State != want {
					t.Fatalf("%s: %s != %s", def.ID, resolution.Capability.State, want)
				}
			}
		})
	}
}
