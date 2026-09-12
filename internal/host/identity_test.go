package host_test

import (
	"errors"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/host"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestObserveCurrentIdentity(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "account", true: "missing metadata"}[missing], func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			if !missing {
				for _, dir := range []string{"/etc", "/proc", "/proc/self", "/proc/self/ns"} {
					mem.AddDir(dir, 0o755)
				}
				mem.AddFile("/etc/passwd", []byte("root:x:0:0:root:/root:/bin/sh\n"), 0o644)
				for _, name := range []string{"user", "mnt", "net"} {
					mem.AddSymlink("/proc/self/ns/"+name, name+":[42]")
				}
			}
			files := platform.NewScopedMemReader("/", mem)
			defer files.Close()
			scope := model.EvaluationScope{RunID: "run", ContextID: "current", Runtime: "podman", Endpoint: "local"}
			c, obs := host.ObserveCurrent(t.Context(), scope, files, model.CurrentCredentials{GroupsKnown: true}, nil, time.Unix(1, 0))
			if c.Identity.Current == nil || c.Identity.Target == nil || c.Identity.Target.UID != 0 {
				t.Fatal("lost known root identity")
			}
			if c.Identity.Current == c.Identity.Target {
				t.Fatal("aliased current and target")
			}
			if !missing && (c.Identity.Target.Username != "root" || len(c.Namespaces) != 3 || len(obs.Facts) < 2) {
				t.Fatalf("lost metadata: %+v %+v", c, obs)
			}
			if missing && (len(c.Namespaces) != 0 || len(obs.Diagnostics) == 0 || obs.Completeness != model.Partial) {
				t.Fatal("missing metadata inferred")
			}
		})
	}
}

func TestObserveCurrentRetainsCredentialFailure(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	scope := model.EvaluationScope{RunID: "run", ContextID: "current", Runtime: "podman", Endpoint: "local"}
	credentials := model.CurrentCredentials{UID: 1000, EUID: 0}
	c, obs := host.ObserveCurrent(t.Context(), scope, files, credentials, errors.New("secret group failure"), time.Unix(1, 0))
	if c.Identity.Current.UID != 0 || obs.Completeness != model.Partial || len(obs.Diagnostics) == 0 {
		t.Fatal("lost effective identity or uncertainty")
	}
}
