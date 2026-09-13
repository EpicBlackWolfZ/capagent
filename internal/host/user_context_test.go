package host

import (
	"context"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"io/fs"
	"testing"
)

const userAccount = "alice"

const userRuntime = "/run/user/1000"

func TestUserRuntimeMetadataAndLinger(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		mode          fs.FileMode
		owner         uint32
		exists, valid bool
	}{
		{"private", 0o700, 1000, true, true}, {"public", 0o755, 1000, true, false},
		{"wrong owner", 0o700, 0, true, false}, {"missing", 0, 0, false, false},
		{"sticky", 0o700 | fs.ModeSticky, 1000, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			for _, p := range []string{"/run", "/run/user", "/var", "/var/lib", "/var/lib/systemd", "/var/lib/systemd/linger"} {
				mem.AddDir(p, 0o755)
			}
			mem.AddFile("/var/lib/systemd/linger/alice", nil, 0o644)
			if tc.exists {
				mem.AddDir(userRuntime, tc.mode)
				mem.SetOwnership(userRuntime, platform.FileOwnership{UID: tc.owner})
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files)
			obs, err := (UserContextProbe{Target: model.UserIdentity{UID: 1000, Username: userAccount}, Now: testClock}).Run(t.Context(), env)
			if err != nil {
				t.Fatal(err)
			}
			u := obs.UserContext
			if u.Runtime.Valid == nil || *u.Runtime.Valid != tc.valid || u.Runtime.Source != "default" || u.Runtime.Path != userRuntime {
				t.Fatal(u)
			}
			if u.LingerEnabled == nil || !*u.LingerEnabled || u.QueryAttempted || u.Accessible != nil {
				t.Fatal("linger implied manager access", u)
			}
		})
	}
}

func userContextEnvironment(t *testing.T, alter func(*platform.MemPlatformReader), result platform.ExecResult,
	failure error,
) (platform.Environment, *platform.FakeCommandRunner) {
	t.Helper()
	mem := platform.NewMemPlatformReader()
	for _, p := range []string{"/run", "/run/user", userRuntime, userRuntime + "/systemd", "/usr", "/usr/bin"} {
		mem.AddDir(p, 0o700)
	}
	mem.SetOwnership(userRuntime, platform.FileOwnership{UID: 1000})
	mem.AddSpecial(userRuntime+userSocketSuffix, fs.ModeSocket|0o600)
	mem.SetOwnership(userRuntime+userSocketSuffix, platform.FileOwnership{UID: 1000})
	mem.AddFile("/usr/bin/systemctl", nil, 0o755)
	if alter != nil {
		alter(mem)
	}
	files := platform.NewScopedMemReader("/", mem)
	t.Cleanup(func() { files.Close() })
	runner := platform.NewFakeCommandRunner()
	policy, _ := platform.NewEnvPolicy(nil, map[string]string{"XDG_RUNTIME_DIR": userRuntime,
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=" + userRuntime + userSocketSuffix})
	runner.RegisterWithError(platform.CommandSpec{Path: "/usr/bin/systemctl", Args: platform.UserManagerArgs(), Env: policy,
		Dir: "/", Timeout: platform.UserManagerTimeout}, result, failure)
	return platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).WithUserManager(platform.NewUserManager(runner)), runner
}
func TestUserManagerOptInAndFailureSemantics(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"passive", "active", "nonzero", "timeout", "truncated", "bad reply", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := platform.ExecResult{Stdout: []byte("258.3\n")}
			var failure error
			switch name {
			case "nonzero":
				result.ExitCode = 1
			case "timeout":
				result.TimedOut = true
			case "truncated":
				result.StdoutTruncated = true
			case "bad reply":
				result.Stdout = []byte("PRIVATE_DATA\nnot-a-version")
			case "cancelled":
				failure = context.Canceled
			}
			env, runner := userContextEnvironment(t, nil, result, failure)
			obs, err := (UserContextProbe{Target: model.UserIdentity{UID: 1000, Username: userAccount}, Active: name != "passive",
				Now: testClock}).Run(t.Context(), env)
			if err != nil {
				t.Fatal(err)
			}
			u := obs.UserContext
			if name == "passive" {
				if u.Accessible != nil || u.QueryAttempted || len(runner.Calls()) != 0 {
					t.Fatal(u)
				}
				return
			}
			if !u.QueryAttempted || len(runner.Calls()) != 1 {
				t.Fatal(u)
			}
			switch name {
			case "active":
				if u.Accessible == nil || !*u.Accessible || u.ManagerVersion != "258.3" {
					t.Fatal(u)
				}
			case "nonzero":
				if u.Accessible == nil || *u.Accessible {
					t.Fatal(u)
				}
			default:
				if u.Accessible != nil || obs.Completeness != model.Partial {
					t.Fatal(u)
				}
			}
		})
	}
}
func TestUserContextRejectsUntrustedPrerequisites(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"bad directory", "unknown owner", "socket owner", "socket type", "socket permission", "missing utility",
		"utility kind", "utility permission", "linger permission", "invalid name", "bad environment"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env, runner := userContextEnvironment(t, func(mem *platform.MemPlatformReader) {
				switch name {
				case "unknown owner":
					mem.AddDir(userRuntime, 0o700)
				case "socket owner":
					mem.SetOwnership(userRuntime+userSocketSuffix, platform.FileOwnership{})
				case "socket type":
					mem.AddFile(userRuntime+userSocketSuffix, nil, 0o600)
					mem.SetOwnership(userRuntime+userSocketSuffix, platform.FileOwnership{UID: 1000})
				case "socket permission":
					mem.AddError(userRuntime+userSocketSuffix, fs.ErrPermission)
				case "missing utility":
					mem.AddError("/usr/bin/systemctl", fs.ErrNotExist)
				case "utility kind":
					mem.AddDir("/usr/bin/systemctl", 0o755)
				case "utility permission":
					mem.AddError("/usr/bin/systemctl", fs.ErrPermission)
				case "linger permission":
					mem.AddError("/var", fs.ErrPermission)
				}
			}, platform.ExecResult{Stdout: []byte("258")}, nil)
			p := UserContextProbe{Target: model.UserIdentity{UID: 1000, Username: userAccount}, Now: testClock, Active: true}
			if name == "bad directory" {
				s := "relative"
				p.RuntimeDirectory = &s
			}
			if name == "invalid name" {
				p.Target.Username = "../bad"
			}
			if name == "bad environment" {
				p.EnvironmentError = platform.ErrInvalidEnvPolicy
			}
			obs, _ := p.Run(t.Context(), env)
			allowed := name == "invalid name" || name == "linger permission"
			if !allowed && (obs.UserContext.QueryAttempted || len(runner.Calls()) != 0) {
				t.Fatal("unsafe dispatch", obs)
			}
		})
	}
}
