package fixture

import (
	json "encoding/json/v2"
	"errors"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func hostDocument() *Document {
	value := true
	return &Document{SchemaVersion: 1, Probe: hostProbe, RunID: "host", Timestamp: time.Unix(1, 0),
		Context: model.EvaluationContext{ID: "current"}, Provenance: Provenance{Kind: "synthetic", Description: "unit test"},
		Host: &HostCalls{Uname: &platform.UnameInfo{Release: "6.12.0", Machine: "x86_64"}, NoNewPrivileges: &value,
			Filesystems: map[string]int64{"/proc": 9}},
		Files: []File{{Path: "proc", Kind: "directory", Mode: 0o755}},
		Commands: []Command{{Path: "/usr/bin/systemctl", Args: []string{"--version"}, Directory: "/", TimeoutMillis: hostVersionMillis,
			Stdout: "systemd 252\n"}}}
}

func TestHostFixtureReplay(t *testing.T) {
	t.Parallel()
	doc := hostDocument()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	services, err := Open(doc)
	if err != nil {
		t.Fatal(err)
	}
	defer services.Close()
	info, err := services.Environment.Host().Uname()
	if err != nil || info.Release != "6.12.0" {
		t.Fatal(info, err)
	}
	value, err := services.Environment.Host().NoNewPrivileges()
	if err != nil || !value {
		t.Fatal(value, err)
	}
	mount, err := services.Environment.Files().StatFS(t.Context(), "proc")
	if err != nil || mount.Type != 9 {
		t.Fatal(mount, err)
	}
	result, err := services.Environment.HostMetadata().SystemdVersion(t.Context(), "/usr/bin/systemctl")
	if err != nil || string(result.Stdout) != "systemd 252\n" {
		t.Fatal(result, err)
	}
	if services.Requirement != nil {
		t.Fatal("host fixture fabricated requirement")
	}
	doc.Host.UnameFailure = "permission"
	doc.Host.SecurityFailure = "timeout"
	snapshot := hostSnapshot(doc)
	if _, err = snapshot.Uname(); err == nil {
		t.Fatal("lost failure")
	}
	if _, err = snapshot.NoNewPrivileges(); err == nil {
		t.Fatal("lost security failure")
	}
	doc.Host.Uname = nil
	doc.Host.NoNewPrivileges = nil
	doc.Host.UnameFailure = ""
	doc.Host.SecurityFailure = ""
	snapshot = hostSnapshot(doc)
	if _, err = snapshot.Uname(); !errors.Is(err, platform.ErrIncomplete) {
		t.Fatal(err)
	}
}

func TestHostFixtureRejectsInvalidAuthority(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Document)
	}{
		{"runtime", func(d *Document) { d.Runtime = "podman"; d.Endpoint = "local" }},
		{"missing calls", func(d *Document) { d.Host = nil }},
		{"requirement", func(d *Document) { d.Requirement = []byte(`{"capability":"host.os"}`) }},
		{"too many commands", func(d *Document) { d.Commands = append(d.Commands, d.Commands[0]) }},
		{"command", func(d *Document) { d.Commands[0].Path = "/tmp/systemctl" }},
		{"argument", func(d *Document) { d.Commands[0].Args = []string{"start"} }},
		{"environment", func(d *Document) { d.Commands[0].Environment = map[string]string{"HOME": "/"} }},
		{"path", func(d *Document) { d.Host.Filesystems = map[string]int64{"/../escape": 1} }},
		{"errno", func(d *Document) { d.Host.UnameFailure = "invalid" }},
		{"too many mounts", func(d *Document) {
			for i := range maxFiles + 1 {
				d.Host.Filesystems[string(rune(i))] = 1
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := hostDocument()
			tt.mutate(doc)
			if err := validate(doc); err == nil {
				t.Fatal("accepted invalid host fixture")
			}
		})
	}
}
