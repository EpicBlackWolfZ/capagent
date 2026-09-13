package podman_test

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestParseInfoStorageMountProgram(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		document, want string
		invalid        bool
	}{
		{`{"store":{"graphOptions":{"overlay.mount_program":{"Executable":"/usr/bin/fuse-overlayfs","Package":"private"},` +
			`"unknown":"synthetic-secret"}}}`,
			"/usr/bin/fuse-overlayfs",
			false},
		{`{"store":{"graphOptions":{}}}`, "", false},
		{`{"store":{"graphOptions":{"overlay.mount_program":{"Executable":3}}}}`, "", true},
	} {
		t.Run(test.document, func(t *testing.T) {
			t.Parallel()
			got, err := podman.ParseInfo([]byte(test.document))
			if (err != nil) != test.invalid || got.StorageMountProgram != test.want {
				t.Fatalf("path=%q error=%v", got.StorageMountProgram, err)
			}
		})
	}
}

func TestStorageMountProgramMetadataUsesOnlySelectedPath(t *testing.T) {
	t.Parallel()
	// A custom configured helper must not be replaced by an installed inventory candidate.
	mem := platform.NewMemPlatformReader()
	mem.AddFile("/usr/bin/fuse-overlayfs", nil, 0o755)
	if err := mem.SetExecutableAccess("/usr/bin/fuse-overlayfs", true, nil); err != nil {
		t.Fatal(err)
	}
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	source := model.Observation{ID: storageFamily, Scope: versionScope(), Completeness: model.Complete,
		Configuration: &model.ConfigurationObservation{Family: storageFamily,
			RuntimePath:       testPodmanPath,
			SelectionComplete: true,
			ParseComplete:     true,
			Storage:           &model.StorageConfiguration{MountProgram: &model.ConfigString{Value: "/missing-custom"}}}}
	helpers := podman.StorageHelperProbes(source, nil)
	if len(helpers) != 1 {
		t.Fatal("missing selected mount-program metadata probe")
	}
	obs, _ := helpers[0].Run(t.Context(), platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
	if obs.Executable.SourceID != source.ID || obs.Executable.SelectedPath != "/missing-custom" || len(obs.Executable.Candidates) != 1 ||
		obs.Executable.Candidates[0].Present == nil || *obs.Executable.Candidates[0].Present {
		t.Fatal("inventory replaced missing configured mount program")
	}
}

func TestStorageHelperSourcesMustBeCompleteLocalAndExplicit(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"reported helper", "remote helper", "unavailable helper", "unreported helper", "incomplete helper",
		"no source", "no parsed configuration", "unsafe path"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			yes := true
			source := model.Observation{Completeness: model.Complete, Podman: &model.PodmanInfo{
				Path: testPodmanPath, Available: &yes, StorageMountProgram: "/usr/bin/fuse-overlayfs"}}
			switch scenario {
			case "remote helper":
				source.Podman.ServiceIsRemote = &yes
			case "unavailable helper":
				source.Podman.Available = nil
			case "unreported helper":
				source.Podman.StorageMountProgram = ""
			case "incomplete helper":
				source.Completeness = model.Partial
			case "no source":
				source.Podman = nil
			case "no parsed configuration":
				source.Podman = nil
				source.Configuration = &model.ConfigurationObservation{Family: storageFamily}
			case "unsafe path":
				source.Podman.StorageMountProgram = "../helper"
			}
			helpers := podman.StorageHelperProbes(source, nil)
			if (len(helpers) == 1) != (scenario == "reported helper") {
				t.Fatal("incorrect selected helper authority", helpers)
			}
		})
	}
}
