package fixture_test

import (
	json "encoding/json/v2"
	"github.com/EpicBlackWolfZ/capagent/internal/fixture"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"testing"
)

func TestContextQueryFixtureAuthority(t *testing.T) {
	t.Parallel()
	data, err := platform.ReadDocument(t.Context(), "../../testdata/fixtures/v1/context-user-active", "fixture.json", fixture.MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"valid", "no execution", "no command", "not context", "args", "environment", "directory", "timeout", "path",
		"runtime directory", "capability budget", "access failure", "unknown mount"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, err := fixture.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "no execution":
				doc.Context.Identity.Execution = nil
			case "no command":
				doc.Commands = nil
			case "not context":
				doc.Probe = "host"
			case "args":
				doc.Commands[0].Args = []string{"start", "unsafe.service"}
			case "environment":
				doc.Commands[0].Environment["DBUS_SESSION_BUS_ADDRESS"] = "tcp:host=example.invalid"
			case "directory":
				doc.Commands[0].Directory = "/tmp"
			case "timeout":
				doc.Commands[0].TimeoutMillis = 1000
			case "path":
				doc.Commands[0].Path = "/bin/sh"
			case "runtime directory":
				doc.Context.Identity.XDGRuntimeDir = "relative"
			case "capability budget":
				doc.Files[0].Capabilities = &platform.CapabilityAttribute{Present: true, Bytes: make([]byte, 257)}
			case "access failure":
				doc.Files[0].AccessFailure = "invalid"
			case "unknown mount":
				doc.Host.MountPolicies["/absent"] = fixture.MountPolicy{}
			}
			encoded, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := fixture.Parse(encoded)
			if (err == nil) != (name == "valid") {
				t.Fatal(name, err)
			}
			if err == nil {
				services, err := fixture.Open(parsed)
				if err != nil {
					t.Fatal(err)
				}
				services.Close()
			}
		})
	}
}
