package host

import (
	"context"
	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"strings"
	"testing"
)

const hostTestContext = "current"
const hostUnknown = "unknown"

const testCgroupRoot = "/sys/fs/cgroup"

func TestCgroupTopologyModes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, mounts, groups string
		types                map[string]platform.FilesystemInfo
		want                 string
	}{
		{"v2", "1 0 0:1 / /sys/fs/cgroup rw - cgroup2 cgroup rw\n", "0::/\n",
			map[string]platform.FilesystemInfo{testCgroupRoot: {Type: 0x63677270}}, "v2"},
		{"v1", "1 0 0:1 / /sys/fs/cgroup rw - cgroup cgroup rw,cpu\n", "1:cpu:/\n",
			map[string]platform.FilesystemInfo{testCgroupRoot: {Type: 0x27e0eb}}, "v1"},
		{"mixed", "1 0 0:1 / /sys/fs/cgroup rw - cgroup cgroup rw,cpu\n2 0 0:2 / /sys/fs/cgroup/unified rw - cgroup2 cgroup rw\n",
			"1:cpu:/\n0::/\n", map[string]platform.FilesystemInfo{testCgroupRoot: {Type: 0x27e0eb},
				"/sys/fs/cgroup/unified": {Type: 0x63677270}}, "mixed"},
		{"absent", "", "", nil, "unavailable"},
		{"malformed", "invalid\n", "", nil, hostUnknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mem := platform.NewMemPlatformReader()
			for _, dir := range []string{"/proc", "/proc/self", "/sys", "/sys/fs", testCgroupRoot, "/sys/fs/cgroup/unified"} {
				mem.AddDir(dir, 0o755)
			}
			mem.AddFile("/proc/self/mountinfo", []byte(tt.mounts), 0o644)
			mem.AddFile("/proc/self/cgroup", []byte(tt.groups), 0o644)
			for _, root := range []string{testCgroupRoot, "/sys/fs/cgroup/unified"} {
				mem.AddFile(root+"/cgroup.controllers", []byte("cpu memory\n"), 0o644)
				mem.AddFile(root+"/cgroup.subtree_control", []byte("cpu\n"), 0o644)
			}
			files := platform.NewScopedMemReaderWithFilesystems("/", mem, tt.types)
			defer files.Close()
			env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).
				WithScope(model.EvaluationScope{RunID: "test", ContextID: hostTestContext})
			obs, _ := (CgroupProbe{Now: testClock}).Run(t.Context(), env)
			if obs.Host.Cgroups.Mode != tt.want {
				t.Fatalf("got %+v", obs.Host.Cgroups)
			}
		})
	}
}

func TestSecurityAbsenceRemainsUnknown(t *testing.T) {
	t.Parallel()
	env := hostTestEnvironment(t, nil)
	obs, _ := (SecurityProbe{Now: testClock}).Run(t.Context(), env)
	if obs.Host.Security.SELinux != hostUnknown || obs.Host.Security.AppArmorEnabled != nil || obs.Completeness != model.Partial {
		t.Fatal("unmounted securityfs treated as disabled")
	}
}

func TestSecurityStates(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, lsm, enforce, enabled, profiles, status, clone, maximum string
		want                                                          string
		partial                                                       bool
	}{
		{"enforcing", "selinux,capability", "1", "N", "", "Seccomp: 2\nNoNewPrivs: 1\n", "1", "1024", "enforcing", false},
		{"permissive", "selinux", "0", "N", "", "Seccomp: 0\nNoNewPrivs: 0\n", "0", "0", "permissive", false},
		{"apparmor", "apparmor", "", "Y", "example (enforce)\nother (complain)\n", "Seccomp: 2\nNoNewPrivs: 1\n", "1", "1024", "disabled", false},
		{"malformed", "selinux", "garbage", "bad", "bad profile", "Seccomp: 9\nNoNewPrivs: 9\n", "invalid", "invalid", hostUnknown, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := map[string]string{"/sys/kernel/security/lsm": tt.lsm, "/sys/module/apparmor/parameters/enabled": tt.enabled,
				"/sys/kernel/security/apparmor/profiles": tt.profiles, "/proc/self/status": tt.status,
				"/proc/sys/kernel/seccomp/actions_avail": "kill_process allow\n", "/proc/sys/user/max_user_namespaces": tt.maximum,
				"/proc/sys/kernel/unprivileged_userns_clone": tt.clone}
			if tt.enforce != "" {
				content["/sys/fs/selinux/enforce"] = tt.enforce
			}
			env := topologyEnvironment(t, content, nil)
			obs, _ := (SecurityProbe{Now: testClock}).Run(t.Context(), env)
			if obs.Host.Security.SELinux != tt.want || (obs.Completeness != model.Complete) != tt.partial {
				t.Fatalf("%+v %+v", obs, obs.Host.Security)
			}
		})
	}
}

