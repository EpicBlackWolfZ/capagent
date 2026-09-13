package probe_test

import (
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
	"testing"
)

func TestSnapshotExecutableAndQuadletOwnership(t *testing.T) {
	t.Parallel()
	yes, owner, inode := true, uint32(1), uint64(2)
	obs := model.Observation{Executable: &model.ExecutableObservation{Candidates: []model.ExecutableCandidate{
		{Present: &yes, Executable: &yes, Device: &inode, Inode: &inode, File: &model.ExecutableMetadata{UID: &owner, GID: &owner}}}},
		Quadlet: &model.QuadletObservation{Locations: []model.SearchLocation{{Path: "/units", Present: &yes, Directory: &yes}}},
		Podman:  &model.PodmanInfo{OCIRuntime: &model.SelectedOCIRuntime{Name: "crun", Path: "/usr/bin/crun"}}}
	copy := probe.SnapshotObservation(obs)
	yes, owner, inode = false, 0, 0
	obs.Executable.Candidates[0].Path = "changed"
	obs.Quadlet.Locations[0].Path = "changed"
	obs.Podman.OCIRuntime.Name = "changed"
	c := copy.Executable.Candidates[0]
	if !*c.Present || !*c.Executable || *c.Device != 2 || *c.Inode != 2 || *c.File.UID != 1 || *c.File.GID != 1 ||
		c.Path != "" || copy.Quadlet.Locations[0].Path != "/units" || !*copy.Quadlet.Locations[0].Directory ||
		!*copy.Quadlet.Locations[0].Present || copy.Podman.OCIRuntime.Name != "crun" {
		t.Fatal("retained mutable payload")
	}
}
