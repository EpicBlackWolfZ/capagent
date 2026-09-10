package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
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
	Run(ctx context.Context, spec CommandSpec) (ExecResult, error)
}

// OSCommandRunner is the production CommandRunner that delegates to os/exec.
type OSCommandRunner struct {
	// defaultTimeout is used when CommandSpec.Timeout is zero. A caller deadline
	// can end execution earlier. Non-positive means use defaultRunnerTimeout.
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

// Run executes an absolute binary with explicit environment, directory and timeout.
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
func (r *OSCommandRunner) Run(ctx context.Context, spec CommandSpec) (ExecResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := ctx.Err(); err != nil {
		return ExecResult{}, err
	}
	prepared, err := spec.normalized()
	if err != nil {
		return ExecResult{}, err
	}
	spec = prepared
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = r.defaultTimeout
	}
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

	cmd := exec.CommandContext(internalCtx, spec.Path, spec.Args...) //nolint:gosec // G204: CommandRunner is a subprocess abstraction layer.
	cmd.Env = spec.Env.Variables()
	cmd.Dir = spec.Dir
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
	return result, safeCommandError(runErr)
}

// FakeCommandRunner is an in-memory CommandRunner used as a deterministic
// test double. Each registered command specification returns the
// preconfigured ExecResult with caller-owned copies of output bytes.
//
// FakeCommandRunner is safe for concurrent use.
type FakeCommandRunner struct {
	mu      sync.RWMutex
	results map[string]ExecResult
	errors  map[string]error
	calls   []FakeCall
}

// FakeCall records a validated invocation. Specification contents are sensitive;
// ordinary formatting uses CommandSpec's redacted representation.
type FakeCall struct{ Spec CommandSpec }

// NewFakeCommandRunner constructs an empty FakeCommandRunner.
func NewFakeCommandRunner() *FakeCommandRunner {
	return &FakeCommandRunner{results: make(map[string]ExecResult), errors: make(map[string]error)}
}

// fakeKey uses length-prefixed fields, including counts for variable-length
// lists. This distinguishes empty arguments and field/list boundaries exactly.
// Timeout zero remains a declaration of the runner default, not an explicit 30s.
func fakeKey(spec CommandSpec) string {
	var key strings.Builder
	field := func(s string) { key.WriteString(strconv.Itoa(len(s))); key.WriteByte(':'); key.WriteString(s) }
	field(spec.Path)
	field(spec.Dir)
	field(strconv.FormatInt(int64(spec.Timeout), 10))
	field(strconv.Itoa(len(spec.Args)))
	for _, arg := range spec.Args {
		field(arg)
	}
	env := spec.Env.Variables()
	field(strconv.Itoa(len(env)))
	for _, variable := range env {
		field(variable)
	}
	return key.String()
}

// Register validates the full specification and copies the configured result.
func (f *FakeCommandRunner) Register(spec CommandSpec, result ExecResult) error {
	return f.RegisterWithError(spec, result, nil)
}

// RegisterWithError stores a result and error for the full specification.
// The injected error is retained as a cause behind redacted diagnostic formatting.
func (f *FakeCommandRunner) RegisterWithError(spec CommandSpec, result ExecResult, err error) error {
	prepared, validationErr := spec.normalized()
	if validationErr != nil {
		return validationErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeKey(prepared)
	f.results[key] = cloneExecResult(result)
	f.errors[key] = err
	return nil
}

// errUnmockedCommand is returned by FakeCommandRunner.Run when no result
// has been registered for the complete requested specification.
var errUnmockedCommand = errors.New("fake command runner: no result registered for command")

// ErrUnmockedCommand exposes errUnmockedCommand for tests that need to
// distinguish unmocked calls from other failures via errors.Is.
func ErrUnmockedCommand() error {
	return errUnmockedCommand
}

// Run validates and records the full specification without reading host state.
// Invalid or already-cancelled calls are rejected before recording an invocation.
func (f *FakeCommandRunner) Run(ctx context.Context, spec CommandSpec) (ExecResult, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return ExecResult{}, err
		}
	}
	prepared, err := spec.normalized()
	if err != nil {
		return ExecResult{}, err
	}
	f.mu.Lock()
	key := fakeKey(prepared)
	result, hasResult := f.results[key]
	runErr := f.errors[key]
	f.calls = append(f.calls, FakeCall{Spec: prepared})
	f.mu.Unlock()
	if !hasResult {
		return ExecResult{}, errUnmockedCommand
	}
	return cloneExecResult(result), safeCommandError(runErr)
}

// Calls returns a deep copy of the invocation log; raw fields are for trusted
// test assertions only, not diagnostics or fixture export without sanitization.
func (f *FakeCommandRunner) Calls() []FakeCall {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]FakeCall, len(f.calls))
	for i, c := range f.calls {
		out[i] = FakeCall{Spec: cloneCommandSpec(c.Spec)}
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
