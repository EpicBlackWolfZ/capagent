package podman_test

import (
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestAuthenticInfoCorpus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file, version, cgroup, backend string
		rootless                       bool
	}{
		{"ubuntu-3.4.4.json", "3.4.4", "v1", "", false},
		{"arch-4.4.4.json", "4.4.4", "v2", "netavark", true},
		{"fedora-5.8.4.json", testVersion, "v2", "netavark", true},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()
			raw, err := platform.ReadDocument(t.Context(), "../../../testdata/podman/info", tt.file, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			p, err := podman.ParseInfo(raw)
			if err != nil || p.Version != tt.version || p.CgroupVersion == nil || *p.CgroupVersion != tt.cgroup ||
				p.Rootless == nil || *p.Rootless != tt.rootless || p.GraphRoot == nil || p.RunRoot == nil || p.StorageDriver == nil {
				t.Fatal("lost captured runtime fields", p, err)
			}
			if tt.backend == "" {
				if p.NetworkBackend != nil {
					t.Fatal("inferred missing historical backend")
				}
			} else if p.NetworkBackend == nil || *p.NetworkBackend != tt.backend {
				t.Fatal("lost reported backend")
			}
		})
	}
}

func TestAuthenticAdditionalVersions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"ubuntu-3.4.4.txt", "rhel-4.9.4.txt"} {
		raw, err := platform.ReadDocument(t.Context(), "../../../testdata/podman/version", name, 4096)
		if err != nil {
			t.Fatal(err)
		}
		version, err := podman.ParseVersion(raw)
		if err != nil {
			t.Fatal(err)
		}
		if name == "rhel-4.9.4.txt" && (version.Canonical != "4.9.4" || version.Suffix != "rhel") {
			t.Fatal("enterprise suffix changed numeric version")
		}
	}
}
