# Container Capability Engine Architecture

This document defines the architectural boundaries, domain model, evaluation semantics, package structure, and contract specifications for `capagent`.

## Implementation status and planned integration

This document describes the current foundation and target architecture. The [M1.2 fixture slice](fixture-evaluation.md) implements the first complete evaluation path. Live host/runtime collection remains future work; M1.1 confinement, command authority and lifecycle contracts apply to every adapter. See [roadmap.md](roadmap.md) and [security.md](security.md) for limits.

The planned application/composition layer in [#61](https://github.com/EpicBlackWolfZ/capagent/issues/61) owns context selection, dependency construction, evaluation order and resource teardown after workers join. It sits above the pure engines and probe framework. `cmd/capagent` remains flags/formatting only, and `internal/probe` remains limited to model/platform dependencies. Today environments are caller-owned; `Orchestrator.Run` does not create or close them.

The current identity booleans and minimal observations are structural scaffolding. [#59](https://github.com/EpicBlackWolfZ/capagent/issues/59) and [#64](https://github.com/EpicBlackWolfZ/capagent/issues/64) add explicit scope, typed payloads and uncertainty/completeness before consumers rely on them. Never treat an unobserved zero value as measured negative evidence. The `context` capability namespace and UID/GID schema bounds remain pending contract alignment.

---

## 1. Architectural Overview

`capagent` transforms raw system facts into authoritative, context-aware capability declarations and deterministic requirement evaluations.

### 1.1 Conceptual Pipeline

```text
Facts
  │  (Raw kernel, filesystem, and binary inspection)
  ▼
Observations
  │  (Structured outputs from isolated probes)
  ▼
Evidence
  │  (Normalized, verified claims contextualized by identity)
  ▼
Capabilities
  │  (Canonical, high-level operational features with states & confidence)
  ▼
Requirements
  │  (3-valued Boolean expressions evaluated against capabilities)
  ▼
Deployment Decision
     (Actionable verdicts: SATISFIED, UNSATISFIED, INDETERMINATE)
```

### 1.2 System Component Topology

```text
                         Deployment Requirements
                                  │
                                  ▼
                         ┌──────────────────┐
                         │ Requirement      │
                         │ Evaluator        │
                         └────────┬─────────┘
                                  │
                                  ▼
                         ┌──────────────────┐
                         │ Capability       │
                         │ Engine           │
                         └────────┬─────────┘
                                  │
                 ┌────────────────┼────────────────┐
                 │                │                │
                 ▼                ▼                ▼
           Host Evidence    Runtime Evidence   Config Evidence
                 │                │                │
                 └────────────────┼────────────────┘
                                  │
                                  ▼
                         ┌──────────────────┐
                         │ Evaluation       │
                         │ Context          │
                         └────────┬─────────┘
                                  │
                 ┌────────────────┼─────────────────┐
                 ▼                ▼                 ▼
                root          target user       container
```

---

## 2. Core Domain Model & Terminology

| Term | Definition |
| :--- | :--- |
| **Fact** | A raw, uninterpreted measurement directly retrieved from the host environment (e.g. file content in `/proc/sys/kernel/osrelease`, stat bits on a path, stdout of a command). |
| **Observation** | A structured datum produced by an individual `Probe` summarizing a specific aspect of the environment. |
| **Evidence** | An authoritative claim supported by one or more observations, categorized by source (live probe, runtime state, configuration, historical knowledge, or heuristic). |
| **Capability** | A canonical, high-level feature (e.g. `container.lifecycle.systemd_native`) associated with an operational state (`supported`, `unsupported`, `misconfigured`, `unavailable`, `unknown`) and a confidence level. |
| **Requirement** | A composite expression (using `AND`, `OR`, `NOT`) declaring the capabilities necessary for a workload to run. Evaluates to `SATISFIED`, `UNSATISFIED`, or `INDETERMINATE`. |
| **EvaluationContext** | The complete execution identity and containerization environment under which capabilities are evaluated (UID, GID, subuid/subgid allocations, `XDG_RUNTIME_DIR`, user systemd manager, namespace isolation). |
| **ConfigurationState** | The effective configuration resulting from precedence evaluation across vendor defaults, system overrides, drop-ins, and user configuration. |
| **Diagnostic** | Detailed explanatory metadata attached to a capability or requirement verdict explaining the *why*, root cause, and dependency chain. |

---

## 3. Requirement Evaluation Logic & Truth Tables

Requirements evaluate to one of three states:
- **`SATISFIED`**
- **`UNSATISFIED`**
- **`INDETERMINATE`**

### 3.1 Logical AND

| Left | Right | Result | Rationale |
| :--- | :--- | :--- | :--- |
| `SATISFIED` | `SATISFIED` | **`SATISFIED`** | Both required capabilities are supported. |
| `SATISFIED` | `UNSATISFIED` | **`UNSATISFIED`** | A required dependency is missing or misconfigured. |
| `SATISFIED` | `INDETERMINATE` | **`INDETERMINATE`** | Cannot confirm if the complete requirement is satisfied. |
| `UNSATISFIED` | *anything* | **`UNSATISFIED`** | Short-circuit: a required condition has definitely failed. |
| `INDETERMINATE` | `SATISFIED` | **`INDETERMINATE`** | One satisfied branch cannot clear the unknown status of another. |
| `INDETERMINATE` | `UNSATISFIED` | **`UNSATISFIED`** | A definite failure overrides indeterminate state. |
| `INDETERMINATE` | `INDETERMINATE` | **`INDETERMINATE`** | Outcome remains unknown. |

### 3.2 Logical OR

| Left | Right | Result | Rationale |
| :--- | :--- | :--- | :--- |
| `SATISFIED` | *anything* | **`SATISFIED`** | Short-circuit: at least one branch is fully satisfied. |
| `UNSATISFIED` | `SATISFIED` | **`SATISFIED`** | Alternative branch satisfies requirement. |
| `UNSATISFIED` | `UNSATISFIED` | **`UNSATISFIED`** | All alternatives have failed. |
| `UNSATISFIED` | `INDETERMINATE` | **`INDETERMINATE`** | Unknown whether the alternative branch could succeed. |
| `INDETERMINATE` | `SATISFIED` | **`SATISFIED`** | Short-circuit: confirmed satisfied branch wins. |
| `INDETERMINATE` | `UNSATISFIED` | **`INDETERMINATE`** | Unknown whether the indeterminate branch could succeed. |
| `INDETERMINATE` | `INDETERMINATE` | **`INDETERMINATE`** | Outcome remains unknown. |

### 3.3 Logical NOT (Predicate Negation)

Arbitrary inversion of operational states is prohibited (e.g. `NOT(unknown)` does not equal `supported`). Negation applies strictly to requirement evaluation predicates:

- `NOT(SATISFIED)` ──▶ **`UNSATISFIED`**
- `NOT(UNSATISFIED)` ──▶ **`SATISFIED`**
- `NOT(INDETERMINATE)` ──▶ **`INDETERMINATE`**

---

## 4. Evidence Precedence Hierarchy

When conflicting claims exist regarding a capability, resolution strictly follows the authoritative precedence order:

```text
1. Direct Live Evidence            (Kernel syscalls, /proc, /sys, live disk checks)
         ▲
         │ (overrides)
2. Runtime Effective State         (`podman info --format json`, Docker info)
         ▲
         │ (overrides)
3. Configuration File Evidence     (Parsed registries.conf, storage.conf, drop-ins)
         ▲
         │ (overrides)
4. Historical Version Knowledge    (Documented release features, deprecation tables)
         ▲
         │ (overrides)
5. Heuristics                      (OS release conventions, fallback assumptions)
```

---

## 5. Execution Context (`EvaluationContext`)

Capabilities are evaluated relative to an explicit context:

```go
type EvaluationContext struct {
    Host          HostContext
    Identity      IdentityContext
    Runtime       RuntimeContext
    Configuration ConfigContext
}

type IdentityContext struct {
    Current        UserIdentity
    Target         UserIdentity
    IsRootless     bool
    SubUIDRanges   []SubIDRange
    SubGIDRanges   []SubIDRange
    XDGRuntimeDir  string
    HasUserSystemd bool
    InContainer    bool
}
```

Capabilities such as rootless storage, rootless port forwarding, and user-level Quadlet generation evaluate against `IdentityContext` rather than system-wide root permissions.

---

## 6. Target Source Tree & Package Boundaries

```text
cmd/
  capagent/
    main.go                 # CLI entry point; handles flags, output formatting, exit codes.
                            # MUST NOT contain capability or probe business logic.

internal/
  model/                    # Core domain primitives: Facts, Observations, Evidence,
                            # Capabilities, EvaluationContext, States.
  probe/                    # Probe interface, registry, runner, timeout & concurrency orchestration.
  platform/                 # OS abstractions: PlatformReader, ScopedReader,
                            # ProcfsReader, SysfsReader, CommandRunner, syscall
                            # wrappers (statfs, uname, golang.org/x/sys/unix
                            # openat2 for kernel-confined containment).
  host/                     # Host probes: os-release, kernel, systemd, cgroups, namespaces,
                            # security (SELinux, AppArmor, seccomp), network, DNS, storage.
  runtime/                  # Runtime adapter interfaces, discovery, and runtime implementations:
    podman/
    docker/
    containerd/
    crio/
    nerdctl/
  config/                   # Configuration source resolution, drop-in discovery, precedence models.
  knowledge/                # Declarative historical version rules and source provenance.
  capability/               # Canonical capability registry, dependency DAG, evidence evaluation.
  requirement/              # Requirement parser, 3-valued logic engine, diagnostic explainers.
  diagnostics/              # Human-readable output renderers, doctor inspection, evidence trees.
  output/                   # Deterministic JSON serializer and Schema v1 definitions.

tests/
  unit/                     # Level 1: Fast tests of parsers, models, and truth tables.
  probe/                    # Level 2: Probe contract tests using mock readers.
  contract/                 # Interface contracts and serialization round-trip tests.
  fixture/                  # Level 3: Simulation fixtures of historical runtimes & broken environments.
  integration/              # Level 3: Real runtime integration tests (Podman, Docker).
  compatibility/            # Level 4: Real OS compatibility matrix verification.
  e2e/                      # End-to-end CLI workflow tests.

testdata/
  hosts/                    # Simulated /etc/os-release and host files
  proc/                     # Simulated /proc hierarchies
  sys/                      # Simulated /sys hierarchies
  podman/                   # podman version, podman info golden files
  docker/                   # docker version, docker info golden files
  containerd/               # containerd configs and socket mocks
  configs/                  # registries.conf, storage.conf, containers.conf variations
  expected/                 # Golden output JSON files
```

### Architectural Boundaries
1. **CLI does not contain business logic**: `cmd/capagent` only parses flags, calls the runner, and writes to stdout/stderr.
2. **Runtime adapters do not depend on CLI**: `internal/runtime/*` only consume `platform.CommandRunner` and `platform.PlatformReader`.
3. **Capability engine does not execute commands**: `internal/capability` evaluates purely over `Evidence` graphs.
4. **Requirement engine does not execute host probes**: `internal/requirement` evaluates strictly against `Capability` outputs.

### `internal/probe` Contract
The probe package is the dynamic-dependency orchestrator that materializes a
canonical execution plan from a static DAG.

**Layer position.** Sits between `internal/platform` (low-level OS abstractions)
and `internal/host` / `internal/runtime` (which are *consumers* of probe
infrastructure in later milestones). May import `internal/model` and
`internal/platform` only — never engine, configuration, knowledge, diagnostics,
or CLI packages.

**Surface.** Two exported types drive all probe execution:

- `Probe` (`internal/probe/probe.go`): declares `ID()`, `Dependencies()`, and
  `Run(ctx, env) → (model.Observation, error)`. Per-run scheduling state lives
  in the orchestrator; probe-local mutation follows the concurrency contract below.
- `Registry` (`internal/probe/registry.go`): owns the canonical DAG. State
  machine is `Open → Resolved → immutable`; subsequent `Register()` calls
  after `Resolve()` return `ErrRegistryResolved`. Failure also seals the registry;
  retries, plan access, and constructor retries retain the original error identity.
  A valid empty registry resolves successfully.

**Scheduling invariants.**

- Topological order is computed via Kahn's algorithm with a **registration-order
  tie-breaker** for `ResolvedPlan()`. `Resolve` captures and copies dependency
  declarations once, validates that snapshot, and compiles private adjacency
  indices and probe references. Runs never call dependency methods again.
  Callers must not mutate declarations concurrently with snapshot capture.
- A dependent becomes runnable **only** after all its declared prerequisites
  finish with `ProbeSucceeded`. Prerequisite `ProbeFailed`, `ProbeCancelled`,
  or `ProbeSkipped` cascades to transitive dependents as `ProbeSkipped` with
  `ErrDependencyFailed`. Dependents are NEVER marked `ProbeCancelled`; only
  direct cancellation propagates that status.
- Concurrency cap is configurable via `WithMaxConcurrency(n ≥ 1)`. Worker
  count is capped at `min(maxConcurrency, number of registered probes)`.
  Workers block while no probe is currently runnable and wake when work
  becomes available. Empty registries return an empty slice without
  spawning goroutines.
- Output follows registration order, including when a dependent was registered
  before its prerequisite. This differs from topological `ResolvedPlan()` order.
  Concurrent probe start and completion order are not guaranteed.
- Returned observations survive errors and cooperative cancellation. Only a
  successful status satisfies prerequisites; partial measurements do not.

**Cancellation responsibilities.** Cancellation is split between the
orchestrator and each probe:

- **Orchestrator.** Propagates a `context.Context` to every `Probe.Run`
  call, and classifies cooperative cancellation by inspecting the
  returned error against `ctx.Err()`. The orchestrator cannot forcibly
  terminate arbitrary Go code executing inside `Probe.Run`.
- **Probe.** `Probe.Run` implementations MUST observe `ctx.Done()` and
  return promptly when the supplied context is cancelled. A probe that
  blocks indefinitely will block its worker goroutine and prevent
  subsequent probes from being scheduled. Returning `ctx.Err()`
  (verbatim or wrapped via `fmt.Errorf("%w", ...)` or `errors.Is`) is
  classified as `ProbeCancelled` by the orchestrator.

**Concurrency responsibilities.** Each registered `Probe` instance is
executed at most once per `Orchestrator.Run`. Sibling probes may execute
concurrently with one another, so the `platform.Environment` and its
dependencies may be accessed concurrently from multiple probe goroutines.
Sequential runs permit probe-local mutation. Callers sharing an instance across
concurrent runs, including different registries, must provide safely reentrant
probes. The orchestrator isolates per-run scheduler state; it does not serialize
shared probe instances. It joins workers and never closes owner-held services.

A directly cancelled probe is recorded as `ProbeCancelled`; transitive
dependents of a cancelled, failed, or skipped probe are recorded as
`ProbeSkipped` with `ErrDependencyFailed`. Dependents are NEVER marked
`ProbeCancelled`; only direct cancellation propagates that status.

**Environment injection.** All probe `Run` invocations receive a
`platform.Environment` value with read-only `Reader()`, `Procfs()`, `Sysfs()`,
and `Runner()` accessors. Private forwarding values hide concrete setup and
close handles, including mutable procfs/sysfs wrapper pointers. Nil services
remain nil. This is ordinary type/API prevention, verified by surface-contract
and shared-reader race tests; it is not a sandbox against reflection or unsafe.
Owners configure services before runs and close them after all runs join.
Providers must support concurrent operations and return caller-owned measurement
buffers. Custom providers must honor this contract. Synchronized operational
state, such as fake command call recording, may change during a run.
`MemPlatformReader` copies inserted content; `FakeCommandRunner` copies registered
and returned stdout/stderr. Test owners retain their concrete handles to seed
fixtures, while probes receive only the measurement interfaces.

**Command specifications and environment ownership.**
`CommandRunner.Run(ctx, CommandSpec)` accepts `Path`, `Args`, `Env`, `Dir`, and
`Timeout`. The path must be absolute; arguments are literal and NUL-free. Empty
`Dir` means `/`, and other directory values must be absolute and NUL-free.
There is no executable lookup or global `chdir`. A zero timeout selects the
configured runner default, a positive timeout overrides it, and negative values
are rejected. An already-cancelled context is rejected before specification
validation or process setup. Nil contexts retain the background-context behavior.

The zero `EnvPolicy` contains only `LC_ALL=C` and `PATH=/usr/bin:/bin`.
`NewEnvPolicy(inherit []string, overrides map[string]string)` captures named
inherited variables and applies explicit overrides after the defaults. The
result is an immutable sorted snapshot; execution never rereads ambient state.
`Variables()` returns a defensive copy for explicit, sensitive inspection.
Callers must not mutate input collections during construction or execution.

Fake registration accepts the same specification and returns validation errors.
Matching includes the absolute path, ordered arguments, effective directory,
environment snapshot and declared timeout. Zero timeout stays distinct from an
explicit 30 seconds because it requests the configured default. Empty and `/`
directories match, as do equivalent environment snapshots. Registrations retain
no mutable argument/output aliases, and call records expose copied specifications.
Injected fake results do not simulate elapsed time or host execution; fixtures
explicitly provide outcomes. Pre-cancelled/invalid calls are not recorded.

Specifications, policies and runner-owned errors have redacted formatting.
Underlying errors remain available through `errors.Is`/`errors.As`; raw output,
explicit environment inspection and unwrapped errors require consumer sanitization.
See the [execution security contract](security.md#executables-environment-and-target-identity)
for current-identity, endpoint and passive-command limitations.

**`internal/platform` CommandRunner process-group lifecycle.**
`OSCommandRunner` creates each subprocess in its own process group via
`Setpgid`. Timeout or caller cancellation signals the owned group; `Run`
reaps the direct child and joins its cancellation-forwarding goroutine and
any started timer callback. Normally completing commands are not signalled
after completion. This does not prove that all descendants have exited.

The default execution timeout is 30 seconds. `Cmd.WaitDelay` adds a 250 ms
budget for lingering pipes after cancellation or observed direct-child exit,
whichever occurs first. Pipe closure bounds drain even when a descendant
changes session, subject to kernel/scheduling delays; it does not promise
termination of escaped descendants. A successful exit with expired drain
wraps `exec.ErrWaitDelay`; nonzero exit preserves `*exec.ExitError` through wrapping.
Retained prefixes remain available on errors. Caller cancellation observable
at classification wins over internal timeout, followed by execution errors.
`TimedOut` is true only when the internal timeout is selected. ExitCode zero
alone cannot establish success, including on startup failure.

Each output stream retains at most 1 MiB. Buffers begin at 4 KiB and grow
geometrically with backing capacity clamped to that limit. Returned copies
and temporary growth allocations are additional memory. Truncation flags
indicate discarded bytes beyond the cap, not general stream completeness.
An empty write at the exact cap does not mark truncation.

The orchestrator defaults to `max(1, min(runtime.NumCPU(), 8))` workers.
This conservative resource default is overridable with any positive
`WithMaxConcurrency` value; invalid explicit values remain errors.

**`internal/platform` ScopedReader filesystem-security boundary (M1.1).**
`ScopedReader` is the canonical filesystem abstraction for untrusted
subpath access. The file methods (`ReadFile`, `Stat`,
`ReadDir`, `Readlink`, `FileCapabilities`) opens the target via `openat2(2)` with
`RESOLVE_IN_ROOT | RESOLVE_NO_MAGICLINKS` against the reader's rootfd,
then performs fd-relative I/O via direct `unix.*` syscalls
(`unix.Read(fd, ...)`, `unix.Fstat(fd, ...)`,
`unix.Fstatat(dirfd, name, ..., AT_SYMLINK_NOFOLLOW)`,
`unix.Readlinkat(fd, "", ...)`). The kernel enforces containment at
every open; lexical `ValidateSubpath` is the first-layer rejection
predicate but is not the security boundary.

**Adapter paths and roots:** `NewProcfsReader(reader)` and
`NewSysfsReader(reader)` derive their root from the supplied `ScopedReader`.
The environment selects `/proc` and `/sys` when it creates those readers;
adapters cannot advertise an independently configured root. Public subpath
methods validate the original input and forward accepted bytes unchanged.
`.` names the scoped root; empty, absolute (including `/`), NUL-containing,
and lexically escaping subpaths are rejected before file I/O. Backslash is
an ordinary Linux filename byte. In particular, `link/../value` must resolve
the link before its parent component; adapters must not clean or join away
that meaning. `ReadSelf` validates its argument then prefixes `self/` literally.
Its confinement boundary is the proc reader root, not a separate self subtree.

**FD ownership:** the implementation never wraps an openat2 FD in
`*os.File`. Each per-operation FD is owned exclusively by
`readSubpath`, whose deferred `unix.Close(fd)` is the only close
path. Callbacks perform fd-relative I/O via direct syscalls; they
must not close the FD. This ownership rule prevents double-close;
FD 0 is a valid descriptor and is treated symmetrically with any
other non-sentinel FD value. Both root and per-operation opens atomically
include `O_CLOEXEC`, so exec does not leak implicitly inherited scoped
handles. No post-open flag mutation or additional close owner is needed.

**Symlink semantics:** the OS reader delegates symlink traversal to
the kernel (SYMLOOP_MAX = 40 hops); the memory reader maintains an
explicit per-hop containment check and a 16-hop application-level
counter. `ReadDir` uses `AT_SYMLINK_NOFOLLOW` for child metadata so
child symlinks are never followed even when their target lies outside
the root, and pre-computes each entry's `FileInfo` so `DirEntry.Info()`
performs no further host I/O after the directory FD has been closed.
All filesystem readers return directory snapshots sorted lexically by name.
The captured metadata remains usable after mutation, deletion, or reader close.
A child disappearing with `ENOENT` between enumeration and metadata capture
may be skipped. Other metadata failures discard the enumeration and return an
error preserving the underlying errno through `errors.Is`; successful absence
must not represent permission denial or I/O failure.

**Metadata and file privileges:** `Stat` and eager `DirEntry.Info()` preserve
regular/directory/symlink/FIFO/socket/device kinds and setuid/setgid/sticky bits.
`OwnershipOf(info)` returns a copied UID/GID plus a known flag; an unspecified
fixture owner is distinct from root. Memory nodes are replaced rather than
mutated, including ownership and copied capability attributes. `FileCapabilities`
reads only `security.capability`, retains at most 64 KiB, and distinguishes absent
attributes from unsupported xattrs, permission failures, and I/O errors. Raw
attribute bytes are not an assertion of valid encoding or effective privileges.
Inspection requires a readable regular file; metadata-only stat does not.

**Bounded reads and context:** `ReadFile(ctx, path)` and `ReadDir(ctx, path)`
propagate the supplied context through procfs/sysfs and the read-only environment
views. Constructors copy validated `ReadLimits`: defaults are 8 MiB retained file
bytes, 8,192 encountered directory names, and 1 MiB aggregate directory-name
bytes. Explicit overrides must be positive and below half MaxInt; zero is not
unlimited. File reads inspect one extra byte to distinguish exact-limit EOF from
actual overflow. Directory enumeration uses fixed-size getdents chunks and eager
fd-relative metadata; disappearing entries still consume the encounter budget.
Limits and cancellation return sorted partial directory results with an error.
Subset membership and selection order on incomplete enumeration are unspecified.
The memory reader scans its existing flat map with context checks and bounded
retention; directory latency can scale with the entire fixture map size.

Data opens first use `O_PATH` to reject observed non-regular files, then a second
confined `O_RDONLY|O_NONBLOCK|O_NOCTTY` open checks type and inode identity before
reading. Both descriptor scopes have one owner and use `O_CLOEXEC`. Replacement
returns `ErrFileChanged` or a file-type/path error without reading replacement
data. This is not atomic regular-file-only opening: a malicious replacement
device can still enter its open handler. The supported contract assumes responsive
host filesystems and procfs/sysfs metadata sources; it does not cover hostile
device drivers or forced cancellation of a stuck syscall. No magic-link reopening
or background goroutine timeout substitutes for the containment boundary.

Scoped operations check closed state, original path validity, then context before
I/O. Context is checked between read/enumeration calls and finite EINTR retries.
Metadata calls remain synchronous. Owners close readers only after workers join.
These are retention and cooperative-work bounds, not a hard time or process RSS
limit: scratch buffers, allocation growth, parsed values, and concurrent calls
consume additional memory.

**Parser completeness:** procfs parsers cap input at 8 MiB, accepted records at
8,192, and retained diagnostics at 32 by default. Line limits are 64 KiB for
filesystems/cgroup and 1 MiB for mountinfo. `NewProcfsReaderWithLimits` accepts
explicit validated `ParserLimits`. Oversized complete lines can be skipped while
valid later records are retained; the result still has an error. A final
unterminated record is accepted only at clean EOF. On incomplete reads, only
complete preceding records are parsed. Diagnostics contain record locations and
categories, not raw input contents; excess diagnostics are counted.

`MountEntry.Root`, `MountPoint`, and `MountSource` remain raw escaped mountinfo
fields. No decoding or path-authority grant is implicit. `ReadCgroupFile` accepts
a single conservative filename (`cpu.max`, `memory.current`, `cgroup.controllers`),
not a controller-presence claim. Controller-list tokens are validated separately.
Invalid SELinux text returns an error; false from `IsSELinuxEnforcing` alone does
not distinguish an absent subsystem from permissive mode. A present subsystem
whose enforce file disappears returns a missing-file error.

**Error mapping:** magic-link rejection via `RESOLVE_NO_MAGICLINKS`
retains `syscall.ELOOP` (not `ErrSubpathEscape`). Native errno identities are
preserved through contextual wrappers, alongside familiar `os.Err*` matches.
`LimitError` matches `ErrLimitExceeded` and `ErrIncomplete`; `ParseError` retains
bounded diagnostics and matching causes. Partial records plus an error are not
successful absence. Callers may inspect context causes before transport/parse
causes when both are present. Pre-5.6 kernels return `ErrSymlinkUnsupported`
from `NewScopedOSReader`; capagent targets Linux 5.6+ as the
documented minimum. Both memory readers traverse pathname components in order,
including intermediate links and trailing-slash directory requirements. Parent
directories must exist in fixtures. Relative unscoped memory keys inhabit a
virtual namespace independent of the process working directory.

Absolute-link semantics intentionally differ: the OS scoped reader interprets
absolute targets relative to its root, while the scoped memory reader interprets
them as paths in the backing virtual tree and rejects traversal outside its
root. Thus even an absolute target spelled under the memory root may name a
different OS-scoped path. Memory rejects symlink traversal above the root rather
than emulating the kernel's root-clamping behavior, and does not model Linux
magic links. The unscoped memory reader uses physical absolute target semantics;
both OS readers delegate pathname resolution to the kernel. These distinctions
are explicit regression cases rather than claims of complete emulation.

**Architecture enforcement.** The repository and fixture tests use the same
per-file AST scanner. It recognizes configured filesystem, descriptor, process,
identity, network, raw syscall, environment and filesystem-touching path-helper
selectors outside `internal/platform/`, with an exemption for the contract harness.
The scanner handles import aliases and direct function-value references, rejects
dot imports of monitored packages, and distinguishes locally shadowed identifiers.
Errno/constants, metadata types and pure path operations remain permitted. The
host-access rule does not independently authorize a package dependency.

Production platform dependencies use an exact third-party allowlist containing
only `golang.org/x/sys/unix`; arbitrary external packages and sibling/subpackages
are rejected. Additional syscall adapters require the separately approved #65
boundary. Model and requirement import restrictions and JSON-v2 enforcement remain
in force. Fixture paths are supplied as logical source locations, so the contract
harness exemption cannot accidentally exempt the negative examples.

CLI code may import only `internal/app` and `internal/version` within the module.
The reserved `internal/app` boundary allows composition of lower layers, forbids
dependencies on `cmd`, and cannot be imported by lower layers. Its implementation
remains #61 work. Neither the CLI nor the application owner has a host-I/O exemption.
Ordinary `os.Args`, `os.Stdout`, `os.Stderr` and `os.Exit` use remains permitted.
Test files may inspect ambient environment through `os` or `syscall`, but may not
perform direct host-I/O, subprocess execution or process-global environment mutation
outside the permitted harnesses.

These are syntactic guards, not full effect analysis or a sandbox. Indirect method
calls, reflection, dynamic execution and unlisted/new primitive APIs need review;
new wrappers must extend the catalog and regression fixtures. Vendored, hidden,
testdata and designated generated files are excluded from the host-access walk.

---

## 7. JSON Schema v1 Specification

Deterministic serialization guarantees that identical inputs produce canonical byte-identical output for a given build.

### 7.1 Architecture Invariant: Domain Model vs. Wire Contract

`internal/model` represents domain data and validation: facts, identity context and evidence/capability records. Three-valued Boolean logic and requirement evaluation belong to `internal/requirement`. **Schema v1 must never become the domain model.**

`internal/output` defines versioned serialization DTOs (`v1`) that project internal domain types into the external wire format. When future schema versions (`v2`, etc.) are introduced, they will define dedicated DTO projections without mutating or churning the internal domain model.

### 7.2 Context Mapping & Intentional Renaming

The domain model explicitly separates executing identity (`Current`) from target deployment identity (`Target`). The external Schema v1 contract evaluates strictly against target workload identity:

| Schema v1 Field | Domain Expression | Semantics / Fallback |
| :--- | :--- | :--- |
| `context.uid` | `evalCtx.Identity.Target.UID` | Target user UID (falls back to `Current.UID` only if `Target` is nil; explicit UID/GID 0 is retained) |
| `context.gid` | `evalCtx.Identity.Target.GID` | Target user GID (falls back to `Current.GID` only if `Target` is nil; explicit UID/GID 0 is retained) |
| `context.target_user` | `evalCtx.Identity.Target.Username` | Target username (falls back to `Current.Username` only if `Target` is nil; explicit UID/GID 0 is retained) |
| `context.is_rootless` | `evalCtx.Identity.IsRootless` | Whether target execution runs under rootless user namespaces |
| `context.in_container` | `evalCtx.Identity.InContainer` | Whether the evaluation environment runs inside a container |
| `host.systemd` | `evalCtx.Host.SystemdActive` | Intentional external rename of model-level `SystemdActive` |
| `capabilities.*.evidence` | `model.EvidenceRef.ID` | Flattening structured `EvidenceRef` references into `[]string` |

### 7.3 Non-Null & Required Collections Guarantee

Schema v1 enforces strict non-null containers:
- Root `runtimes` and `capabilities` maps are always serialized as objects (`{}`), never `null`.
- The `evidence` field in capability items is **required** and always serialized as an array (`[]`), never `null` and never omitted.
- Optional diagnostic strings (e.g. `reason`) omit when empty (`omitempty`).

### 7.4 Modern & Non-Mutating Deterministic Serialization (`encoding/json/v2`)

- **Standard Library `encoding/json/v2`**: All JSON processing exclusively uses `encoding/json/v2` and `encoding/json/jsontext`. Legacy `encoding/json` (v1) is strictly forbidden across the codebase and mechanically prevented by AST contract tests.
- **Immutability**: `output.Marshal(r)` clones evidence slices before sorting. Serialization never mutates the input `Report`.
- **Deterministic Key & Slice Ordering**:
  - Canonical Go struct field declaration order guarantees root key sequencing: `schema_version` $\to$ `context` $\to$ `host` $\to$ `runtimes` $\to$ `capabilities` $\to$ optional `evaluation`.
  - Built-in `json.Deterministic(true)` ensures map keys (`runtimes`, `capabilities`) are sorted lexicographically.
  - Cloned evidence slices are sorted lexicographically before emission.
  - Canonical formatting applies standard 2-space indentation via `jsontext.WithIndent("  ")` with a trailing newline. `output.MarshalCompact` provides unindented output for stream pipelines.
  - Precision of determinism guarantee: `json.Deterministic(true)` guarantees byte-identical output across instances of the same binary for a given build.
- **API Lifecycle & Separation of Concerns**:
  - `NewReport()`: Mutable builder/skeleton with initialized non-nil maps and `SchemaVersion = 1`.
  - `NewReportFromModel(...)`: Direct domain-to-report projection.
  - `(*Report).Validate()`: Verifies report-level invariants not guaranteed by Go's type system (schema version, non-nil collections, valid cgroup/capability enums, non-nil evidence). It is intentionally distinct from the full JSON Schema wire validator.
  - `output.Marshal(r)`: Serializes without implicitly validating; callers assembling reports manually should invoke `r.Validate()` prior to serialization.

### 7.5 Additive Evolution & Versioning Rules

Schema v1 remains pre-release. M1.2 corrects UID/GID bounds and missing-value representation, as documented in [fixture evaluation](fixture-evaluation.md#consumer-and-pre-release-contract). The following compatibility rules apply after the M19 freeze.

Schema v1 adheres to an open additive evolution model (`additionalProperties: true`):
- **Permitted (Non-Breaking)**:
  - Adding new optional properties inside existing objects (`context`, `host`, `capabilities.*`).
  - Introducing new capability IDs or discovered runtimes.
- **Breaking (Requires Schema v2)**:
  - Adding new required properties.
  - Changing an existing property's data type.
  - Removing or renaming an existing property.
  - Restricting enum values.
  - Altering the semantic meaning of an existing property.
- **Unknown Fields Policy**: Parsers accept and ignore unknown properties during unmarshaling (Policy A), ensuring older clients process newer reports seamlessly.

### 7.6 Synthetic Fixture Provenance

Sample reports in `testdata/expected/` (`minimal_linux.json`, `rhel9_podman.json`, `docker_host.json`) represent **synthetic schema contracts and structural serialization baselines**, rather than authoritative claims about real-world runtime behavior (which are verified by runtime integration suites).

### 7.7 Canonical Schema Definition (`schema/v1/schema.json`)

The authoritative schema is [schema/v1/schema.json](../schema/v1/schema.json), embedded in the binary and compiled by contract tests. It defines canonical capability keys, bounded nullable identities, completeness, scope, provenance, evidence references and requirement results. Keeping one source avoids a stale copied schema in this document.

---

## 8. Evaluation Examples

### 8.1 Example: Success Case
On a modern RHEL 9 host running Podman 5.x with cgroup v2, systemd 252, and Netavark:

```json
{
  "schema_version": 1,
  "context": { "uid": 1000, "is_rootless": true },
  "host": { "os": "rhel", "os_version": "9.4", "cgroup_version": "v2", "systemd": true },
  "capabilities": {
    "container.lifecycle.systemd_native": {
      "state": "supported",
      "confidence": "verified",
      "evidence": ["systemd=252", "quadlet_generator=present", "cgroups=v2"]
    },
    "container.network.custom_dns": {
      "state": "supported",
      "confidence": "verified",
      "evidence": ["backend=netavark", "aardvark_dns=present"]
    }
  }
}
```
**Requirement:**
```yaml
all:
  - container.lifecycle.systemd_native
  - container.network.custom_dns
```
**Verdict:** `SATISFIED`

### 8.2 Example: Actionable Failure Case
On an older host where Podman 4.9 is installed but cgroup v1 is active:

```json
{
  "capabilities": {
    "container.lifecycle.systemd_native": {
      "state": "unsupported",
      "confidence": "verified",
      "reason": "cgroup v1 prevents required systemd-native Quadlet execution",
      "evidence": ["systemd=250", "quadlet_generator=present", "cgroups=v1"]
    }
  }
}
```
**Verdict:** `UNSATISFIED` (Root cause immediately obvious; avoids misleading `quadlet: false`).

### 8.3 Example: Unknown / Indeterminate Case
Podman binary is installed, but permissions prevent the user from accessing the podman service socket:

```json
{
  "capabilities": {
    "runtime.podman.available": {
      "state": "unknown",
      "confidence": "unknown",
      "reason": "permission denied accessing /run/user/1000/podman/podman.sock",
      "evidence": ["binary_exists=true", "socket_connect=permission_denied"]
    }
  }
}
```
**Verdict:** `INDETERMINATE` (Automation can fail-closed safely without falsely claiming Podman is missing).
