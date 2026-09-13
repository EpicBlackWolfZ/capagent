package fixture_test

import (
	"reflect"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/fixture"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func inspectionDocument(t *testing.T) *fixture.Document {
	t.Helper()
	d, err := fixture.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	d.Probe = "inspection"
	d.Context.Identity.Current.GroupsKnown, d.Context.Identity.Target.GroupsKnown = true, true
	uid, gid := uint32(1000), uint32(1000)
	for _, name := range []string{"home", "home/test", "run", "run/user", "run/user/1000"} {
		d.Files = append(d.Files, fixture.File{Path: name, Kind: "directory", Mode: 0o700, UID: &uid, GID: &gid})
	}
	env := map[string]string{"HOME": "/home/test", "XDG_RUNTIME_DIR": "/run/user/1000"}
	d.Commands = []fixture.Command{
		{Path: "/usr/bin/podman", Args: []string{"--version"}, Directory: "/", Environment: env,
			TimeoutMillis: 5000, Stdout: "podman version 5.8.4\n"},
		{Path: "/usr/bin/podman", Args: podman.LocalInfoArgs(), Directory: "/", Environment: env, TimeoutMillis: 30000, Stdout: `{}`},
	}
	return d
}

func TestCombinedFixtureServices(t *testing.T) {
	t.Parallel()
	doc := inspectionDocument(t)
	services, err := fixture.Open(doc)
	if err != nil {
		t.Fatal(err)
	}
	defer services.Close()
	doc.Commands[0].Args[0], doc.Commands[1].Args[0] = "mutated", "mutated"
	if !reflect.DeepEqual(services.InfoCommand.Args, podman.LocalInfoArgs()) {
		t.Fatal("retained command aliases")
	}
	for _, spec := range []struct{ info bool }{{false}, {true}} {
		command := services.Command
		if spec.info {
			command = services.InfoCommand
		}
		if _, err := services.Environment.Runner().Run(t.Context(), command); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCombinedFixtureRejectsMismatchedPolicy(t *testing.T) {
	t.Parallel()
	for _, change := range []func(*fixture.Document){
		func(d *fixture.Document) { d.Commands[1].Path = "/other/podman" },
		func(d *fixture.Document) { d.Commands[1].Args = []string{"info", "--format", "json"} },
		func(d *fixture.Document) { d.Commands[1].Directory = "/tmp" },
		func(d *fixture.Document) { d.Commands[1].TimeoutMillis = 1000 },
		func(d *fixture.Document) { d.Commands[1].Environment = map[string]string{"HOME": "/other"} },
		func(d *fixture.Document) { d.Commands = append(d.Commands, d.Commands[0]) },
		func(d *fixture.Document) { d.Context.Identity.Target.UID = 0 },
		func(d *fixture.Document) { d.Context.Identity.Current = nil },
		func(d *fixture.Document) { d.Commands[0].Environment["CONTAINER_HOST"] = "ssh://remote" },
	} {
		doc := inspectionDocument(t)
		change(doc)
		if services, err := fixture.Open(doc); err == nil {
			services.Close()
			t.Fatal("accepted inconsistent inspection policy")
		}
	}
}
