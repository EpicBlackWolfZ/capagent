package platform

import (
	"context"
	"errors"
	"strings"
	"time"
)

const UserManagerTimeout = 2 * time.Second

// UserManager permits exactly one local read query. No ambient bus address, manager
// start, login operation, remote option or caller-provided command arguments exist.
type UserManager struct{ runner CommandRunner }

func NewUserManager(runner CommandRunner) UserManager { return UserManager{runner: runner} }
func UserManagerArgs() []string {
	return []string{"--user", "--no-pager", "--no-ask-password", "show", "--property=Version", "--value"}
}
func (m UserManager) Query(ctx context.Context, executable, runtimeDir string) (ExecResult, error) {
	spec, err := UserManagerCommand(executable, runtimeDir)
	if err != nil || m.runner == nil {
		return ExecResult{}, errors.New("user manager query unavailable")
	}
	return m.runner.Run(ctx, spec)
}

// UserManagerCommand also defines the exact offline replay contract.
func UserManagerCommand(executable, runtimeDir string) (CommandSpec, error) {
	if (executable != "/usr/bin/systemctl" && executable != "/bin/systemctl") || !ValidRuntimePath(runtimeDir) {
		return CommandSpec{}, errors.New("invalid user manager command")
	}
	// Pin the fallback address too, including on systemctl versions using a bus fallback.
	policy, err := NewEnvPolicy(nil, map[string]string{"XDG_RUNTIME_DIR": runtimeDir,
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=" + escapeBusPath(runtimeDir+"/systemd/private")})
	if err != nil {
		return CommandSpec{}, err
	}
	return CommandSpec{Path: executable, Args: UserManagerArgs(), Env: policy, Dir: "/", Timeout: UserManagerTimeout}, nil
}

func (e Environment) WithUserManager(manager UserManager) Environment { e.manager = manager; return e }
func (e Environment) UserManager() UserManager                        { return e.manager }

// D-Bus address escaping is byte-oriented and narrower than URL path escaping.
func escapeBusPath(name string) string {
	const hex = "0123456789abcdef"
	const nibbleBits = 4
	const nibbleMask = 15
	var out strings.Builder
	for _, b := range []byte(name) {
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("/._-*", rune(b)) {
			out.WriteByte(b)
		} else {
			out.WriteByte('%')
			out.WriteByte(hex[b>>nibbleBits])
			out.WriteByte(hex[b&nibbleMask])
		}
	}
	return out.String()
}
