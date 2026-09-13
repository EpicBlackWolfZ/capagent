package platform

import (
	"testing"
)

func TestHostProcfsPathsAndMountDecoding(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		raw, want string
		valid     bool
	}{
		{`/path\040space`, "/path space", true}, {`/path\011tab`, "/path\ttab", true},
		{`/path\012line`, "/path\nline", true}, {`/path\134slash`, "/path\\slash", true},
		{`/bad\123`, "", false}, {`/bad\1`, "", false}, {"/plain", "/plain", true},
	} {
		got, err := DecodeMountPath(tt.raw)
		if (err == nil) != tt.valid || err == nil && got != tt.want {
			t.Fatalf("%q %v", got, err)
		}
	}
	mem := NewMemPlatformReader()
	mem.AddDir("/proc", 0o755)
	mem.AddDir("/proc/self", 0o755)
	mem.AddFile("/proc/self/cgroup", []byte("0::/\n"), 0o644)
	files := NewScopedMemReader("/", mem)
	defer files.Close()
	proc := NewHostProcfsReader(files)
	groups, err := proc.Cgroups(t.Context())
	if err != nil || len(groups) != 1 {
		t.Fatal(groups, err)
	}
	if _, err := proc.ReadProcFile(t.Context(), "../etc/passwd"); err == nil {
		t.Fatal("proc prefix escaped")
	}
	controllers, err := ParseControllerList(t.Context(), []byte("cpu memory\n"), nil)
	if err != nil || len(controllers) != 2 {
		t.Fatal(controllers, err)
	}
	env := NewEnvironment(nil, nil, nil, nil).WithHost(HostSnapshot{UnameResult: UnameInfo{Release: "test"}}, HostMetadata{})
	info, err := env.Host().Uname()
	if err != nil || info.Release != "test" {
		t.Fatal(info, err)
	}
	if _, err := env.HostMetadata().SystemdVersion(t.Context(), "/bin/systemctl"); err == nil {
		t.Fatal("runner missing")
	}
	runner := NewFakeCommandRunner()
	runner.Register(CommandSpec{Path: "/bin/systemctl", Args: []string{"--version"}, Dir: "/", Timeout: HostVersionTimeout}, ExecResult{})
	if _, err := NewHostMetadata(runner).SystemdVersion(t.Context(), "/bin/systemctl"); err != nil {
		t.Fatal(err)
	}
}
