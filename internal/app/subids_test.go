package app

import (
	"bytes"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"strings"
	"testing"
)

func TestContextFixtureReportsAllocations(t *testing.T) {
	t.Parallel()
	var out, diagnostics bytes.Buffer
	status := Execute(t.Context(), Options{Fixture: "../../testdata/fixtures/v1/context-rootless"}, &out, &diagnostics)
	if status != ExitIndeterminate {
		t.Fatal(status, diagnostics.String())
	}
	for _, want := range []string{`"subids":`, `"total":35`, `"total":12`, `"setuid":true`, `"executable":true`, `"usable":null`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestActiveHostContextHasNoRuntimeRequirement(t *testing.T) {
	t.Parallel()
	services := targetServices(t, 1000)
	opts := Options{Active: true}
	if !validOptions(opts) {
		t.Fatal("host active rejected")
	}
	report, err := evaluateCurrentServices(t.Context(), opts, services)
	if err != nil || report.Evaluation.Collection != "active" || report.Evaluation.Requirement.State != "" || len(report.Runtimes) != 0 {
		t.Fatal(report, err)
	}
}
func TestCurrentUserEnvironmentAuthority(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "relative", "/run/user/1000"} {
		services := currentServices{userEnvironment: func() (platform.EnvPolicy, error) {
			return platform.NewEnvPolicy(nil, map[string]string{
				"XDG_RUNTIME_DIR": value, "DBUS_SESSION_BUS_ADDRESS": "tcp:host=SECRET"})
		}}
		p := currentUserProbe(Options{}, services, model.UserIdentity{UID: 1000})
		if p.RuntimeDirectory == nil || *p.RuntimeDirectory != value {
			t.Fatal("environment selection lost")
		}
		services.worker = &targetPayload{}
		p = currentUserProbe(Options{}, services, model.UserIdentity{UID: 1000})
		if p.RuntimeDirectory != nil {
			t.Fatal("launcher environment reached target")
		}
	}
	services := currentServices{userEnvironment: func() (platform.EnvPolicy, error) {
		return platform.EnvPolicy{}, platform.ErrInvalidEnvPolicy
	}}
	if currentUserProbe(Options{}, services, model.UserIdentity{}).EnvironmentError == nil {
		t.Fatal("capture failure lost")
	}
}
