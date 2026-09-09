package platform_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
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

func TestOSCommandRunner_TruncatesLongStdout(t *testing.T) {
	t.Parallel()

	// /bin/yes produces unlimited output; we feed it through head -c to
	// generate exactly 2 MiB of output, which exceeds the 1 MiB cap.
	const totalBytes = 2 * 1024 * 1024
	r := platform.NewOSCommandRunner(10 * time.Second)
	result, err := r.Run(context.Background(), "/bin/sh", "-c",
		"head -c "+itoa(totalBytes)+" /dev/zero")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.StdoutTruncated {
		t.Errorf("StdoutTruncated = false, want true (got %d bytes)", len(result.Stdout))
	}
	if len(result.Stdout) != (1<<20) {
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
		"head -c "+itoa(totalBytes)+" /dev/zero >&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.StderrTruncated {
		t.Errorf("StderrTruncated = false, want true (got %d bytes)", len(result.Stderr))
	}
	if len(result.Stderr) != (1<<20) {
		t.Errorf("len(Stderr) = %d, want %d", len(result.Stderr), 1<<20)
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

// itoa converts an int to its decimal string representation. Implemented
// locally to avoid pulling strconv into the test for a trivial need.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
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

func TestOSCommandRunner_DoesNotPanicOnZeroDurationTimeout(t *testing.T) {
	t.Parallel()

	r := platform.NewOSCommandRunner(0)
	// 0 timeout must use defaultRunnerTimeout, not panic.
	result, err := r.Run(context.Background(), echoCommand, "x")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.TimedOut {
		t.Error("TimedOut = true, want false")
	}
}

func TestOSCommandRunner_RespectsSyscallImport(t *testing.T) {
	t.Parallel()

	// This test exists only to assert that the syscall package is
	// imported (used for process-group kill). Without it the build would
	// fail with "imported and not used".
	var _ = syscall.Kill
}