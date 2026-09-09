# Container Capability Engine Architecture

This document defines the architectural boundaries, domain model, evaluation semantics, package structure, and contract specifications for `capagent`.

## Implementation status and planned integration

This document describes both the current foundation and its target architecture. The current CLI does not yet evaluate hosts. [M1.1](https://github.com/EpicBlackWolfZ/capagent/milestone/23) closes known resource, filesystem and execution-contract gaps; [M1.2](https://github.com/EpicBlackWolfZ/capagent/milestone/24) supplies a minimal evaluation path before broader capability work. See [roadmap.md](roadmap.md) and [security.md](security.md) for delivery status and limits.

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

**`internal/platform` CommandRunner process-group lifecycle.**
`OSCommandRunner` creates each subprocess in its own process group via
`Setpgid`. When timeout or caller cancellation interrupts execution, the
runner terminates the entire process group and waits for the process to
be reaped. Normally completing commands are not signalled after
completion. This does not prove that all descendants have exited. Bounded
pipe-drain handling remains open in [#37](https://github.com/EpicBlackWolfZ/capagent/issues/37). Timeout-vs-caller-cancellation precedence: when both events
become observable before result classification, caller cancellation wins.
`TimedOut` is set to `true` only when the internal timeout is the
selected termination reason.

**`internal/platform` ScopedReader filesystem-security boundary (M1.1).**
`ScopedReader` is the canonical filesystem abstraction for untrusted
subpath access. Each of the four file methods (`ReadFile`, `Stat`,
`ReadDir`, `Readlink`) opens the target via `openat2(2)` with
`RESOLVE_IN_ROOT | RESOLVE_NO_MAGICLINKS` against the reader's rootfd,
then performs fd-relative I/O via direct `unix.*` syscalls
(`unix.Read(fd, ...)`, `unix.Fstat(fd, ...)`,
`unix.Fstatat(dirfd, name, ..., AT_SYMLINK_NOFOLLOW)`,
`unix.Readlinkat(fd, "", ...)`). The kernel enforces containment at
every open; lexical `ValidateSubpath` is the first-layer rejection
predicate but is not the security boundary.

**FD ownership:** the implementation never wraps an openat2 FD in
`*os.File`. Each per-operation FD is owned exclusively by
`readSubpath`, whose deferred `unix.Close(fd)` is the only close
path. Callbacks perform fd-relative I/O via direct syscalls; they
must not close the FD. This ownership rule prevents double-close;
FD 0 is a valid descriptor and is treated symmetrically with any
other non-sentinel FD value.

**Symlink semantics:** the OS reader delegates symlink traversal to
the kernel (SYMLOOP_MAX = 40 hops); the memory reader maintains an
explicit per-hop containment check and a 16-hop application-level
counter. `ReadDir` uses `AT_SYMLINK_NOFOLLOW` for child metadata so
child symlinks are never followed even when their target lies outside
the root, and pre-computes each entry's `FileInfo` so `DirEntry.Info()`
performs no further host I/O after the directory FD has been closed.

**Error mapping:** magic-link rejection via `RESOLVE_NO_MAGICLINKS`
is surfaced as `syscall.ELOOP` (not `ErrSubpathEscape`), per the
corrected semantics. Pre-5.6 kernels return `ErrSymlinkUnsupported`
from `NewScopedOSReader`; capagent targets Linux 5.6+ as the
documented minimum. The OS-vs-memory discrepancy for absolute
symlink targets outside root (OS reinterprets absolute targets
relative to the scoped root,
returning `ErrNotExist` if the reinterpreted path is absent; memory
returns `ErrSubpathEscape` via an explicit lexical check) is
documented and tested.

**Architecture enforcement.** The AST-based host-IO denylist test
(`TestArchitecture_ForbidHostIOPrimitivesOutsidePlatform`) detects configured
direct file-I/O, command-execution and ambient-environment patterns
outside `internal/platform/`, with an exemption for the
`tests/contract/` harness. The `cmd/` CLI prefix is NOT in
the denylist allowlist; CLI binaries that need `os.Args`,
`os.Stdout`, `os.Stderr`, or `os.Exit` use those primitives
directly because they are not in any denylist. Test files may use
`os.Getenv`, `os.LookupEnv`, and `os.Environ` (ambient environment
exemption) but NOT file-I/O or `os/exec`.

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
| `context.uid` | `evalCtx.Identity.Target.UID` | Target user UID (falls back to `Current.UID` if `Target` is unpopulated) |
| `context.gid` | `evalCtx.Identity.Target.GID` | Target user GID (falls back to `Current.GID` if `Target` is unpopulated) |
| `context.target_user` | `evalCtx.Identity.Target.Username` | Target username (falls back to `Current.Username` if `Target` is unpopulated) |
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
  - Canonical Go struct field declaration order guarantees root key sequencing: `schema_version` $\to$ `context` $\to$ `host` $\to$ `runtimes` $\to$ `capabilities`.
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

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://github.com/EpicBlackWolfZ/capagent/schema/v1/schema.json",
  "title": "CapagentReportV1",
  "description": "Authoritative machine-readable report format for container host compatibility evaluations (Schema v1).",
  "type": "object",
  "additionalProperties": true,
  "required": [
    "schema_version",
    "context",
    "host",
    "runtimes",
    "capabilities"
  ],
  "properties": {
    "schema_version": {
      "type": "integer",
      "const": 1,
      "description": "Canonical schema major version integer (fixed to 1 for Schema v1)."
    },
    "context": {
      "type": "object",
      "description": "Evaluation execution context summarizing target user identity and environment boundaries.",
      "additionalProperties": true,
      "required": [
        "uid",
        "gid",
        "target_user",
        "is_rootless",
        "in_container"
      ],
      "properties": {
        "uid": { "type": "integer" },
        "gid": { "type": "integer" },
        "target_user": { "type": "string" },
        "is_rootless": { "type": "boolean" },
        "in_container": { "type": "boolean" }
      }
    },
    "host": {
      "type": "object",
      "description": "Observed host-level kernel, distribution, cgroup, and init system facts.",
      "additionalProperties": true,
      "required": [
        "os",
        "os_version",
        "kernel",
        "architecture",
        "cgroup_version",
        "systemd"
      ],
      "properties": {
        "os": { "type": "string" },
        "os_version": { "type": "string" },
        "kernel": { "type": "string" },
        "architecture": { "type": "string" },
        "cgroup_version": { "type": "string", "enum": ["v1", "v2", "mixed", "unavailable", "unknown"] },
        "systemd": { "type": "boolean" }
      }
    },
    "runtimes": {
      "type": "object",
      "description": "Observed container runtimes discovered on the host environment.",
      "additionalProperties": {
        "type": "object",
        "additionalProperties": true,
        "properties": {
          "installed": { "type": "boolean" },
          "version": { "type": "string" },
          "accessible": { "type": "boolean" },
          "network_backend": { "type": "string" },
          "storage_driver": { "type": "string" }
        }
      }
    },
    "capabilities": {
      "type": "object",
      "description": "Evaluated canonical capabilities mapped to operational state, confidence, and evidence.",
      "additionalProperties": {
        "type": "object",
        "additionalProperties": true,
        "required": [
          "state",
          "confidence",
          "evidence"
        ],
        "properties": {
          "state": { "type": "string", "enum": ["supported", "unsupported", "misconfigured", "unavailable", "unknown"] },
          "confidence": { "type": "string", "enum": ["verified", "derived", "heuristic", "unknown"] },
          "reason": { "type": "string" },
          "evidence": { "type": "array", "items": { "type": "string" } }
        }
      }
    }
  }
}
```

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
