package podman_test

import (
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestVersionCorpus(t *testing.T) {
	t.Parallel()
	const root = "../../../testdata/podman/version/"
	captured, err := platform.ReadDocument(t.Context(), root, "fedora-5.8.4.txt", 4096)
	if err != nil {
		t.Fatal(err)
	}
	version, err := podman.ParseVersion(captured)
	if err != nil || version.Canonical != "5.8.4" {
		t.Fatal("captured version", version, err)
	}
	data, err := platform.ReadDocument(t.Context(), root, "synthetic.json", 4096)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Raw, Canonical, Suffix string }
	if err := json.Unmarshal(data, &fixtures, json.MatchCaseInsensitiveNames(true)); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		version, err := podman.ParseVersion([]byte(fixture.Raw))
		if err != nil || version.Canonical != fixture.Canonical || version.Suffix != fixture.Suffix {
			t.Fatal("synthetic version", fixture, err)
		}
	}
}

func TestParseVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, canonical, suffix, build, trailing string
	}{
		{"podman version 3.4.4\n", "3.4.4", "", "", ""},
		{"podman version 4.6.1\n", "4.6.1", "", "", ""},
		{"podman version 5.1.0\r\n", "5.1.0", "", "", ""},
		{"podman version 4.4.1-12.el9_2", "4.4.1", "12.el9_2", "", ""},
		{"podman version 4.9.4-3.el9_4", "4.9.4", "3.el9_4", "", ""},
		{"podman version 5.0.0-dev", "5.0.0", "dev", "", ""},
		{"podman version 5.0.0-rc.1+abc123 amd64", "5.0.0", "rc.1", "abc123", "amd64"},
		{"podman version 5.8.4 (git abcdef) linux/amd64", "5.8.4", "", "", "(git abcdef) linux/amd64"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := podman.ParseVersion([]byte(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			if got.Canonical != tt.canonical || got.Suffix != tt.suffix || got.Build != tt.build || got.Trailing != tt.trailing {
				t.Fatalf("unexpected parsed version: %+v", got)
			}
			if got.Raw != strings.TrimSpace(tt.input) || got.Major == 0 {
				t.Fatalf("missing raw or numeric version: %+v", got)
			}
		})
	}
}

func TestParseVersionRejectsAmbiguousOutput(t *testing.T) {
	t.Parallel()
	inputs := []string{"", "5.8.4", "docker version 5.8.4", "podman version 5.8", "podman version 05.8.4",
		"podman version 5.8.4-", "podman version 5.8.4+", "podman version 5.8.4\nsecret", "podman version 5.8.4\x1b[31m",
		"podman version 4294967296.0.0", "podman version 5.8.4 https://secret.invalid/token", strings.Repeat("x", 4097)}
	inputs = append(inputs, "podman version 5.8.4 SECRET", "podman version 5.8.4 4.9.4", "podman version 5.8.4 user/password")
	for _, input := range inputs {
		if _, err := podman.ParseVersion([]byte(input)); err == nil {
			t.Errorf("accepted invalid version %q", input)
		}
	}
}
