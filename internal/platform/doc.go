// Package platform provides the low-level OS abstraction layer for capagent.
//
// In accordance with capagent architecture, this package isolates all interactions
// with the underlying host operating system behind deterministic, testable
// interfaces. It depends solely on internal/model, the Go standard library,
// and golang.org/x/sys/unix for kernel-confined filesystem operations. The
// package contains zero container, runtime, or business-logic interpretations.
//
// The package exposes:
//
//   - PlatformReader: a deterministic filesystem abstraction over file reads,
//     bounded directory enumeration, immutable ownership metadata, raw file
//     capability attributes, and symlink targets. Both a real
//     implementation (OSPlatformReader) and an in-memory test double
//     (MemPlatformReader) are provided.
//   - ScopedReader: a root-confined filesystem abstraction that performs each
//     operation through a kernel-enforced containment boundary (Linux 5.6+
//     openat2(2) with RESOLVE_IN_ROOT | RESOLVE_NO_MAGICLINKS on the OS
//     reader; explicit lexical + per-hop checks on the memory reader). The
//     reader owns its root FD for its lifetime and releases it via Close().
//   - ProcfsReader / SysfsReader: thin parsers of Linux pseudo-filesystem
//     protocols built on top of a ScopedReader, returning pure transport
//     structs with bounded completeness diagnostics. Nil error means complete;
//     partial records carry an error and cannot establish absence.
//     Context cancellation is cooperative between syscalls; it cannot forcibly
//     interrupt arbitrary filesystem or device-driver I/O.
//   - CommandSpec / EnvPolicy: absolute executables, literal arguments, explicit
//     working directories/timeouts and immutable allowlisted environment snapshots.
//     Credentials and namespaces remain inherited; these APIs are not a sandbox.
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
// group and reaps its direct child. Escaped descendants may survive. Completing commands are
// not signalled after completion.
package platform
