package probe

import (
	"context"
	"errors"

	"github.com/EpicBlackWolfZ/capagent/internal/model"
	"github.com/EpicBlackWolfZ/capagent/internal/platform"
)

var (
	// ErrDependencyFailed is returned by Orchestrator when a probe's
	// dependency has failed, been cancelled, or been skipped. Dependents
	// receive this error verbatim with status ProbeSkipped.
	ErrDependencyFailed = errors.New("probe dependency failed or skipped")

	// ErrRegistryResolved is returned by Registry.Register when called
	// after the registry has been resolved. Once resolved the registry is
	// immutable; subsequent Register calls are rejected to preserve the
	// canonical execution plan.
	ErrRegistryResolved = errors.New("registry is resolved and immutable")

	// ErrCycleDetected is returned by Registry.Resolve when the declared
	// dependencies form a cycle (including self-dependency). Cyclic graphs
	// cannot be topologically ordered and must be rejected up front.
	ErrCycleDetected = errors.New("dependency cycle detected")
)

// Probe is the canonical measurement interface for capagent.
//
// A probe is identified by a unique ID, declares zero or more dependencies
// on other probe IDs, and produces an Observation describing what it measured.
//
// Implementations MUST be safe for concurrent use by the orchestrator;
// multiple probes from different goroutines may call into the same shared
// platform.Environment concurrently.
//
// Cancellation responsibilities are split between the orchestrator and the
// probe:
//
//   - Orchestrator: propagates a context.Context to every Probe.Run call,
//     and classifies cooperative cancellation by inspecting the returned
//     error against ctx.Err().
//   - Probe: Probe.Run implementations MUST observe ctx.Done() and return
//     promptly when the supplied context is cancelled. The orchestrator
//     cannot forcibly terminate arbitrary Go code executing inside
//     Probe.Run; a probe that blocks indefinitely will block its worker
//     goroutine and prevent subsequent probes from being scheduled.
//     Returning ctx.Err() (verbatim or wrapped via fmt.Errorf("%w", ...)
//     or errors.Is) is classified as ProbeCancelled by the orchestrator.
//
// Probes MUST NOT mutate shared dependencies (e.g. platform.Environment
// fields) unless those dependencies explicitly support concurrent mutation.
// The Environment fields are reference-typed: probes receive pointers that
// may be observed by sibling probes.
type Probe interface {
	// ID returns the unique, stable identifier for this probe.
	// The returned string must be a valid, non-empty probe ID and must
	// remain constant for the lifetime of the probe instance.
	ID() string

	// Dependencies returns the IDs of probes that must complete with
	// status ProbeSucceeded before this probe may run. An empty slice means
	// the probe has no dependencies.
	Dependencies() []string

	// Run executes the probe under ctx and returns an Observation describing
	// the measurement. Returning a non-nil error does NOT mean the probe
	// was skipped; the orchestrator records the result with status
	// ProbeFailed. Cancellation via ctx surfaces as ProbeCancelled.
	Run(ctx context.Context, env platform.Environment) (model.Observation, error)
}