func topologyEnvironment(t *testing.T, content map[string]string, mounts map[string]platform.FilesystemInfo) platform.Environment {
	t.Helper()
	mem := platform.NewMemPlatformReader()
	for name, text := range content {
		parts := strings.Split(strings.TrimPrefix(name, "/"), "/")
		for i := 1; i < len(parts); i++ {
			mem.AddDir("/"+strings.Join(parts[:i], "/"), 0o755)
		}
		mem.AddFile(name, []byte(text), 0o644)
	}
	files := platform.NewScopedMemReaderWithFilesystems("/", mem, mounts)
	t.Cleanup(func() { files.Close() })
	return platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).
		WithScope(model.EvaluationScope{RunID: "test", ContextID: hostTestContext})
}

func TestCgroupRestrictedAndNamespaceRelativePaths(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, root, membership, mount string
		statType                      int64
		want                          string
	}{
		{"subtree", "/tenant", "/tenant/process", testCgroupRoot, cgroupV2Magic, "v2"},
		{"outside", "/tenant", "/elsewhere", testCgroupRoot, cgroupV2Magic, "v2"},
		{"escaping", "/", "/../outside", testCgroupRoot, cgroupV2Magic, "v2"},
		{"escaped mount", "/", "/", `/sys/fs/cgroup\040space`, cgroupV2Magic, "v2"},
		{"invalid mount", "/", "/", `/sys/fs/cgroup\999`, cgroupV2Magic, hostUnknown},
		{"wrong filesystem", "/", "/", testCgroupRoot, 1, hostUnknown},
		{"stat unavailable", "/", "/", testCgroupRoot, 0, hostUnknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mount := strings.ReplaceAll(tt.mount, `\040`, " ")
			content := map[string]string{"/proc/self/mountinfo": "1 0 0:1 " + tt.root + " " + tt.mount + " rw - cgroup2 cgroup rw\n",
				"/proc/self/cgroup": "0::" + tt.membership + "\n", mount + "/process/cgroup.controllers": "cpu memory\n",
				mount + "/process/cgroup.subtree_control": "cpu\n", mount + "/cgroup.controllers": "cpu\n", mount + "/cgroup.subtree_control": ""}
			var mounts map[string]platform.FilesystemInfo
			if tt.statType != 0 {
				mounts = map[string]platform.FilesystemInfo{mount: {Type: tt.statType}}
			}
			env := topologyEnvironment(t, content, mounts)
			obs, _ := (CgroupProbe{Now: testClock}).Run(t.Context(), env)
			if obs.Host.Cgroups.Mode != tt.want {
				t.Fatalf("%+v %+v", obs, obs.Host.Cgroups)
			}
		})
	}
}

func TestNamespaceMeasurementsAndCancellation(t *testing.T) {
	t.Parallel()
	mem := platform.NewMemPlatformReader()
	for _, dir := range []string{"/proc", "/proc/self", "/proc/self/ns"} {
		mem.AddDir(dir, 0o755)
	}
	for _, kind := range namespaceKinds {
		mem.AddSymlink("/proc/self/ns/"+kind, kind+":[42]")
	}
	files := platform.NewScopedMemReader("/", mem)
	defer files.Close()
	env := platform.NewEnvironment(nil, nil, nil, nil).WithFiles(files).
		WithScope(model.EvaluationScope{RunID: "test", ContextID: hostTestContext})
	obs, err := (NamespaceProbe{Now: testClock}).Run(t.Context(), env)
	if err != nil || len(obs.Host.Namespaces.Namespaces) != len(namespaceKinds) || obs.Completeness != model.Complete {
		t.Fatal(obs, err)
	}
	mem.AddSymlink("/proc/self/ns/user", "invalid")
	obs, _ = (NamespaceProbe{Now: testClock}).Run(t.Context(), env)
	if obs.Completeness != model.Partial {
		t.Fatal("invalid identity accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = (NamespaceProbe{Now: testClock}).Run(ctx, env); err == nil {
		t.Fatal("lost cancellation")
	}
	for _, probe := range Probes(testClock) {
		if probe.ID() == "" || len(probe.Dependencies()) != 0 {
			t.Fatal("unexpected probe dependency")
		}
	}
}
