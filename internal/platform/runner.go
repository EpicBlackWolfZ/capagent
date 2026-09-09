package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// maxRunnerOutputBytes is the per-stream output buffer cap. Streams that
// exceed this size are silently truncated; the corresponding ExecResult
// truncation flag is set to true.
const maxRunnerOutputBytes = 1 << 20

const (
	initialRunnerOutputBytes = 4 << 10
	outputGrowthFactor       = 2
)

// defaultRunnerTimeout is the internal subprocess timeout applied when
// OSCommandRunner.defaultTimeout is zero or negative.
const defaultRunnerTimeout = 30 * time.Second

// runnerWaitDelay bounds pipe draining after cancellation or direct-child exit.
// Escaped descendants may outlive this budget; closing pipes does not kill them.
const runnerWaitDelay = 250 * time.Millisecond

// ExecResult captures the bounded outcome of a subprocess invocation.
//
// The semantics of Truncated flags are: bytes beyond maxRunnerOutputBytes
// for that stream have been discarded. They do not certify complete output
// after an execution/drain error. ExitCode is meaningful only with the error:
// startup failure retains zero, as does successful exit with a drain error.
type ExecResult struct {
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
	ExitCode        int
	Duration        time.Duration
	TimedOut        bool
}

// CommandRunner is the canonical subprocess execution abstraction for capagent probes.
//
// Implementations MUST bound per-stream output, distinguish caller context
// cancellation from internal timeout, bound pipe draining, and reap their
// direct child. Process-group cleanup cannot terminate escaped descendants.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (ExecResult, error)
}

// OSCommandRunner is the production CommandRunner that delegates to os/exec.
type OSCommandRunner struct {
	// defaultTimeout is applied when no caller-provided context deadline is
	// earlier. Zero or negative means use defaultRunnerTimeout.
	defaultTimeout time.Duration
}

// NewOSCommandRunner constructs an OSCommandRunner with the supplied
// default internal timeout. A non-positive timeout falls back to
// defaultRunnerTimeout at run time.
func NewOSCommandRunner(defaultTimeout time.Duration) *OSCommandRunner {
	return &OSCommandRunner{defaultTimeout: defaultTimeout}
}

// boundedBuffer is a thread-safe, capacity-limited writer used as the
// stdout/stderr sink for OSCommandRunner. It accepts all writes (so the
// subprocess never blocks) but only retains the first capacity bytes and
// records that truncation occurred.
type boundedBuffer struct {
	mu        sync.Mutex
	data      []byte
	capacity  int
	truncated bool
}

func newBoundedBuffer(capacity int) *boundedBuffer {
	return &boundedBuffer{
		data:     make([]byte, 0, min(initialRunnerOutputBytes, capacity)),
		capacity: capacity,
	}
}

// Write accepts all bytes from p, storing only the first capacity bytes.
// It always returns (len(p), nil) so the subprocess's writes never block.
func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.truncated || len(p) == 0 {
		return len(p), nil
	}

	retained := min(len(p), b.capacity-len(b.data))
	needed := len(b.data) + retained
	if needed > cap(b.data) {
		// Explicit growth keeps backing capacity within the same hard limit
		// as retained length; append's automatic growth can overshoot it.
		capacity := min(b.capacity, max(needed, cap(b.data)*outputGrowthFactor))
		data := make([]byte, len(b.data), capacity)
		copy(data, b.data)
		b.data = data
	}
	b.data = append(b.data, p[:retained]...)
	b.truncated = retained < len(p)
	return len(p), nil
}

// Bytes returns a copy of the retained prefix.
func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}

// Truncated reports whether at least one byte beyond capacity was dropped.
func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// Run executes the named binary with a caller context and internal timeout.
// An observable caller cancellation returns ctx.Err() with TimedOut false;
// otherwise an internal timeout returns DeadlineExceeded with TimedOut true.
//
// Timeout vs. caller-cancellation precedence: when both the caller context
// and the internal timeout become observable before result classification,
// the caller context wins. TimedOut is set to true ONLY when the internal
// timeout is the selected termination reason; if the caller context
// cancelled first, TimedOut remains false even if the internal timer would
// subsequently have fired.
//
// Process-group cleanup: a SIGKILL is delivered to the entire process group
// ONLY when execution is interrupted (internal timeout or caller
// cancellation). The runner's CommandContext cancellation hook terminates
// owned process group; cmd.Run reaps the direct child. Normally completing
// commands are not signalled after completion. Descendants may still exist.
// Pipe draining ends within runnerWaitDelay of cancellation or observed child
// exit, subject to kernel/scheduling delays. A successful exit with expired
// drain returns exec.ErrWaitDelay; a nonzero exit retains its ExitError.
func (r *OSCommandRunner) Run(ctx context.Context, name string, args ...string) (ExecResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	timeout := r.defaultTimeout
	if timeout <= 0 {
		timeout = defaultRunnerTimeout
	}

	start := time.Now()

	// Internal context decoupled from parent so we can attribute the
	// cancellation source deterministically.
	internalCtx, internalCancel := context.WithCancel(context.Background())

	forwarded := make(chan struct{})
	defer func() {
		internalCancel()
		<-forwarded
	}()

	// Propagate caller cancellation into internalCtx.
	go func() {
		defer close(forwarded)
		select {
		case <-ctx.Done():
			internalCancel()
		case <-internalCtx.Done():
		}
	}()

	var internalTimedOut atomic.Bool
	timeoutDone := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		defer close(timeoutDone)
		internalTimedOut.Store(true)
		internalCancel()
	})

	cmd := exec.CommandContext(internalCtx, name, args...) //nolint:gosec // G204: CommandRunner is a subprocess abstraction layer.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = runnerWaitDelay
	// Override the default Cancel to terminate the entire process group
	// (not only the immediate child). Without this, a forked grand-child
	// inheriting the runner's stdout/stderr pipes can keep them open,
	// preventing cmd.Wait from returning after the immediate child has
	// been signalled.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Best-effort signal to the entire process group. The error is
		// intentionally ignored: ESRCH simply means the group is already
		// gone, and any other error is non-actionable here.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return cmd.Process.Kill()
	}

	stdoutBuf := newBoundedBuffer(maxRunnerOutputBytes)
	stderrBuf := newBoundedBuffer(maxRunnerOutputBytes)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	runErr := cmd.Run()

	// If the callback has started, join it before classifying its outcome.
	// This prevents a late timeout flag update after Run has returned.
	if !timer.Stop() {
		<-timeoutDone
	}

	result := ExecResult{
		Stdout:          stdoutBuf.Bytes(),
		Stderr:          stderrBuf.Bytes(),
		StdoutTruncated: stdoutBuf.Truncated(),
		StderrTruncated: stderrBuf.Truncated(),
		Duration:        time.Since(start),
	}

	if exitErr, ok := runErr.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		// Process exited non-zero; preserve original error wrapping but
		// still attribute cancellation source.
	}

	// Discrimination order: parent cancellation > internal timeout > exit
	// status error.
	//
	// Precedence rule: when both caller cancellation and the internal
	// timeout become observable before result classification, caller
	// cancellation wins. TimedOut is set to true ONLY when the internal
	// timeout is the selected termination reason; if the caller context
	// was cancelled first, TimedOut remains false even if the internal
	// timer would subsequently have fired.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if internalTimedOut.Load() {
		result.TimedOut = true
		return result, context.DeadlineExceeded
	}
	return result, runErr
}

