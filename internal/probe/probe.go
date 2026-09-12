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
// # Execution contract
//
// The orchestrator invokes a registered Probe instance at most once per
// Run; sibling probes may execute concurrently with one another. Because
// of that, the platform.Environment and its dependencies may be observed
// concurrently from multiple probe goroutines even though each individual
// Probe value is not re-entered within one Run. Concurrent Run calls (even
// through different registries) may share instances only if those probes are
// safely reentrant. Sequential reuse permits probe-local mutable state.
//
// Implementations therefore MUST NOT assume single-goroutine access to
// shared dependencies. Under sequential reuse they MAY mutate local state,
// but they MUST NOT mutate shared dependencies (e.g. platform.Environment
// services) unless those dependencies explicitly document that they are
// safe for concurrent mutation.
//
// # Cancellation contract
//
// Cancellation responsibilities are split between the orchestrator and the
// probe:
//
//   - Orchestrator: propagates a context.Context to every Probe.Run call
//     and classifies cooperative cancellation by inspecting the returned
//     error against ctx.Err().
//   - Probe: Probe.Run implementations MUST observe ctx.Done() and return
//     promptly when the supplied context is cancelled. The orchestrator
//     cannot forcibly terminate arbitrary Go code executing inside
//     Probe.Run; a probe that blocks indefinitely will block its worker
//     goroutine and prevent subsequent probes from being scheduled.
//     Returning ctx.Err() (verbatim or wrapped via fmt.Errorf("%w", ...)
//     or errors.Is) is classified as ProbeCancelled by the orchestrator.
type Probe interface {
	// ID returns the unique, stable identifier for this probe.
	// The returned string must be a valid, non-empty probe ID and must
	// remain constant for the lifetime of the probe instance.
	ID() string

	// Dependencies is captured and copied exactly once by the first Resolve.
	// Callers must not mutate declarations concurrently with that capture.
	// Dependencies returns the IDs of probes that must complete with
	// status ProbeSucceeded before this probe may run. An empty slice means
	// the probe has no dependencies.
	Dependencies() []string

	// Run executes under ctx and the explicit candidate in env.Scope(). Scope IDs
	// resolve to the application-owned context snapshot. Returned mutable payloads
	// are copied by the orchestrator; implementations must not mutate them during
	// return/transfer. The result is an Observation describing
	// the measurement. Returning a non-nil error does NOT mean the probe
	// was skipped; the orchestrator records the result with status
	// ProbeFailed. Cancellation via ctx surfaces as ProbeCancelled.
	Run(ctx context.Context, env platform.Environment) (model.Observation, error)
}
