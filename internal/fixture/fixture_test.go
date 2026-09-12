package fixture_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/fixture"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const document = `{
 "schema_version":1,"run_id":"fixture","timestamp":"2026-09-12T00:00:00Z",
 "provenance":{"kind":"synthetic","description":"test fixture"},
 "context":{"id":"user","identity":{"current":{"uid":1000,"gid":1000},"target":{"uid":1000,"gid":1000}}},
 "runtime":"podman","endpoint":"local",
 "files":[{"path":"usr","kind":"directory","mode":493},
 {"path":"usr/libexec","kind":"directory","mode":493},
 {"path":"usr/libexec/podman","kind":"directory","mode":493},
 {"path":"usr/libexec/podman/netavark","kind":"file","mode":493,"uid":0,"gid":0}],
 "commands":[{"path":"/usr/bin/podman","args":["info","--format","json"],"stdout":"{}"}],
 "requirement":{"capability":"runtime.podman.netavark"}
}`

func TestFixtureServices(t *testing.T) {
	t.Parallel()
	doc, err := fixture.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	services, err := fixture.Open(doc)
	if err != nil {
		t.Fatal(err)
	}
	defer services.Close()
	result, err := services.Environment.Runner().Run(t.Context(), services.Command)
	if err != nil || string(result.Stdout) != "{}" {
		t.Fatal(result, err)
	}
	stat, err := services.Environment.Files().Stat("usr/libexec/podman/netavark")
	if err != nil || !stat.Mode().IsRegular() {
		t.Fatal(stat, err)
	}
	if services.Environment.Scope() != doc.Scope() {
		t.Fatal("fixture lost scope")
	}
	if err := services.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := services.Environment.Files().Stat("."); err == nil {
		t.Fatal("fixture resource was not closed")
	}
}

func TestFixtureMetadataAndCommandSnapshots(t *testing.T) {
	t.Parallel()
	doc, err := fixture.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	doc.Files = append(doc.Files, fixture.File{Path: "link", Kind: "symlink", Target: "/usr/libexec/podman/netavark"},
		fixture.File{Path: "denied", Kind: "error", Failure: "permission"})
	doc.Commands[0].Environment = map[string]string{"CAPAGENT_FIXTURE": "original"}
	services, err := fixture.Open(doc)
	if err != nil {
		t.Fatal(err)
	}
	defer services.Close()
	doc.Commands[0].Args[0] = "mutated"
	doc.Commands[0].Environment["CAPAGENT_FIXTURE"] = "mutated"
	result, err := services.Environment.Runner().Run(t.Context(), services.Command)
	if err != nil || string(result.Stdout) != "{}" {
		t.Fatal("command snapshot changed", result, err)
	}
	if target, err := services.Environment.Files().Readlink("link"); err != nil || target != "/usr/libexec/podman/netavark" {
		t.Fatal(target, err)
	}
	if _, err := services.Environment.Files().Stat("link"); err != nil {
		t.Fatal(err)
	}
	if _, err := services.Environment.Files().Stat("denied"); !errors.Is(err, fs.ErrPermission) {
		t.Fatal(err)
	}
}

func TestFixtureFailureCategories(t *testing.T) {
	t.Parallel()
	for _, category := range []string{"unavailable", "not_found", "permission", "timeout", "cancelled"} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()
			doc, err := fixture.Parse([]byte(document))
			if err != nil {
				t.Fatal(err)
			}
			doc.Commands[0].Failure = category
			services, err := fixture.Open(doc)
			if err != nil {
				t.Fatal(err)
			}
			defer services.Close()
			result, err := services.Environment.Runner().Run(t.Context(), services.Command)
			if err == nil || result.TimedOut != (category == "timeout") {
				t.Fatal(result, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := services.Environment.Runner().Run(ctx, services.Command); err == nil {
				t.Fatal("cancelled invocation succeeded")
			}
		})
	}
}

func TestFixtureInvalidMetadataAndPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*fixture.Document)
	}{
		{"duplicate path", func(d *fixture.Document) { d.Files = append(d.Files, d.Files[0]) }},
		{"partial ownership", func(d *fixture.Document) { d.Files[len(d.Files)-1].GID = nil }},

		{"unknown file failure", func(d *fixture.Document) { d.Files[0].Failure = "invalid" }},
		{"missing error", func(d *fixture.Document) { d.Files[0].Kind = "error" }},
		{"timeout budget", func(d *fixture.Document) { d.Commands[0].TimeoutMillis = 30001 }},
		{"environment", func(d *fixture.Document) { d.Commands[0].Environment = map[string]string{"INVALID=NAME": "value"} }},
		{"unknown command failure", func(d *fixture.Document) { d.Commands[0].Failure = "invalid" }},
		{"commands missing", func(d *fixture.Document) { d.Commands = nil }},
		{"files limit", func(d *fixture.Document) { d.Files = make([]fixture.File, 4097) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, err := fixture.Parse([]byte(document))
			if err != nil {
				t.Fatal(err)
			}
			tt.change(doc)
			services, err := fixture.Open(doc)
			if err == nil {
				services.Close()
				t.Fatal("invalid fixture accepted")
			}
		})
	}
	if _, err := fixture.Open(nil); err == nil {
		t.Fatal("nil fixture accepted")
	}
	// Fixture reads reuse the existing memory containment implementation.
	doc, err := fixture.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	services, err := fixture.Open(doc)
	if err != nil {
		t.Fatal(err)
	}
	defer services.Close()
	if _, err := services.Environment.Files().ReadFile(t.Context(), "../outside"); err == nil {
		t.Fatal("fixture escaped root")
	}
	if _, ok := services.Environment.Files().(platform.ScopedReader); ok {
		t.Fatal("probe obtained fixture close authority")
	}
}

func TestInvalidFixtures(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, old, new string }{
		{"schema", `"schema_version":1`, `"schema_version":2`},
		{"provenance", `"synthetic"`, `"unknown"`},
		{"escape", `usr/libexec/podman/netavark`, `../outside`},
		{"unknown kind", `"kind":"file"`, `"kind":"other"`},
		{"relative command", `/usr/bin/podman`, `bin/podman`},
		{"missing run", `"run_id":"fixture"`, `"run_id":""`},
		{"missing identity UID", `"uid":1000,"gid":1000`, `"gid":1000`},
		{"null identity GID", `"uid":1000,"gid":1000`, `"uid":1000,"gid":null`},
		{"unsupported runtime", `"runtime":"podman"`, `"runtime":"docker"`},
		{"unsupported endpoint", `"endpoint":"local"`, `"endpoint":"remote"`},
		{"bad requirements", `"capability":"runtime.podman.netavark"`, `"typo":true`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, err := fixture.Parse([]byte(strings.Replace(document, tt.old, tt.new, 1)))
			if err == nil {
				services, openErr := fixture.Open(doc)
				if openErr == nil {
					services.Close()
					t.Fatal("invalid fixture accepted")
				}
			}
		})
	}
	for _, raw := range []string{"null", "{", strings.Repeat(" ", fixture.MaxBytes+1)} {
		if _, err := fixture.Parse([]byte(raw)); err == nil {
			t.Fatal("invalid fixture JSON accepted")
		}
	}
}
