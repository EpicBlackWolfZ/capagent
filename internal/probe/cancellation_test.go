package probe_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
	"github.com/EpicBlackWolfZ/capagent/internal/probe"
)

// TestOrchestrator_CancellationBeforeExecution verifies that a probe whose
// context is already cancelled before Run begins is recorded as
// ProbeCancelled without invoking the probe's Run function.
func TestOrchestrator_CancellationBeforeExecution(t *testing.T) {
	t.Parallel()

	var ran atomic.Int32
	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "never-runs",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			ran.Add(1)
			return model.Observation{}, nil
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(ctx, newEnv())
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Status != probe.ProbeCancelled {
		t.Errorf("Status = %v, want ProbeCancelled", results[0].Status)
	}
	if ran.Load() != 0 {
		t.Errorf("probe.Run invoked %d times, want 0 (context was already cancelled)", ran.Load())
	}
}

// TestOrchestrator_CancellationReturnsWrappedErr verifies that a probe which
// returns fmt.Errorf("%w", ctx.Err()) is classified as ProbeCancelled.
func TestOrchestrator_CancellationReturnsWrappedErr(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "wrap",
		startHook: func() {
			close(started)
		},
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, fmt.Errorf("interrupted: %w", ctx.Err())
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(1))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	done := make(chan []probe.ProbeResult, 1)
	go func() {
		done <- o.Run(ctx, newEnv())
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start in time")
	}
	cancel()

	var results []probe.ProbeResult
	select {
	case results = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}

	if results[0].Status != probe.ProbeCancelled {
		t.Errorf("Status = %v, want ProbeCancelled", results[0].Status)
	}
	if !errors.Is(results[0].Err, context.Canceled) {
		t.Errorf("Err = %v, want wraps context.Canceled", results[0].Err)
	}
}

// TestOrchestrator_CancellationReturnsDirectErr verifies that a probe which
// returns ctx.Err() directly (no wrapping) is classified as ProbeCancelled.
func TestOrchestrator_CancellationReturnsDirectErr(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "direct",
		startHook: func() {
			close(started)
		},
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(1))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	done := make(chan []probe.ProbeResult, 1)
	go func() {
		done <- o.Run(ctx, newEnv())
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start in time")
	}
	cancel()

	var results []probe.ProbeResult
	select {
	case results = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}

	if results[0].Status != probe.ProbeCancelled {
		t.Errorf("Status = %v, want ProbeCancelled", results[0].Status)
	}
	if !errors.Is(results[0].Err, context.Canceled) {
		t.Errorf("Err = %v, want wraps context.Canceled", results[0].Err)
	}
}

