package podman_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestNetworkSharesSelectedEngineSourceBytesAndTarget(t *testing.T) {
	t.Parallel()
	for _, version := range []string{storageLegacyVersion, testVersion} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			addEngineFile(mem, engineSystemPath, "[engine]\nhelper_binaries_dir=['/helpers']\n"+
				"[network]\nnetwork_backend='netavark'\n[containers]\ndns_servers=['192.0.2.53']")
			p := engineProbe(t, version, 1001)
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			engine, network, err := p.Collect(t.Context(), platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope()))
			if err != nil || network.Completeness != model.Complete || !network.Configuration.ParseComplete {
				t.Fatal("network source collection failed", err)
			}
			if len(engine.Configuration.Sources) != len(network.Configuration.Sources) {
				t.Fatal("different source plans")
			}
			for i, source := range network.Configuration.Sources {
				e := engine.Configuration.Sources[i]
				if source.Path != e.Path || source.SHA256 != e.SHA256 || source.Selected != e.Selected {
					t.Fatal("source bytes or scope diverged")
				}
			}
			n := network.Configuration.Network
			if n.EngineSourceID != engine.ID || n.Strings["network.network_backend"].Value != networkTestBackend {
				t.Fatal("lost family binding")
			}
			paths := n.HelperPaths[networkTestBackend]
			if paths.InheritedDefault || len(paths.Values) != 1 || paths.Values[0] != "/helpers/netavark" {
				t.Fatal("netavark used PATH fallback")
			}
			if len(n.HelperPaths[networkTestPasta].Values) != 3 {
				t.Fatal("pasta PATH fallback lost")
			}
		})
	}
}

func TestNetworkSourceFailuresKeepIndependentProjection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, text, status string
		complete, parsed   bool
	}{
		{"absent", "", "", true, true},
		{"malformed", "[network]\nsecret='unterminated", "malformed", true, false},
		{"typed field", "[network]\ndns_bind_port='secret'", "invalid", true, false},
		{"unsupported path", "[network]\nnetwork_config_dir='secret'", "unsupported", false, false},
		{"syntax", "[network]\nx={a=1,}", "unsupported", false, false},
		{"parser limit", "[network]\nx=" + strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40), networkTestLimit, false, false},
		{"read limit", strings.Repeat(" ", 65537), networkTestLimit, false, false},
		{networkTestDenied, "", networkTestDenied, false, false},
		{"partial", "", "incomplete", false, false},
		{storageCancelledCase, "", "", false, false},
		{networkTestUnqualified, "[network]\nnetwork_backend='netavark'", "parsed", false, true},
		{"no files", "", "", false, false},
		{"engine type error", "[engine]\nruntime=3\n[network]\nnetwork_backend='netavark'", "parsed", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			if test.status != "" {
				addEngineFile(mem, engineSystemPath, test.text)
			}
			if test.name == networkTestDenied {
				mem.AddError(engineSystemPath, fs.ErrPermission)
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithScope(versionScope())
			if test.name == "partial" {
				env = env.WithFiles(engineReadFailure{ScopedReader: files, failure: platform.ErrIncomplete})
			}
			if test.name == "no files" {
				env = env.WithFiles(nil)
			}
			ctx := t.Context()
			if test.name == storageCancelledCase {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			p := engineProbe(t, testVersion, 1001)
			if test.name == networkTestUnqualified {
				p.Version.Scope.ContextID = "unbound"
			}
			engine, network, _ := p.Collect(ctx, env)
			if (network.Completeness == model.Complete) != test.complete || network.Configuration.ParseComplete != test.parsed {
				t.Fatalf("completeness=%s parsed=%v", network.Completeness, network.Configuration.ParseComplete)
			}
			for _, source := range network.Configuration.Sources {
				if source.Path == engineSystemPath && test.status != "" {
					if source.Status != test.status {
						t.Fatalf("status=%s want=%s", source.Status, test.status)
					}
					if test.name == "partial" && (source.Network != nil || source.SHA256 != "") {
						t.Fatal("partial bytes became interpreted evidence")
					}
				}
			}
			if test.name == networkTestUnqualified && network.Configuration.Network != nil {
				t.Fatal("unqualified source became effective configuration")
			}
			if test.name == "engine type error" && engine.Configuration.ParseComplete {
				t.Fatal("engine field error lost")
			}
			for _, diagnostic := range network.Diagnostics {
				if strings.Contains(diagnostic.Message, "secret") {
					t.Fatal("source data leaked")
				}
			}
		})
	}
}
