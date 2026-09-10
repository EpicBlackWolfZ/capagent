package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	resourceHelperTest  = "-test.run=^TestRunnerResourceHelper$"
	resourceLifetime    = 8 * time.Second
	resourceReturnLimit = 3 * time.Second
	resourcePoll        = 5 * time.Millisecond
	resourceTimeout     = time.Second
	resourceExitCode    = 7
	resourcePrefix      = "retained-prefix\n"
)

func resourceArgs(mode, scenario, root string) []string {
	return []string{resourceHelperTest, "--", mode, scenario, root}
}

func waitResourceFile(path string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		time.Sleep(resourcePoll)
	}
	return nil, fmt.Errorf("timed out waiting for %s", path)
}

// Each scenario runs in an isolated subreaper so the test owns and reaps even
// the session-escaped grandchild. No process-global subreaper state leaks into
// the ordinary parallel suite, and production needs no subreaper controller.
func TestRunnerDetachedPipeBudget(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"success", "failure", "timeout", "cancel", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 2*resourceLifetime)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], resourceArgs("supervisor", scenario, t.TempDir())...)
			cmd.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0")
			cmd.WaitDelay = time.Second
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("supervisor: %v\n%s", err, out)
			}
		})
	}
}

func TestRunnerResourceHelper(t *testing.T) {
	index := 0
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index == 0 {
		return
	}
	args := os.Args[index:]
	const helperArgs = 3
	if len(args) != helperArgs {
		t.Fatal("invalid helper arguments")
	}
	mode, scenario, root := args[0], args[1], args[2]
	switch mode {
	case "supervisor":
		runResourceSupervisor(t, scenario, root)
	case "parent":
		runResourceParent(t, scenario, root)
	case "holder":
		session, err := unix.Getsid(0)
		if err != nil {
			t.Fatal(err)
		}
		if session != os.Getpid() {
			t.Fatal("holder did not escape session")
		}
		if _, err := os.Stdout.WriteString(resourcePrefix); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stderr.WriteString(resourcePrefix); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "holder"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		// Finite even if the supervisor crashes. Normally the supervisor kills and
		// reaps us as its adopted child after checking the runner's result.
		time.Sleep(resourceLifetime)
		os.Exit(0)
	default:
		t.Fatal("unknown resource helper mode")
	}
}

func runResourceParent(t *testing.T, scenario, root string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], resourceArgs("holder", scenario, root)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := waitResourceFile(filepath.Join(root, "holder"), resourceReturnLimit); err != nil {
		// A failed handshake must still release the direct child.
		if killErr := cmd.Process.Kill(); killErr != nil {
			t.Error(killErr)
		}
		_ = cmd.Wait() // Expected signal exit after failed readiness.
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "parent"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	switch scenario {
	case "success":
		os.Exit(0)
	case "failure":
		os.Exit(resourceExitCode)
	default:
		_ = cmd.Wait() // The runner intentionally kills this parent during Wait.
		os.Exit(0)
	}
}

func reapResourceChild(t *testing.T, pid int) {
	t.Helper()
	// pid is an unreaped child of this isolated supervisor and cannot be reused.
	if err := unix.Kill(pid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		t.Error(err)
	}
	deadline := time.Now().Add(resourceReturnLimit)
	for time.Now().Before(deadline) {
		var status unix.WaitStatus
		got, err := unix.Wait4(pid, &status, unix.WNOHANG, nil)
		if got == pid {
			return
		}
		if err != nil {
			t.Errorf("reap holder: %v", err)
			return
		}
		time.Sleep(resourcePoll)
	}
	t.Error("holder was not reaped within cleanup budget")
}

func runResourceSupervisor(t *testing.T, scenario, root string) {
	t.Helper()
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if scenario == "deadline" {
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithTimeout(ctx, resourceTimeout)
		defer deadlineCancel()
	}
	timeout := resourceLifetime
	if scenario == "timeout" {
		timeout = resourceTimeout
	}
	type outcome struct {
		result ExecResult
		err    error
	}
	done := make(chan outcome, 1)
	start := time.Now()
	go func() {
		policy, err := NewEnvPolicy(nil, map[string]string{"GORACE": "atexit_sleep_ms=0"})
		if err != nil {
			done <- outcome{err: err}
			return
		}
		spec := CommandSpec{Path: os.Args[0], Args: resourceArgs("parent", scenario, root), Env: policy}
		result, err := NewOSCommandRunner(timeout).Run(ctx, spec)
		done <- outcome{result, err}
	}()
	// Cleanup first cancels and joins Run, then kills/reaps the adopted holder.
	// It also executes on readiness/assertion failures, including RED runs.
	var joined bool
	defer func() {
		cancel()
		if data, err := os.ReadFile(filepath.Join(root, "holder")); err == nil {
			pid, parseErr := strconv.Atoi(string(data))
			if parseErr == nil {
				if err := unix.Kill(pid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
					t.Error(err)
				}
			}
		}
		if !joined {
			select {
			case <-done:
			case <-time.After(resourceLifetime):
				t.Error("Run did not join during cleanup")
			}
		}
		if data, err := os.ReadFile(filepath.Join(root, "holder")); err == nil {
			pid, parseErr := strconv.Atoi(string(data))
			if parseErr != nil {
				t.Error(parseErr)
				return
			}
			reapResourceChild(t, pid)
		}
	}()
	parent, err := waitResourceFile(filepath.Join(root, "parent"), resourceReturnLimit)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "cancel" {
		cancel()
	}
	var got outcome
	select {
	case got = <-done:
		joined = true
	case <-time.After(resourceReturnLimit):
		t.Fatal("runner blocked on detached pipe holder")
	}
	if time.Since(start) > resourceReturnLimit {
		t.Errorf("Run exceeded %v", resourceReturnLimit)
	}
	if !strings.Contains(string(got.result.Stdout), resourcePrefix) || !strings.Contains(string(got.result.Stderr), resourcePrefix) {
		t.Errorf("lost output prefixes: %+v", got.result)
	}
	if got.result.StdoutTruncated || got.result.StderrTruncated {
		t.Error("pipe drain failure must not masquerade as byte-cap overflow")
	}
	if got.result.TimedOut != (scenario == "timeout") {
		t.Errorf("TimedOut=%v for %s", got.result.TimedOut, scenario)
	}
	switch scenario {
	case "success":
		if !errors.Is(got.err, exec.ErrWaitDelay) || got.result.ExitCode != 0 {
			t.Errorf("success drain result: %+v, %v", got.result, got.err)
		}
	case "failure":
		var exitErr *exec.ExitError
		if !errors.As(got.err, &exitErr) || got.result.ExitCode != resourceExitCode {
			t.Errorf("failure: %+v, %v", got.result, got.err)
		}
	case "cancel":
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("cancel: %v", got.err)
		}
	default:
		if !errors.Is(got.err, context.DeadlineExceeded) {
			t.Errorf("deadline: %v", got.err)
		}
	}
	pid, err := strconv.Atoi(string(parent))
	if err != nil {
		t.Fatal(err)
	}
	var status unix.WaitStatus
	if _, err := unix.Wait4(pid, &status, unix.WNOHANG, nil); !errors.Is(err, unix.ECHILD) {
		t.Errorf("runner did not reap its direct child: %v", err)
	}
}
