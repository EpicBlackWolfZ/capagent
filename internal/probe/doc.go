// Package probe implements the canonical probe interface and the
// dependency-aware orchestration engine for capagent.
//
// In accordance with capagent architecture, this package:
//
//   - Defines the Probe contract: each probe exposes an ID, declares its
//     dependencies on other probe IDs, and emits a structured Observation.
//   - Maintains a Registry with a strict state machine: Register is allowed
//     only before Resolve; subsequent Resolve calls are no-ops; once
//     resolved the registry is immutable.
//   - Resolves the dependency graph into a canonical topological order via
//     Kahn's algorithm. The deterministic tie-breaker prioritizes nodes by
//     their original registration order, ensuring byte-identical execution
//     plans across runs.
//   - Schedules probes concurrently through a worker pool whose upper bound
//     is min(maxConcurrency, number of registered probes). Workers block
//     on the condition-variable-backed queue while no probe is currently
//     runnable and wake when a probe becomes runnable. Probes become
//     runnable only after all declared dependencies succeed; failures and
//     cancellations propagate by marking transitive dependents as
//     ProbeSkipped (never as ProbeCancelled).
//   - Returns results in canonical DAG order regardless of completion times,
//     making output deterministic and reproducible.
//
// Execution and cancellation contract:
//
//   - A registered Probe instance is executed at most once per Orchestrator
//     Run; the orchestrator does not invoke the same Probe value concurrently
//     from multiple goroutines.
//   - Sibling probes may execute concurrently with one another. Because
//     Environment is a value whose fields are reference-typed, the
//     shared dependencies may be accessed concurrently by probe goroutines.
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
package probe
