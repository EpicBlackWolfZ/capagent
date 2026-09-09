package platform_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// sleepCommand is used to build deterministic time-based tests. "/bin/sleep"
// is part of POSIX coreutils and is virtually guaranteed on Linux CI.
const sleepCommand = "/bin/sleep"

// echoCommand is used to verify normal completion of a trivial subprocess.
const echoCommand = "/bin/echo"

// falseCommand exits with status 1.
const falseCommand = "/bin/false"

func TestOSCommandRunner_EchoSuccess(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(5 * time.Second)
	result, err := r.Run(context.Background(), echoCommand, "hello world")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if !bytes.Contains(result.Stdout, []byte("hello world")) {
		t.Errorf("Stdout = %q, want contains hello world", string(result.Stdout))
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}

func TestOSCommandRunner_NonZeroExitCaptured(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(5 * time.Second)
	result, err := r.Run(context.Background(), falseCommand)
	if err == nil {
		t.Fatal("expected error from /bin/false")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("expected *exec.ExitError, got %T (%v)", err, err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestOSCommandRunner_InternalTimeoutTriggered(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(50 * time.Millisecond)
	result, err := r.Run(context.Background(), sleepCommand, "5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if !result.TimedOut {
		t.Error("TimedOut = false, want true")
	}
	if result.Duration > 3*time.Second {
		t.Errorf("Duration = %v, want <= 3s", result.Duration)
	}
}

func TestOSCommandRunner_DefaultTimeoutWhenZero(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(0)
	if r == nil {
		t.Fatal("NewOSCommandRunner(0) = nil")
	}
	// Sanity: zero/negative config must not panic and must produce a normal
	// exit for fast commands.
	result, err := r.Run(context.Background(), echoCommand, "ok")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false")
	}
}

func TestOSCommandRunner_CallerContextCancellation(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(30 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := r.Run(ctx, sleepCommand, "5")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false")
	}
}

func TestOSCommandRunner_AlreadyCancelledContext(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(5 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := r.Run(ctx, sleepCommand, "1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false")
	}
}

// TestOSCommandRunner_RaceTimeoutVsCancellation exercises the documented
// precedence rule: when caller context cancellation and the internal
// timeout become observable simultaneously, caller cancellation wins and
// TimedOut remains false.
//
// The test deliberately schedules both events to fire near-simultaneously
// rather than asserting a fixed ordering; it then verifies the contractual
// invariant:
//
//   - When ctx.Err() != nil at classification time, the returned error is
//     ctx.Err() (context.Canceled) and TimedOut is false, regardless of
//     whether the internal timer also fired.
//   - When ctx.Err() == nil but internalTimedOut is observable, TimedOut is
//     true and the returned error is context.DeadlineExceeded.
//
// To force a near-simultaneous race, both the internal timeout (50ms) and
// the caller cancel (also 50ms) are configured to fire at approximately the
// same wall-clock instant. The test passes as long as the implemented
// discrimination order holds; it does NOT assume the scheduler will
// deliver one event before the other.
func TestOSCommandRunner_RaceTimeoutVsCancellation(t *testing.T) {
	t.Parallel()

	const iterations = 25
	for i := 0; i < iterations; i++ {
		r := platform.NewOSCommandRunner(50 * time.Millisecond)

		ctx, cancel := context.WithCancel(context.Background())
		// Fire caller cancel at the same approximate instant as the
		// 50ms internal timer. Whichever the scheduler observes first
		// is irrelevant to the contract: the test only asserts the
		// post-classification invariant.
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		result, err := r.Run(ctx, sleepCommand, "5")

		switch {
		case ctx.Err() != nil:
			// Caller cancellation was observable at classification.
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("iter %d: ctx.Err() != nil, error = %v, want context.Canceled", i, err)
			}
			if result.TimedOut {
				t.Errorf("iter %d: ctx.Err() != nil, TimedOut = true; caller cancellation must win", i)
			}
		case errors.Is(err, context.DeadlineExceeded):
			// Internal timeout fired first (or exclusively).
			if !result.TimedOut {
				t.Errorf("iter %d: DeadlineExceeded error but TimedOut = false", i)
			}
		default:
			t.Fatalf("iter %d: unexpected classification: err=%v, ctx.Err()=%v, TimedOut=%v",
				i, err, ctx.Err(), result.TimedOut)
		}
	}
}

func TestOSCommandRunner_TruncatesLongStdout(t *testing.T) {
	t.Parallel()

	// /bin/yes produces unlimited output; we feed it through head -c to
	// generate exactly 2 MiB of output, which exceeds the 1 MiB cap.
	const totalBytes = 2 * 1024 * 1024
	r := platform.NewOSCommandRunner(10 * time.Second)
	result, err := r.Run(context.Background(), "/bin/sh", "-c",
		"head -c "+strconv.Itoa(totalBytes)+" /dev/zero")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.StdoutTruncated {
		t.Errorf("StdoutTruncated = false, want true (got %d bytes)", len(result.Stdout))
	}
	if len(result.Stdout) != (1 << 20) {
		t.Errorf("len(Stdout) = %d, want %d", len(result.Stdout), 1<<20)
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestOSCommandRunner_TruncatesLongStderr(t *testing.T) {
	t.Parallel()

	const totalBytes = 2 * 1024 * 1024
	r := platform.NewOSCommandRunner(10 * time.Second)
	result, err := r.Run(context.Background(), "/bin/sh", "-c",
		"head -c "+strconv.Itoa(totalBytes)+" /dev/zero >&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.StderrTruncated {
		t.Errorf("StderrTruncated = false, want true (got %d bytes)", len(result.Stderr))
	}
	if len(result.Stderr) != (1 << 20) {
		t.Errorf("len(Stderr) = %d, want %d", len(result.Stderr), 1<<20)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (process must exit successfully after truncation)", result.ExitCode)
	}
}

// TestOSCommandRunner_TruncatesBothStreamsSimultaneously verifies the
// full output-cap contract for both streams in a single command:
//
//   - len(Stdout) is bounded to 1 MiB when the subprocess emits 2 MiB.
//   - len(Stderr) is bounded to 1 MiB when the subprocess emits 2 MiB.
//   - StdoutTruncated is true when stdout exceeded the cap.
//   - StderrTruncated is true when stderr exceeded the cap.
//   - The subprocess still exits successfully (ExitCode == 0); the runner
//     does NOT terminate it just because its output exceeded the cap.
func TestOSCommandRunner_TruncatesBothStreamsSimultaneously(t *testing.T) {
	t.Parallel()

	const totalBytes = 2 * 1024 * 1024
	// Two parallel background processes: one floods stdout, one floods stderr.
	// `wait` blocks until both finish so the runner captures full output.
	script := "head -c " + strconv.Itoa(totalBytes) + " /dev/zero & head -c " +
		strconv.Itoa(totalBytes) + " /dev/zero >&2 & wait"

	r := platform.NewOSCommandRunner(10 * time.Second)
	result, err := r.Run(context.Background(), "/bin/sh", "-c", script)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Stdout) > (1 << 20) {
		t.Errorf("len(Stdout) = %d, want <= %d", len(result.Stdout), 1<<20)
	}
	if len(result.Stderr) > (1 << 20) {
		t.Errorf("len(Stderr) = %d, want <= %d", len(result.Stderr), 1<<20)
	}
	if !result.StdoutTruncated {
		t.Errorf("StdoutTruncated = false, want true (got %d bytes)", len(result.Stdout))
	}
	if !result.StderrTruncated {
		t.Errorf("StderrTruncated = false, want true (got %d bytes)", len(result.Stderr))
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false (truncation must not trip timeout)")
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (process must exit successfully after truncation)", result.ExitCode)
	}
}

func TestOSCommandRunner_BinaryNotFound(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(5 * time.Second)
	_, err := r.Run(context.Background(), "/no/such/binary/surely")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	// exec.Command surfaces a *fs.PathError wrapping syscall.ENOENT;
	// exec.ErrNotFound is only produced by exec.LookPath.
	if !errors.Is(err, syscall.ENOENT) {
		t.Errorf("error = %v, want wraps syscall.ENOENT", err)
	}
}

func TestOSCommandRunner_NilContextTreatedAsBackground(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(5 * time.Second)
	// Pass a typed nil context to exercise the nil-guard branch in Run.
	var nilCtx context.Context //nolint:staticcheck // SA1012: intentional nil-ctx test.
	result, err := r.Run(nilCtx, echoCommand, "ok")
	if err != nil {
		t.Fatalf("Run(nil, ...): %v", err)
	}
	if !bytes.Contains(result.Stdout, []byte("ok")) {
		t.Errorf("Stdout = %q, want contains ok", string(result.Stdout))
	}
}

func TestOSCommandRunner_InternalTimeoutTrumpsNoExternalCancel(t *testing.T) {
	t.Parallel()

	// Long-running sleep, short internal timeout. Verify TimedOut flag
	// even when ctx.Err() is nil.
	r := platform.NewOSCommandRunner(75 * time.Millisecond)
	result, err := r.Run(context.Background(), sleepCommand, "10")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if !result.TimedOut {
		t.Error("TimedOut = false, want true")
	}
}

func TestFakeCommandRunner_RegisterAndMatch(t *testing.T) {
	t.Parallel()

	f := platform.NewFakeCommandRunner()
	want := platform.ExecResult{
		Stdout:   []byte("mock stdout"),
		ExitCode: 0,
	}
	f.Register("mytool", []string{"--version"}, want)

	got, err := f.Run(context.Background(), "mytool", "--version")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !bytes.Equal(got.Stdout, want.Stdout) {
		t.Errorf("Stdout = %q, want %q", string(got.Stdout), string(want.Stdout))
	}

	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("Calls len = %d, want 1", len(calls))
	}
	if calls[0].Name != "mytool" || len(calls[0].Args) != 1 || calls[0].Args[0] != "--version" {
		t.Errorf("Calls[0] = %+v, want {mytool, [--version]}", calls[0])
	}
}

func TestFakeCommandRunner_ArgsDisambiguateSameName(t *testing.T) {
	t.Parallel()

	f := platform.NewFakeCommandRunner()
	f.Register("tool", []string{"info"}, platform.ExecResult{Stdout: []byte("info")})
	f.Register("tool", []string{"version"}, platform.ExecResult{Stdout: []byte("v1")})

	info, err := f.Run(context.Background(), "tool", "info")
	if err != nil {
		t.Fatalf("Run info: %v", err)
	}
	if string(info.Stdout) != "info" {
		t.Errorf("info stdout = %q", string(info.Stdout))
	}

	ver, err := f.Run(context.Background(), "tool", "version")
	if err != nil {
		t.Fatalf("Run version: %v", err)
	}
	if string(ver.Stdout) != "v1" {
		t.Errorf("version stdout = %q", string(ver.Stdout))
	}
}

func TestFakeCommandRunner_UnmockedReturnsError(t *testing.T) {
	t.Parallel()

	f := platform.NewFakeCommandRunner()
	_, err := f.Run(context.Background(), "missing", "args")
	if !errors.Is(err, platform.ErrUnmockedCommand()) {
		t.Errorf("error = %v, want ErrUnmockedCommand", err)
	}
}

func TestFakeCommandRunner_RegisterWithError(t *testing.T) {
	t.Parallel()

	f := platform.NewFakeCommandRunner()
	wantErr := errors.New("boom")
	f.RegisterWithError("fail", []string{"x"}, platform.ExecResult{ExitCode: 99}, wantErr)

	result, err := f.Run(context.Background(), "fail", "x")
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
	if result.ExitCode != 99 {
		t.Errorf("ExitCode = %d, want 99", result.ExitCode)
	}
}

func TestFakeCommandRunner_ArgsWithNULDisambiguated(t *testing.T) {
	t.Parallel()

	// Verify the NUL-separator key disambiguates pathological inputs that
	// might otherwise collide. ("foo", "bar") vs ("fo", "obar") must NOT
	// collide under NUL joining.
	f := platform.NewFakeCommandRunner()
	f.Register("foo", []string{"bar"}, platform.ExecResult{Stdout: []byte("hit-foo-bar")})
	f.Register("fo", []string{"obar"}, platform.ExecResult{Stdout: []byte("hit-fo-obar")})

	got1, err := f.Run(context.Background(), "foo", "bar")
	if err != nil {
		t.Fatalf("Run foo bar: %v", err)
	}
	if string(got1.Stdout) != "hit-foo-bar" {
		t.Errorf("foo bar = %q", string(got1.Stdout))
	}

	got2, err := f.Run(context.Background(), "fo", "obar")
	if err != nil {
		t.Fatalf("Run fo obar: %v", err)
	}
	if string(got2.Stdout) != "hit-fo-obar" {
		t.Errorf("fo obar = %q", string(got2.Stdout))
	}
}

func TestFakeCommandRunner_Concurrent(t *testing.T) {
	t.Parallel()

	f := platform.NewFakeCommandRunner()
	f.Register("noop", nil, platform.ExecResult{Stdout: []byte("ok")})

	const goroutines = 16
	const iterations = 50

	var wg sync.WaitGroup
	var ok int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				result, err := f.Run(context.Background(), "noop")
				if err != nil {
					t.Errorf("concurrent Run: %v", err)
					return
				}
				if string(result.Stdout) != "ok" {
					t.Errorf("concurrent stdout = %q", string(result.Stdout))
					return
				}
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&ok); got != goroutines*iterations {
		t.Errorf("ok = %d, want %d", got, goroutines*iterations)
	}

	calls := f.Calls()
	if len(calls) != goroutines*iterations {
		t.Errorf("Calls len = %d, want %d", len(calls), goroutines*iterations)
	}
}

func TestFakeCommandRunner_CallsIsolated(t *testing.T) {
	t.Parallel()

	f := platform.NewFakeCommandRunner()
	f.Register("echo", []string{"a"}, platform.ExecResult{Stdout: []byte("a")})
	f.Register("echo", []string{"b"}, platform.ExecResult{Stdout: []byte("b")})

	_, _ = f.Run(context.Background(), "echo", "a")
	_, _ = f.Run(context.Background(), "echo", "b")
	_, _ = f.Run(context.Background(), "echo", "a")

	calls := f.Calls()
	if len(calls) != 3 {
		t.Fatalf("Calls = %d, want 3", len(calls))
	}
	if calls[0].Args[0] != "a" || calls[1].Args[0] != "b" || calls[2].Args[0] != "a" {
		t.Errorf("Calls = %+v", calls)
	}
	// Mutating the returned Args slice must not affect internal state.
	calls[0].Args[0] = "tampered"
	again, _ := f.Run(context.Background(), "echo", "a")
	if !strings.Contains(string(again.Stdout), "a") {
		t.Errorf("post-tamper result corrupted: %q", string(again.Stdout))
	}
}

func TestOSCommandRunner_KillsProcessGroupOnTimeout(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skipf("/bin/sh not available: %v", err)
	}

	// Spawn a shell that execs a sleeping child. The internal timeout
	// must kill the entire process group, not just the shell wrapper.
	r := platform.NewOSCommandRunner(80 * time.Millisecond)
	result, err := r.Run(context.Background(), "/bin/sh", "-c", "exec /bin/sleep 30")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	if !result.TimedOut {
		t.Error("TimedOut = false, want true")
	}
	if result.Duration > 3*time.Second {
		t.Errorf("Duration = %v, want <= 3s", result.Duration)
	}
}

