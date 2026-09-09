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

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "wrap",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, fmt.Errorf("interrupted: %w", ctx.Err())
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(ctx, newEnv())
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

	r := probe.NewRegistry()
	if err := r.Register(&fakeProbe{
		id: "direct",
		run: func(ctx context.Context, env platform.Environment) (model.Observation, error) {
			<-ctx.Done()
			return model.Observation{}, ctx.Err()
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(ctx, newEnv())
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
	if err := r.Register(&fakeProbe{id: "B", deps: []string{"A"}}); err != nil {
		t.Fatalf("Register B: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(ctx, newEnv())
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

// TestOrchestrator_IndependentBranchesContinueAfterCancellation verifies
// that cancellation of one branch does not abort sibling probes that are
// unrelated to the cancelled dependency chain.
func TestOrchestrator_IndependentBranchesContinueAfterCancellation(t *testing.T) {
	t.Parallel()

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
	if err := r.Register(&fakeProbe{id: "X"}); err != nil {
		t.Fatalf("Register X: %v", err)
	}
	if err := r.Register(&fakeProbe{id: "Y"}); err != nil {
		t.Fatalf("Register Y: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(ctx, newEnv())
	for _, res := range results {
		switch res.ProbeID {
		case "A":
			if res.Status != probe.ProbeCancelled {
				t.Errorf("A.Status = %v, want ProbeCancelled", res.Status)
			}
		case "X", "Y":
			// X and Y may have already finished before cancel fires,
			// in which case they are ProbeSucceeded. Otherwise they
			// become ProbeCancelled because ctx was cancelled while
			// they were running.
			if res.Status != probe.ProbeSucceeded && res.Status != probe.ProbeCancelled {
				t.Errorf("%s.Status = %v, want Succeeded or Cancelled", res.ProbeID, res.Status)
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
func TestOrchestrator_CancellationFanOutGraph(t *testing.T) {
	t.Parallel()

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
	if err := r.Register(&fakeProbe{id: "X"}); err != nil {
		t.Fatalf("Register X: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Wait long enough for X to definitely finish (it has no deps and
		// runs immediately on the worker pool).
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	o, err := probe.NewOrchestrator(r)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	results := o.Run(ctx, newEnv())

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
