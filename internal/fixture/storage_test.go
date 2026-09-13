package fixture

import (
	json "encoding/json/v2"
	"errors"
	"io/fs"
	"testing"
)

func TestStorageAccessFixtureRoundTrip(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"allowed", "denied", "incomplete", "invalid failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			doc := hostDocument()
			yes, no := true, false
			doc.Files[0].DirectoryRead = &yes
			doc.Files[0].DirectoryWrite = &no
			doc.Host.MountPolicies = map[string]MountPolicy{"/proc": {ReadOnly: true}}
			switch scenario {
			case "denied":
				doc.Files[0].DirectoryRead = &no
			case "incomplete":
				doc.Files[0].DirectoryReadFailure = "permission"
			case "invalid failure":
				doc.Files[0].DirectoryWriteFailure = "invalid"
			}
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Parse(data)
			if scenario == "invalid failure" {
				if err == nil {
					t.Fatal("accepted invalid directory failure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			services, err := Open(decoded)
			if err != nil {
				t.Fatal(err)
			}
			defer services.Close()
			allowed, err := services.Environment.Files().DirectoryAccess(t.Context(), "proc", false)
			if scenario == "incomplete" {
				if !errors.Is(err, fs.ErrPermission) {
					t.Fatal(err)
				}
			} else if err != nil || allowed != (scenario == "allowed") {
				t.Fatal(allowed, err)
			}
			allowed, err = services.Environment.Files().DirectoryAccess(t.Context(), "proc", true)
			if err != nil || allowed {
				t.Fatal("read access became write access", err)
			}
			mount, err := services.Environment.Files().StatFS(t.Context(), "proc")
			if err != nil || !mount.FlagsKnown || !mount.ReadOnly {
				t.Fatal("mount policy lost", err)
			}
		})
	}
}
