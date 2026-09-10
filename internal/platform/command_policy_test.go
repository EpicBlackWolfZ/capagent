package platform_test

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

const policySecret = "private-policy-marker"

type policyObservation struct {
	Args []string
	Env  []string
	Dir  string
}

func policyArgs(args ...string) []string {
	return append([]string{"-test.run=^TestCommandPolicyHelper$", "--", "--policy-helper"}, args...)
}

// Environment mutation is confined to a subprocess; ordinary tests remain parallel.
func TestCommandPolicyHelper(t *testing.T) {
	index := 0
	for i, arg := range os.Args {
		if arg == "--policy-helper" {
			index = i + 1
			break
		}
	}
	if index == 0 {
		return
	}
	args := os.Args[index:]
	if len(args) > 0 && args[0] == "snapshot" {
		const snapshotArgCount = 2
		if len(args) != snapshotArgCount {
			t.Fatal("snapshot requires its parent-owned fixture directory")
		}
		t.Setenv("CAPAGENT_CAPTURE", "before")
		t.Setenv("CAPAGENT_OVERRIDE", "inherited")
		t.Setenv("CAPAGENT_SECRET", policySecret)
		// The parent owns this directory: os.Exit below bypasses child cleanups.
		fakeDir := args[1]
		if err := os.WriteFile(filepath.Join(fakeDir, "fake-tool"), []byte("#!/bin/sh\nexit 91\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", fakeDir)
		t.Setenv("LC_ALL", "ambient-locale")
		t.Setenv("DOCKER_HOST", "tcp://private.invalid:1234")
		t.Setenv("LD_PRELOAD", policySecret)
		t.Setenv("HTTP_PROXY", policySecret)
		t.Setenv("HOME", "/private-home")
		t.Setenv("PWD", "/private-pwd")
		t.Setenv("CAPAGENT_ABSENT", "")
		if err := os.Unsetenv("CAPAGENT_ABSENT"); err != nil {
			t.Fatal(err)
		}
		overrides := map[string]string{"CAPAGENT_OVERRIDE": "explicit", "CAPAGENT_EMPTY": ""}
		policy, err := platform.NewEnvPolicy([]string{"CAPAGENT_CAPTURE", "CAPAGENT_OVERRIDE", "CAPAGENT_ABSENT"}, overrides)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPAGENT_CAPTURE", "after")
		overrides["CAPAGENT_OVERRIDE"] = "mutated"
		runner := platform.NewOSCommandRunner(5 * time.Second)
		if _, err := runner.Run(t.Context(), platform.CommandSpec{Path: "fake-tool"}); !errors.Is(err, platform.ErrInvalidCommandSpec) {
			t.Fatalf("bare executable: %v", err)
		}
		result, err := runner.Run(t.Context(), platform.CommandSpec{Path: os.Args[0], Args: policyArgs(), Env: policy})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stdout.Write(result.Stdout); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(policyObservation{Args: args, Env: os.Environ(), Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestCommandPolicy_DefaultAndLiteralArguments(t *testing.T) {
	t.Parallel()
	args := []string{"", "$(touch /never)", "; echo nope", "*", "space argument"}
	for _, dir := range []string{"", t.TempDir()} {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			result, err := platform.NewOSCommandRunner(5*time.Second).Run(t.Context(), platform.CommandSpec{
				Path: os.Args[0], Args: policyArgs(args...), Dir: dir,
			})
			if err != nil {
				t.Fatal(err)
			}
			var got policyObservation
			if err := json.Unmarshal(result.Stdout, &got); err != nil {
				t.Fatal(err)
			}
			wantDir := dir
			if wantDir == "" {
				wantDir = "/"
			}
			if got.Dir != wantDir || !reflect.DeepEqual(got.Args, args) ||
				!reflect.DeepEqual(got.Env, []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}) {
				t.Fatalf("unexpected observation: %+v", got)
			}
		})
	}
}

func TestCommandPolicy_ExplicitSnapshot(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], policyArgs("snapshot", t.TempDir())...)
	cmd.Env = []string{"GORACE=atexit_sleep_ms=0"}
	cmd.WaitDelay = time.Second
	// Coverage-instrumented helpers may write a coverage diagnostic to stderr;
	// keep the JSON observation stream separate, just as production consumers do.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("snapshot helper: %v\n%s\n%s", err, out, stderr.Bytes())
	}
	var got policyObservation
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"CAPAGENT_CAPTURE=before", "CAPAGENT_EMPTY=", "CAPAGENT_OVERRIDE=explicit", "LC_ALL=C", "PATH=/usr/bin:/bin"}
	if got.Dir != "/" || !reflect.DeepEqual(got.Env, want) {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
}

