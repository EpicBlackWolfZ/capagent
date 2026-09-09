// Package platform provides the low-level OS abstraction layer for capagent.
//
// In accordance with capagent architecture, this package isolates all interactions
// with the underlying host operating system behind deterministic, testable
// interfaces. It depends solely on internal/model and the Go standard library,
// and contains zero container, runtime, or business-logic interpretations.
//
// The package exposes:
//
//   - PlatformReader: a deterministic filesystem abstraction over file reads,
//     directory enumeration, stat metadata, and symlink targets. Both a real
//     implementation (OSPlatformReader) and an in-memory test double
//     (MemPlatformReader) are provided.
//   - ProcfsReader / SysfsReader: thin parsers of Linux pseudo-filesystem
//     protocols built on top of a PlatformReader, returning pure transport
//     structs without inferring runtime or container semantics.
//   - CommandRunner: a bounded subprocess execution abstraction with strict
//     per-stream output caps, internal timeout handling, cancellation
//     discrimination, and per-subprocess process-group cleanup. Both a real
//     implementation (OSCommandRunner) and a deterministic test double
//     (FakeCommandRunner) are provided.
//   - Environment: a unified injection struct that bundles a PlatformReader,
//     ProcfsReader, SysfsReader, and CommandRunner for probe execution.
//
// OSCommandRunner creates each subprocess in its own process group via
// Setpgid. When timeout or caller cancellation interrupts execution, the
// runner's CommandContext cancellation hook terminates the entire process
// group and waits for it to be reaped. Normally completing commands are
// not signalled after completion.
package platform