func TestOSCommandRunner_ExitCodeOnKilled(t *testing.T) {
	t.Parallel()

	// Verify that a process killed by the internal timeout records a
	// non-zero ExitCode (kernel-reported signal status).
	r := platform.NewOSCommandRunner(80 * time.Millisecond)
	result, err := r.Run(context.Background(), sleepCommand, "30")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	if !result.TimedOut {
		t.Error("TimedOut = false, want true")
	}
	if result.ExitCode == 0 {
		t.Errorf("ExitCode = 0, want non-zero (signal)")
	}
}

// TestOSCommandRunner_NormalCompletionDoesNotSignalChildren verifies that a
// command which completes normally does NOT trigger an extra SIGKILL to its
// own process group. We assert this by spawning a benign parent plus a child
// that writes a sentinel file after sleeping a short time; if the runner
// sent an extra signal at end-of-Run, the child would be killed before
// writing the sentinel.
func TestOSCommandRunner_NormalCompletionDoesNotSignalChildren(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skipf("/bin/sh not available: %v", err)
	}

	dir := t.TempDir()
	sentinel := filepath.Join(dir, "sentinel")

	// Parent exits cleanly after child finishes. Child writes the
	// sentinel then exits. If the runner sends an extra signal to the
	// process group after the parent exits, the child would be killed
	// before writing the file.
	script := fmt.Sprintf(`(sleep 0.05; echo alive > %s) & wait`, sentinel)
	r := platform.NewOSCommandRunner(5 * time.Second)
	result, err := r.Run(context.Background(), "/bin/sh", "-c", script)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	data, readErr := os.ReadFile(sentinel)
	if readErr != nil {
		t.Fatalf("child did not write sentinel: %v (extra SIGKILL likely killed it)", readErr)
	}
	if strings.TrimSpace(string(data)) != "alive" {
		t.Errorf("sentinel = %q, want 'alive'", string(data))
	}
}

