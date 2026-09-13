package podman_test

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/runtime/podman"
)

func TestInspectionCommandPolicy(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	mem.AddDir("/home", 0o755)
	mem.AddDir("/home/test", 0o700)
	mem.AddDir("/run", 0o755)
	mem.AddDir("/run/user", 0o755)
	mem.AddDir(testRuntimeDir, 0o700)
	mem.SetOwnership(testRuntimeDir, platform.FileOwnership{UID: 1000, GID: 1000})
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	user := model.UserIdentity{UID: 1000, GID: 1000, HomeDir: "/home/test"}
	policy, err := platform.NewEnvPolicy(nil, map[string]string{testRuntimeEnv: testRuntimeDir, "XDG_DATA_HOME": "/data", "PATH": "/poison",
		"CONTAINER_HOST": "ssh://secret@elsewhere", "CONTAINERS_CONF": "/private"})
	if err != nil {
		t.Fatal(err)
	}
	commands, err := podman.PrepareInspection(t.Context(), files, user, testPodmanPath, policy)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"HOME=/home/test", "LC_ALL=C", "PATH=/usr/bin:/bin", "XDG_DATA_HOME=/data", "XDG_RUNTIME_DIR=/run/user/1000"}
	if !reflect.DeepEqual(commands.Version.Env.Variables(), want) || !reflect.DeepEqual(commands.Info.Env.Variables(), want) {
		t.Fatal("environment policy was not frozen and narrowed")
	}
	if commands.Version.Timeout != 5*time.Second || commands.Info.Timeout != 30*time.Second ||
		commands.Version.Dir != "/" || commands.Info.Dir != "/" || !slices.Equal(commands.Info.Args, podman.LocalInfoArgs()) {
		t.Fatal("incorrect command budget or invocation")
	}
	args := podman.LocalInfoArgs()
	args[0] = "--remote=true"
	if slices.Equal(args, podman.LocalInfoArgs()) {
		t.Fatal("shared mutable command arguments")
	}

	for _, vars := range []map[string]string{
		{}, {"HOME": "", testRuntimeEnv: testRuntimeDir}, {"HOME": "relative", testRuntimeEnv: testRuntimeDir},
		{"HOME": "/absent", testRuntimeEnv: testRuntimeDir}, {testRuntimeEnv: ""}, {testRuntimeEnv: "/absent"},
		{testRuntimeEnv: testRuntimeDir, "XDG_CONFIG_HOME": "relative"},
		{testRuntimeEnv: testRuntimeDir, "XDG_DATA_HOME": "/bad\npath"},
	} {
		captured, policyErr := platform.NewEnvPolicy(nil, vars)
		if policyErr != nil {
			t.Fatal(policyErr)
		}
		if _, err := podman.PrepareInspection(t.Context(), files, user, testPodmanPath, captured); err == nil {
			t.Fatal("accepted unsafe or missing execution environment")
		}
	}
	mem.SetOwnership(testRuntimeDir, platform.FileOwnership{UID: 0, GID: 0})
	if _, err := podman.PrepareInspection(t.Context(), files, user, testPodmanPath, policy); err == nil {
		t.Fatal("accepted another user's runtime directory")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := podman.PrepareInspection(ctx, files, user, testPodmanPath, policy); err == nil {
		t.Fatal("prepared cancelled inspection")
	}
}

const testRuntimeEnv = "XDG_RUNTIME_DIR"
