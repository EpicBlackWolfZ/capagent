package platform

import (
	"context"
	"reflect"
	"testing"
)

type managerCapture struct{ calls []CommandSpec }

func (m *managerCapture) Run(_ context.Context, spec CommandSpec) (ExecResult, error) {
	m.calls = append(m.calls, spec)
	return ExecResult{}, nil
}
func TestUserManagerCommandAuthority(t *testing.T) {
	t.Parallel()
	runner := &managerCapture{}
	service := NewUserManager(runner)
	if _, err := service.Query(t.Context(), "/usr/bin/systemctl", "/run/user/1000"); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatal(runner.calls)
	}
	spec := runner.calls[0]
	args := []string{"--user", "--no-pager", "--no-ask-password", "show", "--property=Version", "--value"}
	env := []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/systemd/private", "LC_ALL=C", "PATH=/usr/bin:/bin",
		"XDG_RUNTIME_DIR=/run/user/1000"}
	if !reflect.DeepEqual(spec.Args, args) || !reflect.DeepEqual(spec.Env.Variables(), env) ||
		spec.Timeout != UserManagerTimeout || spec.Dir != "/" {
		t.Fatal("manager policy changed")
	}
	args = UserManagerArgs()
	args[0] = "--system"
	if UserManagerArgs()[0] != "--user" {
		t.Fatal("shared argv")
	}
	for _, tc := range []struct{ path, dir string }{{"/bin/sh", "/run/user/1000"}, {"/usr/bin/systemctl", "relative"}} {
		if _, err := service.Query(t.Context(), tc.path, tc.dir); err == nil {
			t.Fatal("unsafe command")
		}
	}
	if _, err := (UserManager{}).Query(t.Context(), "/usr/bin/systemctl", "/run/user/1000"); err == nil {
		t.Fatal("missing service")
	}
	if len(runner.calls) != 1 {
		t.Fatal("invalid query dispatched")
	}
	if got := escapeBusPath("/a+b,c;= ~é*"); got != "/a%2bb%2cc%3b%3d%20%7e%c3%a9*" {
		t.Fatal(got)
	}
}
