package version_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/EpicBlackWolfZ/capagent/internal/version"
)

func TestGet(t *testing.T) {
	t.Parallel()

	info := version.Get()
	if info.Version == "" {
		t.Error("expected non-empty Version")
	}
	if info.Vendor == "" {
		t.Error("expected non-empty Vendor")
	}
	if info.GoVersion == "" {
		t.Error("expected non-empty GoVersion")
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()

	infoStr := version.Info()
	if !strings.Contains(infoStr, "capagent version") {
		t.Errorf("expected Info() to contain 'capagent version', got: %s", infoStr)
	}
	if !strings.Contains(infoStr, version.Version) {
		t.Errorf("expected Info() to contain Version %q, got: %s", version.Version, infoStr)
	}
}

func TestPrintStartupBanner(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	version.PrintStartupBanner(&buf)

	output := buf.String()
	expectedSubstrings := []string{
		"Version",
		version.Version,
		"Vendor",
		version.Vendor,
		"Go Version",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(output, sub) {
			t.Errorf("PrintStartupBanner() output missing expected substring %q; got:\n%s", sub, output)
		}
	}
}
