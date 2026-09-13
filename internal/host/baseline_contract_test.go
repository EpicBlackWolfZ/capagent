package host

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

func TestAssignmentGrammar(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		raw, want string
		valid     bool
	}{
		{"", "", true}, {"\"two words\" # comment", "two words", true}, {"'literal $name'", "literal $name", true},
		{"\"escaped \\$name\"", "escaped $name", true}, {"plain # comment", "plain", true}, {"unterminated\\", "", false},
		{"\"unterminated", "", false}, {"\"a\"\"b\"", "", false}, {"a;b", "", false}, {"abc\x00", "", false}, {"a b", "", false},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			got, ok := assignmentValue(tt.raw)
			if ok != tt.valid || ok && got != tt.want {
				t.Fatalf("%q %t", got, ok)
			}
		})
	}
	if validKey("") || validKey("invalid key") {
		t.Fatal("invalid key accepted")
	}
}

func TestHostReadLimits(t *testing.T) {
	t.Parallel()
	limits := platform.DefaultParserLimits()
	for _, data := range [][]byte{[]byte(strings.Repeat("x", limits.LineBytes+1)), []byte(strings.Repeat("\n", limits.Records+1)),
		[]byte(strings.Repeat("x", limits.InputBytes+1))} {
		if _, err := hostLines(data, nil); err == nil {
			t.Fatal("unbounded parser")
		}
	}
	lines, err := hostLines([]byte("ID=debian\nID=truncated"), platform.ErrIncomplete)
	if err == nil || strings.Contains(strings.Join(lines, "\n"), "truncated") {
		t.Fatal("incomplete record retained")
	}
}

func hostTestEnvironment(t *testing.T, files map[string]string) platform.Environment {
	t.Helper()
	mem := platform.NewMemPlatformReader()
	for _, dir := range []string{"/etc", "/usr", "/usr/lib", "/usr/bin", "/lib", "/lib/systemd", "/usr/lib/systemd",
		"/bin", "/proc", "/proc/1", "/run", "/run/systemd", "/run/systemd/system"} {
		mem.AddDir(dir, 0o755)
	}
	for path, text := range files {
		mem.AddFile(path, []byte(text), 0o755)
	}
	scoped := platform.NewScopedMemReader("/", mem)
	t.Cleanup(func() { scoped.Close() })
	return platform.NewEnvironment(nil, nil, nil, nil).WithFiles(scoped).WithScope(model.EvaluationScope{RunID: "test", ContextID: "current"})
}
func testClock() time.Time { return time.Unix(1, 0) }

func TestOSReleaseFallbackAndFields(t *testing.T) {
	t.Parallel()
	env := hostTestEnvironment(t, map[string]string{"/usr/lib/os-release": "# comment\nID=fedora\nNAME='Fedora Linux'\n" +
		"PRETTY_NAME=\"Fedora 40\"\nVARIANT=Server\nID_LIKE='rhel centos'\nOTHER=ignored\n"})
	obs, err := (OSReleaseProbe{Now: testClock}).Run(t.Context(), env)
	if err != nil || obs.Host.OS.Name != "Fedora Linux" || len(obs.Host.OS.IDLike) != 2 || obs.Host.OS.Variant != "Server" {
		t.Fatalf("%+v %v", obs, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cancelled, err := (OSReleaseProbe{Now: testClock}).Run(ctx, env)
	if !errors.Is(err, context.Canceled) || cancelled.Completeness != model.Partial {
		t.Fatal("cancellation lost")
	}
}

func TestKernelObservations(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		machine, release string
		failure          error
	}{
		{"aarch64", "5.14.0-427.el9", nil}, {"riscv64", "custom", nil}, {"", "", fs.ErrPermission}, {"bad\n", "invalid", nil},
	} {
		t.Run(tt.machine, func(t *testing.T) {
			t.Parallel()
			env := hostTestEnvironment(t, nil).WithHost(platform.HostSnapshot{
				UnameResult: platform.UnameInfo{Release: tt.release, Machine: tt.machine},
				UnameError:  tt.failure}, platform.HostMetadata{})
			obs, _ := (KernelProbe{Now: testClock}).Run(t.Context(), env)
			if tt.machine == "aarch64" && obs.Host.Kernel.Architecture != "arm64" {
				t.Fatal("normalization")
			}
			if tt.failure != nil && obs.Completeness != model.Partial {
				t.Fatal("lost failure")
			}
		})
	}
}

func TestSystemdStateAndVersion(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, comm, version string
		failed, truncated   bool
	}{
		{"running", "systemd\n", "systemd 252 (252-14.el9)\n", false, false},
		{"not init", "init\n", "systemd 239\n", false, false},
		{"vendor", "systemd\n", "systemd 255.4\n", false, false},
		{"bad version", "init\n", "broken\n", false, false},
		{"command failed", "init\n", "", true, false},
		{"truncated", "systemd\n", "systemd 255\n", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := hostTestEnvironment(t, map[string]string{"/proc/1/comm": tt.comm, "/usr/bin/systemctl": "", "/usr/lib/systemd/systemd": ""})
			runner := platform.NewFakeCommandRunner()
			var commandErr error
			if tt.failed {
				commandErr = fs.ErrPermission
			}
			spec := platform.CommandSpec{Path: "/usr/bin/systemctl", Args: []string{"--version"}, Dir: "/", Timeout: platform.HostVersionTimeout}
			result := platform.ExecResult{Stdout: []byte(tt.version), StdoutTruncated: tt.truncated}
			if err := runner.RegisterWithError(spec, result, commandErr); err != nil {
				t.Fatal(err)
			}
			env = env.WithHost(nil, platform.NewHostMetadata(runner))
			obs, err := (SystemdProbe{Now: testClock}).Run(t.Context(), env)
			if err != nil || obs.Host.Systemd.Installed == nil || !*obs.Host.Systemd.Installed {
				t.Fatal("installation lost")
			}
			if (obs.Host.Systemd.Version == "") != (tt.failed || tt.truncated || tt.name == "bad version") {
				t.Fatal("version uncertainty")
			}
			if *obs.Host.Systemd.Running != (tt.comm == "systemd\n") {
				t.Fatal("PID1 scope")
			}
		})
	}
}

