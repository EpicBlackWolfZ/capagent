package platform_test

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

// fdZeroHelperEnv is the env var that signals "this process is the
// subprocess helper for FD-0 testing". When set to "1", the test
// re-enters helper mode instead of running the parent assertions.
const fdZeroHelperEnv = "BE_CAPAGENT_FDZERO_HELPER"

// fdZeroHelperOK is the marker line the subprocess helper prints on
// stdout when its assertions succeed. The parent test scans for this
// token.
const fdZeroHelperOK = "CAPAGENT_FD_ZERO_OK"

// TestScopedOSReader_FDZeroLifecycle verifies that ScopedOSReader
// treats FD 0 as a valid kernel-returned descriptor.
//
// The test runs in two modes:
//
//  1. Helper subprocess mode (env var set): close FD 0 (stdin), then
//     construct and exercise a ScopedOSReader.
//  2. Parent mode (default): spawn ourselves as a subprocess with the
//     env var set, then assert that the helper printed the success
//     marker.
//
// We use a subprocess so the parent test process is never affected by
// closing stdin.
func TestScopedOSReader_FDZeroLifecycle(t *testing.T) {
	t.Parallel()

	if os.Getenv(fdZeroHelperEnv) == "1" {
		runFDZeroHelper(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestScopedOSReader_FDZeroLifecycle")
	cmd.Env = append(os.Environ(), fdZeroHelperEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\nOutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), fdZeroHelperOK) {
		t.Fatalf("subprocess did not report success marker %q.\nOutput:\n%s",
			fdZeroHelperOK, out)
	}
}

// runFDZeroHelper is the subprocess payload. It closes FD 0 (stdin),
// then constructs a ScopedOSReader, exercises every method, and
// verifies the post-Close contract.
//
// The helper prints fdZeroHelperOK on stdout exactly once on success
// and calls t.Fatal on any failure.
func runFDZeroHelper(t *testing.T) {
	// Close FD 0 (stdin) so the next openat2 allocation may
	// legitimately receive FD 0.
	if err := syscall.Close(0); err != nil {
		t.Fatalf("close FD 0: %v", err)
	}
	// Confirm FD 0 is actually free; any other FD number means
	// the test environment has a non-stdin FD on 0 and we cannot
	// validate the FD-0 semantics in this run.
	if _, err := unix.FcntlInt(0, syscall.F_GETFD, 0); err == nil {
		t.Skipf("FD 0 is still allocated by something; cannot validate FD-0 semantics")
	}

	dir := t.TempDir()
	r, err := platform.NewScopedOSReader(dir)
	if err != nil {
		t.Fatalf("NewScopedOSReader with FD 0 available: %v", err)
	}

	// Every method must work; missing/empty subpaths produce
	// expected errors that are NOT ErrClosed.
	if _, err := r.ReadFile(t.Context(), "does-not-exist"); err == nil {
		t.Errorf("ReadFile missing: expected error, got nil")
	} else if errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadFile missing: ErrClosed returned prematurely (FD 0 handling bug)")
	}
	if _, err := r.Stat("does-not-exist"); err == nil {
		t.Errorf("Stat missing: expected error, got nil")
	} else if errors.Is(err, platform.ErrClosed) {
		t.Errorf("Stat missing: ErrClosed returned prematurely (FD 0 handling bug)")
	}
	if _, err := r.ReadDir(t.Context(), "does-not-exist"); err == nil {
		t.Errorf("ReadDir missing: expected error, got nil")
	} else if errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadDir missing: ErrClosed returned prematurely (FD 0 handling bug)")
	}
	if _, err := r.Readlink("does-not-exist"); err == nil {
		t.Errorf("Readlink missing: expected error, got nil")
	} else if errors.Is(err, platform.ErrClosed) {
		t.Errorf("Readlink missing: ErrClosed returned prematurely (FD 0 handling bug)")
	}

	// Root() must remain valid.
	if got := r.Root(); got != dir {
		t.Errorf("Root() = %q, want %q", got, dir)
	}

	// Close the reader. Subsequent operations must return ErrClosed.
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := r.ReadFile(t.Context(), "x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadFile after Close error = %v, want ErrClosed", err)
	}
	if _, err := r.Stat("x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("Stat after Close error = %v, want ErrClosed", err)
	}
	if _, err := r.ReadDir(t.Context(), "x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("ReadDir after Close error = %v, want ErrClosed", err)
	}
	if _, err := r.Readlink("x"); !errors.Is(err, platform.ErrClosed) {
		t.Errorf("Readlink after Close error = %v, want ErrClosed", err)
	}
	if got := r.Root(); got != dir {
		t.Errorf("Root() after Close = %q, want %q", got, dir)
	}

	// Idempotent close.
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v (idempotent contract violated)", err)
	}

	os.Stdout.WriteString(fdZeroHelperOK + "\n")
}