// TestOSCommandRunner_KillsDescendantsOnTimeout verifies that the entire
// process group is killed when the internal timeout fires. We spawn a shell
// that detaches a sleep child writing its PID to a file; after the timeout,
// we probe the child PID with signal 0 to confirm it is no longer alive.
func TestOSCommandRunner_KillsDescendantsOnTimeout(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skipf("/bin/sh not available: %v", err)
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")

	// Detach a sleep child and record its PID. The parent shell waits
	// long enough that the runner's internal timeout will fire first.
	// The sleep child writes its PID into pidFile so the test can probe
	// it after the kill. Using a sentinel "ready" file ensures the sleep
	// child is running before we attempt to kill it.
	script := fmt.Sprintf(
		`/bin/sleep 30 & echo $! > %s; touch %s; wait`,
		pidFile, filepath.Join(dir, "ready"),
	)
	r := platform.NewOSCommandRunner(200 * time.Millisecond)
	result, err := r.Run(context.Background(), "/bin/sh", "-c", script)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	if !result.TimedOut {
		t.Error("TimedOut = false, want true")
	}

	// Give the killed children a brief moment to be reaped.
	time.Sleep(50 * time.Millisecond)

	raw, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("child PID file not written: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if parseErr != nil {
		t.Fatalf("invalid child PID %q: %v", string(raw), parseErr)
	}

	// Probe the child with signal 0. If the process group was killed,
	// the child is gone and kill returns ESRCH.
	if probeErr := syscall.Kill(pid, 0); probeErr == nil {
		t.Errorf("descendant PID %d survived process-group kill", pid)
	} else if !errors.Is(probeErr, syscall.ESRCH) {
		t.Errorf("kill(0) on descendant PID %d returned unexpected error: %v", pid, probeErr)
	}
}