func TestHostDiagnosticsBoundedAndSanitized(t *testing.T) {
	t.Parallel()
	obs := newHostObservation("test", model.EvaluationScope{}, testClock)
	for _, err := range []error{fs.ErrPermission, fs.ErrNotExist, platform.ErrIncomplete, platform.ErrLimitExceeded,
		platform.ErrMalformed, context.Canceled, errors.New("SECRET")} {
		recordSource(&obs, "fixed", err)
	}
	for range maxHostDiagnostics * 2 {
		hostWarning(&obs, "bounded")
	}
	if len(obs.Diagnostics) != maxHostDiagnostics || obs.Completeness != model.Partial {
		t.Fatal("unbounded diagnostics")
	}
	for _, diag := range obs.Diagnostics {
		if strings.Contains(diag.Message, "SECRET") {
			t.Fatal("raw error leaked")
		}
	}
}

func TestDistributionCorpus(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, id, version string }{
		{"rhel-8", "rhel", "8"}, {"rhel-9", "rhel", "9"}, {"rhel-10", "rhel", "10"},
		{"fedora-39", "fedora", "39"}, {"fedora-40", "fedora", "40"}, {"centos-stream-9", "centos", "9"},
		{"debian-12", "debian", "12"}, {"ubuntu-22.04", "ubuntu", "22.04"}, {"ubuntu-24.04", "ubuntu", "24.04"},
		{"alpine-3.19", "alpine", "3.19"}, {"nobara-44", "nobara", "44"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := platform.ReadDocument(t.Context(), "../../testdata/hosts", tt.name+".os-release", maxHostText)
			if err != nil {
				t.Fatal(err)
			}
			env := hostTestEnvironment(t, map[string]string{"/etc/os-release": string(data)})
			obs, err := (OSReleaseProbe{Now: testClock}).Run(t.Context(), env)
			if err != nil || obs.Host.OS.ID != tt.id || obs.Host.OS.VersionID != tt.version {
				t.Fatalf("%+v %v", obs.Host.OS, err)
			}
		})
	}
}

func TestKernelNumericComparison(t *testing.T) {
	t.Parallel()
	base := model.KernelVersion{Major: 6, Minor: 1, Patch: 1}
	for _, tt := range []struct {
		v    model.KernelVersion
		want int
	}{
		{base, 0}, {model.KernelVersion{Major: 5}, 1}, {model.KernelVersion{Major: 6, Minor: 2}, -1},
		{model.KernelVersion{Major: 6, Minor: 1, Patch: 2}, -1},
	} {
		if got := CompareKernelVersions(base, tt.v); got != tt.want {
			t.Fatal(got)
		}
	}
}