// FakeCommandRunner is an in-memory CommandRunner used as a deterministic
// test double. Each registered (name, args...) tuple returns the
// preconfigured ExecResult with caller-owned copies of output bytes.
//
// FakeCommandRunner is safe for concurrent use.
type FakeCommandRunner struct {
	mu      sync.RWMutex
	results map[string]ExecResult
	errors  map[string]error
	calls   []FakeCall
}

// fakeSeparator is the NUL byte used to delimit command name and arguments
// when building FakeCommandRunner lookup keys. It cannot occur in shell
// arguments, ensuring unambiguous matching.
const fakeSeparator = "\x00"

// FakeCall records a single Run invocation for inspection in tests.
type FakeCall struct {
	Name string
	Args []string
}

// NewFakeCommandRunner constructs an empty FakeCommandRunner.
func NewFakeCommandRunner() *FakeCommandRunner {
	return &FakeCommandRunner{
		results: make(map[string]ExecResult),
		errors:  make(map[string]error),
	}
}

// fakeKey computes the lookup key for a (name, args...) tuple.
func fakeKey(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, name)
	parts = append(parts, args...)
	return strings.Join(parts, fakeSeparator)
}

// Register associates an ExecResult with the (name, args...) tuple, copying output bytes.
func (f *FakeCommandRunner) Register(name string, args []string, result ExecResult) {
	f.RegisterWithError(name, args, result, nil)
}

// RegisterWithError associates both an ExecResult and an error with the
// (name, args...) tuple. When err is non-nil, Run returns it verbatim.
func (f *FakeCommandRunner) RegisterWithError(name string, args []string, result ExecResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeKey(name, args)
	f.results[key] = cloneExecResult(result)
	f.errors[key] = err
}

// errUnmockedCommand is returned by FakeCommandRunner.Run when no result
// has been registered for the requested (name, args...) tuple.
var errUnmockedCommand = errors.New("fake command runner: no result registered for command")

// ErrUnmockedCommand exposes errUnmockedCommand for tests that need to
// distinguish unmocked calls from other failures via errors.Is.
func ErrUnmockedCommand() error {
	return errUnmockedCommand
}

// Run returns a copy of the registered result for (name, args...) and records the
// invocation for later inspection. Unmocked calls return ErrUnmockedCommand.
func (f *FakeCommandRunner) Run(ctx context.Context, name string, args ...string) (ExecResult, error) {
	f.mu.Lock()
	key := fakeKey(name, args)
	result, hasResult := f.results[key]
	err := f.errors[key]
	f.calls = append(f.calls, FakeCall{Name: name, Args: append([]string(nil), args...)})
	f.mu.Unlock()

	if !hasResult {
		return ExecResult{}, fmt.Errorf("%w: %s %v", errUnmockedCommand, name, args)
	}
	return cloneExecResult(result), err
}

// Calls returns a copy of the invocation log recorded so far.
func (f *FakeCommandRunner) Calls() []FakeCall {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]FakeCall, len(f.calls))
	for i, c := range f.calls {
		out[i] = FakeCall{
			Name: c.Name,
			Args: append([]string(nil), c.Args...),
		}
	}
	return out
}

// Compile-time guarantees that both runners satisfy the interface.
var (
	_ CommandRunner = (*OSCommandRunner)(nil)
	_ CommandRunner = (*FakeCommandRunner)(nil)
)

// cloneExecResult transfers output ownership without sharing mutable slices.
func cloneExecResult(result ExecResult) ExecResult {
	result.Stdout = append([]byte(nil), result.Stdout...)
	result.Stderr = append([]byte(nil), result.Stderr...)
	return result
}
