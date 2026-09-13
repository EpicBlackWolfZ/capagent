package host

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestUserManagerRealCommandErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path  string
		args        []string
		unavailable bool
	}{
		{"nonzero", "/bin/false", nil, true},
		{"startup", "/capagent-missing-command", nil, false},
		{"drain", "/bin/sh", []string{"-c", "printf '258.3\\n'; sleep 1 &"}, false},
		{"nonzero drain", "/bin/sh", []string{"-c", "printf '258.3\\n'; sleep 1 & exit 1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, failure := platform.NewOSCommandRunner(time.Second).Run(t.Context(), platform.CommandSpec{Path: tc.path, Args: tc.args})
			if failure == nil {
				t.Fatal("expected real command failure")
			}
			env, _ := userContextEnvironment(t, nil, result, failure)
			obs, err := (UserContextProbe{Target: model.UserIdentity{UID: 1000, Username: userAccount}, Active: true,
				Now: testClock}).Run(t.Context(), env)
			if err != nil {
				t.Fatal(err)
			}
			if tc.unavailable {
				if obs.UserContext.Accessible == nil || *obs.UserContext.Accessible || obs.Completeness != model.Complete {
					t.Fatal("completed failure lost", obs.UserContext, obs.Completeness)
				}
			} else if obs.UserContext.Accessible != nil || obs.Completeness != model.Partial {
				t.Fatal("incomplete query asserted accessibility", obs.UserContext, obs.Completeness)
			}
		})
	}
}

func TestSystemctlFallbackUncertainty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		failure error
		present *bool
	}{
		{"absent denied", fs.ErrPermission, nil}, {"absent absent", fs.ErrNotExist, ptr(false)}, {"absent present", nil, ptr(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			mem.AddDir("/bin", 0o755)
			if tc.failure != nil {
				mem.AddError("/bin/systemctl", tc.failure)
			} else {
				mem.AddFile("/bin/systemctl", nil, 0o755)
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			runner := platform.NewFakeCommandRunner()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithHost(nil, platform.NewHostMetadata(runner))
			obs, _ := (SystemdProbe{Now: testClock}).Run(context.Background(), env)
			got := obs.Host.Systemd.UtilityInstalled
			if (got == nil) != (tc.present == nil) || got != nil && *got != *tc.present {
				t.Fatal("incorrect fallback presence", got)
			}
			if tc.failure != nil && len(runner.Calls()) != 0 {
				t.Fatal("uninspected candidate executed")
			}
		})
	}
}