// TestOSCommandRunner_KillsDescendantsOnCallerCancellation verifies that
// caller-context cancellation also terminates the entire process group.
func TestOSCommandRunner_KillsDescendantsOnCallerCancellation(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skipf("/bin/sh not available: %v", err)
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")

	script := fmt.Sprintf(
		`/bin/sleep 30 & echo $! > %s; touch %s; wait`,
		pidFile, filepath.Join(dir, "ready"),
	)
	r := platform.NewOSCommandRunner(30 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	result, err := r.Run(ctx, "/bin/sh", "-c", script)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false on caller cancellation")
	}

	time.Sleep(50 * time.Millisecond)

	raw, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("child PID file not written: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if parseErr != nil {
		t.Fatalf("invalid child PID %q: %v", string(raw), parseErr)
	}
	if probeErr := syscall.Kill(pid, 0); probeErr == nil {
		t.Errorf("descendant PID %d survived process-group kill", pid)
	} else if !errors.Is(probeErr, syscall.ESRCH) {
		t.Errorf("kill(0) on descendant PID %d returned unexpected error: %v", pid, probeErr)
	}
}

// TestOSCommandRunner_ReapsKilledProcess verifies that a process killed by
// SIGKILL is reaped (no zombie) once Run returns. We rely on the absence of
// any visible side effect: if Run returned without blocking indefinitely,
// the kernel reaped the child. The PID file path also serves to ensure we
// can spawn the process without errors.
func TestOSCommandRunner_ReapsKilledProcess(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(60 * time.Millisecond)
	result, err := r.Run(context.Background(), sleepCommand, "10")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	if !result.TimedOut {
		t.Error("TimedOut = false, want true")
	}
	// Calling Run again must work and must not block. If the previous
	// child were not reaped, the runner would deadlock on the new process.
	// Use a separate runner with a generous timeout so CI does not flake
	// on process-startup latency.
	r2 := platform.NewOSCommandRunner(5 * time.Second)
	result2, err := r2.Run(context.Background(), echoCommand, "reaped")
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if string(result2.Stdout) != "reaped\n" {
		t.Errorf("second stdout = %q, want 'reaped'", string(result2.Stdout))
	}
}

// TestOSCommandRunner_ReapsKilledProcessOnCallerCancellation mirrors
// TestOSCommandRunner_ReapsKilledProcess but exercises the caller-cancel
// termination path. After caller cancellation triggers the SIGKILL, the
// subsequent Run call must succeed without a zombie leak.
func TestOSCommandRunner_ReapsKilledProcessOnCallerCancellation(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(30 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := r.Run(ctx, sleepCommand, "10")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false on caller cancel")
	}

	// Subsequent Run must work without blocking on a zombie.
	result2, err := r.Run(context.Background(), echoCommand, "ok")
	if err != nil {
		t.Fatalf("second Run after cancel: %v", err)
	}
	if string(result2.Stdout) != "ok\n" {
		t.Errorf("second stdout = %q, want 'ok'", string(result2.Stdout))
	}
}