func TestCommandPolicy_ValidationAndRedaction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec platform.CommandSpec
	}{
		{"empty", platform.CommandSpec{}},
		{"bare", platform.CommandSpec{Path: policySecret}},
		{"relative", platform.CommandSpec{Path: "./" + policySecret}},
		{"path NUL", platform.CommandSpec{Path: "/" + policySecret + "\x00"}},
		{"argument NUL", platform.CommandSpec{Path: trueCommand, Args: []string{policySecret + "\x00"}}},
		{"relative cwd", platform.CommandSpec{Path: trueCommand, Dir: policySecret}},
		{"cwd NUL", platform.CommandSpec{Path: trueCommand, Dir: "/" + policySecret + "\x00"}},
		{"negative timeout", platform.CommandSpec{Path: trueCommand, Timeout: -time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := platform.NewFakeCommandRunner()
			for _, runner := range []platform.CommandRunner{platform.NewOSCommandRunner(0), fake} {
				result, err := runner.Run(t.Context(), tt.spec)
				if !errors.Is(err, platform.ErrInvalidCommandSpec) {
					t.Fatalf("invalid spec: %v", err)
				}
				if result.Duration != 0 || result.TimedOut || len(result.Stdout) != 0 {
					t.Fatal("invalid spec performed execution")
				}
				assertPolicyRedacted(t, err)
			}
			if err := fake.Register(tt.spec, platform.ExecResult{}); !errors.Is(err, platform.ErrInvalidCommandSpec) {
				t.Fatal(err)
			}
			if len(fake.Calls()) != 0 {
				t.Fatal("invalid call was recorded")
			}
		})
	}
	for _, vars := range []map[string]string{{"": "x"}, {"BAD=KEY": "x"}, {"BAD\x00KEY": "x"}, {"1BAD": "x"}, {"KEY": policySecret + "\x00"}} {
		_, err := platform.NewEnvPolicy(nil, vars)
		if !errors.Is(err, platform.ErrInvalidEnvPolicy) {
			t.Fatalf("invalid environment: %v", err)
		}
		assertPolicyRedacted(t, err)
	}
	if _, err := platform.NewEnvPolicy([]string{"*"}, nil); !errors.Is(err, platform.ErrInvalidEnvPolicy) {
		t.Fatal(err)
	}
	policy, err := platform.NewEnvPolicy(nil, map[string]string{"SECRET": policySecret})
	if err != nil {
		t.Fatal(err)
	}
	spec := platform.CommandSpec{Path: "/" + policySecret, Dir: "/" + policySecret, Args: []string{policySecret}, Env: policy}
	assertPolicyRedacted(t, policy)
	assertPolicyRedacted(t, spec)
	_, err = platform.NewOSCommandRunner(0).Run(t.Context(), spec)
	if !errors.Is(err, syscall.ENOENT) {
		t.Fatal(err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatal("lost startup error type")
	}
	assertPolicyRedacted(t, err)
	_, err = platform.NewFakeCommandRunner().Run(t.Context(), spec)
	assertPolicyRedacted(t, err)
}

func assertPolicyRedacted(t *testing.T, value any) {
	t.Helper()
	if err, ok := value.(error); ok && strings.Contains(err.Error(), policySecret) {
		t.Fatal("secret disclosed by Error()")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if strings.Contains(fmt.Sprintf(format, value), policySecret) {
			t.Fatalf("secret disclosed with %s", format)
		}
	}
}

func TestCommandPolicy_TimeoutAndPrecancelled(t *testing.T) {
	t.Parallel()
	runner := platform.NewOSCommandRunner(5 * time.Second)
	result, err := runner.Run(t.Context(), platform.CommandSpec{Path: sleepCommand, Args: []string{"5"}, Timeout: 20 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) || !result.TimedOut {
		t.Fatalf("per-call timeout: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	marker := filepath.Join(t.TempDir(), "never-created")
	for _, r := range []platform.CommandRunner{runner, platform.NewFakeCommandRunner()} {
		result, err := r.Run(ctx, platform.CommandSpec{Path: "/bin/touch", Args: []string{marker}})
		if !errors.Is(err, context.Canceled) || result.Duration != 0 {
			t.Fatalf("precancelled: %+v %v", result, err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("precancelled command started")
	}
}