// TestOrchestrator_CancelledDependencySkipsDependents verifies that a
// cancelled probe propagates ProbeSkipped + ErrDependencyFailed to its
// dependents (not ProbeCancelled).
func TestOrchestrator_CancelledDependencySkipsDependents(t *testing.T) {
	t.Parallel()

	aStarted := make(chan struct{})

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "A",
		startHook: func() {
			close(aStarted)
		},
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "B", deps: []string{"A"}}); err != nil {
		t.Fatalf("Register B: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(1))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	done := make(chan []probe.ProbeResult, 1)
	go func() {
		done <- o.Run(ctx, newEnv())
	}()

	select {
	case <-aStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("A did not start in time")
	}
	cancel()

	var results []probe.ProbeResult
	select {
	case results = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}

	a := findResult(results, "A")
	if a == nil || a.Status != probe.ProbeCancelled {
		t.Errorf("A.Status = %v, want ProbeCancelled", a)
	}
	b := findResult(results, "B")
	if b == nil || b.Status != probe.ProbeSkipped {
		t.Errorf("B.Status = %v, want ProbeSkipped", b)
	}
	if !errors.Is(b.Err, probe.ErrDependencyFailed) {
		t.Errorf("B.Err = %v, want ErrDependencyFailed", b.Err)
	}
}

// TestOrchestrator_IndependentBranchContinuesAfterCancellation is the
// deterministic counterpart to the weaker
// TestOrchestrator_IndependentBranchesContinueAfterCancellation. It uses
// explicit start/completion signaling rather than time.Sleep to prove
// the stronger invariant:
//
//   - An independent probe that has been observed running at the moment
//     of caller cancellation is still permitted to complete normally and
//     be classified as ProbeSucceeded.
//
// Steps:
//
//  1. Register A (cancellable) and X (independent, blocks on release).
//  2. Wait until X is running (xStarted).
//  3. Cancel the context. A must observe cancellation and become
//     ProbeCancelled.
//  4. Release X. X must complete and be classified as ProbeSucceeded,
//     even though the context is already cancelled.
func TestOrchestrator_IndependentBranchContinuesAfterCancellation(t *testing.T) {
	t.Parallel()

	xStarted := make(chan struct{})
	xRelease := make(chan struct{})

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "A",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{
		id: "X",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			close(xStarted)
			select {
			case <-xRelease:
				return model.Observation{ID: "X", ProbeID: "X"}, nil
			case <-ctx.Done():
				return model.Observation{ID: "X", ProbeID: "X"}, nil
			}
		},
	}); err != nil {
		t.Fatalf("Register X: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	done := make(chan []probe.ProbeResult, 1)
	go func() {
		done <- o.Run(ctx, newEnv())
	}()

	// Step 2: wait until X is actually running.
	select {
	case <-xStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("X did not start in time")
	}

	// Step 3: cancel. X is still blocked on xRelease so cancel does not
	// cause its run to return yet; A will observe the cancellation.
	cancel()

	// Step 4: release X. X completes normally regardless of the already
	// cancelled context.
	close(xRelease)

	// Collect results.
	var results []probe.ProbeResult
	select {
	case results = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}

	for _, res := range results {
		switch res.ProbeID {
		case "A":
			if res.Status != probe.ProbeCancelled {
				t.Errorf("A.Status = %v, want ProbeCancelled", res.Status)
			}
			if !errors.Is(res.Err, context.Canceled) {
				t.Errorf("A.Err = %v, want wraps context.Canceled", res.Err)
			}
		case "X":
			if res.Status != probe.ProbeSucceeded {
				t.Errorf("X.Status = %v, want ProbeSucceeded", res.Status)
			}
		}
	}
}

// TestOrchestrator_CancellationFanOutGraph is a compact end-to-end test
// that verifies the full cancellation classification in a single graph:
//
//	┌→ B
//	A ─┤
//	└→ C
//
//	X  (independent sibling)
//
// A is directly cancelled by caller context, so its status is ProbeCancelled.
// B and C are direct dependents of A and must become ProbeSkipped (NOT
// ProbeCancelled). X is an unrelated sibling registered before A so it has
// plenty of time to complete normally; X must succeed to prove that
// cancellation does not globally poison unrelated work.
//
// This single test consolidates three distinct invariants:
//
//  1. Directly interrupted probe  -> ProbeCancelled.
//  2. Direct dependent of cancelled -> ProbeSkipped with ErrDependencyFailed.
//  3. Unrelated sibling             -> ProbeSucceeded (not poisoned).
//
// Synchronization: X uses a completeHook to signal completion. The cancel
// goroutine waits on that signal so cancellation is applied AFTER X has
// already finished, eliminating any reliance on wall-clock sleeps.
func TestOrchestrator_CancellationFanOutGraph(t *testing.T) {
	t.Parallel()

	xDone := make(chan struct{})

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{id: "A",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register A: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "B", deps: []string{"A"}}); err != nil {
		t.Fatalf("Register B: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "C", deps: []string{"A"}}); err != nil {
		t.Fatalf("Register C: %v", err)
	}
	if err := r.Register(&fakeProbe{
		id: "X",
		completeHook: func(_ probe.ProbeResult) {
			close(xDone)
		},
	}); err != nil {
		t.Fatalf("Register X: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	o, err := probe.NewOrchestrator(r, probe.WithMaxConcurrency(4))
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	done := make(chan []probe.ProbeResult, 1)
	go func() {
		done <- o.Run(ctx, newEnv())
	}()

	// Wait for X to actually complete before cancelling. The remaining
	// probes (A, B, C) are processed deterministically from this point.
	select {
	case <-xDone:
	case <-time.After(2 * time.Second):
		t.Fatal("X did not complete in time")
	}
	cancel()

	var results []probe.ProbeResult
	select {
	case results = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}

	want := map[string]struct {
		status probe.ProbeStatus
		errIs  error
	}{
		"A": {status: probe.ProbeCancelled, errIs: context.Canceled},
		"B": {status: probe.ProbeSkipped, errIs: probe.ErrDependencyFailed},
		"C": {status: probe.ProbeSkipped, errIs: probe.ErrDependencyFailed},
		"X": {status: probe.ProbeSucceeded},
	}
	for _, res := range results {
		w, ok := want[res.ProbeID]
		if !ok {
			t.Errorf("unexpected probe result: %s", res.ProbeID)
			continue
		}
		if res.Status != w.status {
			t.Errorf("%s.Status = %v, want %v", res.ProbeID, res.Status, w.status)
		}
		if w.errIs != nil && !errors.Is(res.Err, w.errIs) {
			t.Errorf("%s.Err = %v, want wraps %v", res.ProbeID, res.Err, w.errIs)
		}
	}
}
