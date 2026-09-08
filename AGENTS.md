# AGENTS.md — Developer & AI Agent Guidelines for capagent

Welcome to the **capagent** codebase. This document defines the engineering principles, architectural invariants, package boundaries, coding standards, and TDD workflows that all contributors and autonomous coding agents **must strictly follow**.

---

## 1. Project Vision & Role

`capagent` is a portable, evidence-driven container compatibility engine written in Go. It determines whether a Linux host environment satisfies container workload deployment requirements (e.g. Quadlet, rootless storage, Netavark DNS, cgroup v2), explains *why*, and provides a stable machine-readable contract for automation (such as Ansible).

---

## 2. Core Architectural Principles

1. **Passive by Default**: Probes never create containers, open persistent listening ports, or modify host files. Any active verification requires an explicit `--active` opt-in gate.
2. **Evidence Before Inference**: Direct live measurements always beat runtime self-reports, which beat configuration files, which beat historical version knowledge, which beat heuristics.
3. **Unknown is Not False**: Capabilities distinguish five operational states: `supported`, `unsupported`, `misconfigured`, `unavailable`, and `unknown`.
4. **Context is First-Class**: Capabilities evaluate under an explicit `EvaluationContext` distinguishing current execution identity from target deployment identity (UID, GID, subuid/subgid allocations, user systemd, rootless status).
5. **Zero External Runtime Dependencies**: Compiles with `CGO_ENABLED=0` to a single statically linked binary without third-party runtime dependencies. Standard library is prioritized for core logic.
6. **TDD is Mandatory**: Every capability and domain semantic follows the RED $\to$ GREEN $\to$ REFACTOR cycle. A capability without automated tests is not a supported capability.

---

## 3. The Five Semantic Levels

Never conflate or collapse the five semantic levels of the engine:

```text
Facts
  │  (Raw, uninterpreted filesystem, kernel, or command measurements)
  ▼
Observations
  │  (Structured outputs produced by isolated probes)
  ▼
Evidence
  │  (Authoritative claims backed by observations, ranked by precedence)
  ▼
Capabilities
  │  (Canonical features with operational state, confidence, and evidence refs)
  ▼
Requirements
  │  (Composite 3-valued Boolean logic evaluating to SATISFIED, UNSATISFIED, INDETERMINATE)
  ▼
Deployment Decision
```

---

## 4. Package Boundaries & Dependency Flow

The package dependency graph is strictly unidirectional:

```text
internal/model (Pure domain data structs; ZERO internal/external dependencies)
      │
      ├──────────────────────┐
      ▼                      ▼
internal/capability    internal/requirement (3-valued logic; depends only on model)
      │
      ▼
internal/probe, platform, host, runtime, config, knowledge
      │
      ▼
cmd/capagent (CLI entrypoint; flag parsing and formatting ONLY; zero business logic)
```

### Invariant Boundary Rules:
- `internal/model` MUST contain ONLY domain data types and validation methods (`IsValid()`). It MUST NOT contain capability evaluation or requirement logic.
- `internal/requirement` encapsulates all 3-valued Boolean algebra and requirement AST evaluation.
- `cmd/capagent` MUST NOT contain probe logic, capability resolution, or requirement evaluation.
- Runtime adapters (`internal/runtime/*`) MUST NOT depend on the CLI package.

---

## 5. Evidence Precedence Hierarchy

When conflicting evidence exists, resolution strictly follows this authoritative ranking:

```text
1. PrecedenceLive       (Kernel syscalls, /proc, /sys, live disk checks)
      >
2. PrecedenceRuntime    (Runtime-reported effective state, e.g. podman info)
      >
3. PrecedenceConfig     (Parsed configuration files: containers.conf, storage.conf)
      >
4. PrecedenceKnowledge  (Documented historical version rules and deprecation tables)
      >
5. PrecedenceHeuristic  (OS release conventions, fallback assumptions)
```

Direct live evidence always overrides contradictory historical knowledge or configuration claims.

---

## 6. Three-Valued Requirement Logic

Requirements evaluate to one of three states:
- **`SATISFIED`**
- **`UNSATISFIED`**
- **`INDETERMINATE`**

### Truth Tables:

#### Logical AND (`And(a, b)`):
| Left | Right | Result |
| :--- | :--- | :--- |
| `SATISFIED` | `SATISFIED` | `SATISFIED` |
| `SATISFIED` | `UNSATISFIED` | `UNSATISFIED` |
| `SATISFIED` | `INDETERMINATE` | `INDETERMINATE` |
| `UNSATISFIED` | *any* | `UNSATISFIED` (definite failure short-circuit) |
| `INDETERMINATE` | `SATISFIED` | `INDETERMINATE` |
| `INDETERMINATE` | `UNSATISFIED` | `UNSATISFIED` |
| `INDETERMINATE` | `INDETERMINATE` | `INDETERMINATE` |

#### Logical OR (`Or(a, b)`):
| Left | Right | Result |
| :--- | :--- | :--- |
| `SATISFIED` | *any* | `SATISFIED` (definite satisfaction short-circuit) |
| `UNSATISFIED` | `SATISFIED` | `SATISFIED` |
| `UNSATISFIED` | `UNSATISFIED` | `UNSATISFIED` |
| `UNSATISFIED` | `INDETERMINATE` | `INDETERMINATE` |
| `INDETERMINATE` | `SATISFIED` | `SATISFIED` |
| `INDETERMINATE` | `UNSATISFIED` | `INDETERMINATE` |
| `INDETERMINATE` | `INDETERMINATE` | `INDETERMINATE` |

#### Predicate NOT (`Not(s)`):
- `NOT(SATISFIED)` $\to$ `UNSATISFIED`
- `NOT(UNSATISFIED)` $\to$ `SATISFIED`
- `NOT(INDETERMINATE)` $\to$ `INDETERMINATE`

`All(...)` and `Any(...)` are defined strictly as left-associative folds over `And` and `Or`.

---

## 7. Canonical Capability IDs

Canonical capability IDs follow the hierarchy `<namespace>.[<subsystem...>.]<name>`:
- **Segment Count**: Minimum of two dot-separated segments is required.
- **Namespace**: First segment ($s_0$), e.g. `host`, `runtime`, `container`, `image`, `network`, `storage`, `systemd`.
- **Name**: Final segment ($s_{n-1}$).
- **Subsystem**: Intermediate segments ($s_1 \dots s_{n-2}$), or `""` if exactly two segments total.
- **Two-segment IDs**: Valid (e.g. `runtime.docker` $\to$ Namespace: `runtime`, Subsystem: `""`, Name: `docker`).
- **Multi-segment IDs**: Valid (e.g. `runtime.podman.network.netavark` $\to$ Namespace: `runtime`, Subsystem: `podman.network`, Name: `netavark`).
- **Validation**: All segments must be non-empty and match `^[a-z0-9_]+$`.

---

## 8. Go Coding & Linting Invariants

All Go code must pass the `.golangci.yml` baseline with zero warnings:

- **No Unchecked Errors (`errcheck`)**: Check every error return. Never use `_ = fn()` without explicit documented rationale.
- **No Magic Numbers (`mnd`)**: Extract numbers to named constants.
- **No Naked Returns (`nakedret`)**: Explicitly return values in named return functions.
- **Line Length (`lll`)**: Keep lines $\le 140$ characters.
- **Standard Testing**: Use standard library `testing` with table-driven tests (`tests := []struct{ ... }`), subtests `t.Run`, and `t.Parallel()`. Avoid introducing third-party test assertions into core domain packages.
- **Race Detection**: Always run tests with `go test -race ./...`.
- **Targeted Coverage**: Maintain $> 95\%$ test coverage on `internal/model` and `internal/requirement`.

---

## 9. Standard Makefile Targets

- `make test`: Run all unit tests with race detection (`go test -race ./...`).
- `make coverage`: Run tests, output per-package coverage statistics, and verify coverage $> 95\%$.
- `make lint`: Run `golangci-lint run ./...`.
- `make tidy`: Run `go mod tidy` and `go mod verify`.
- `make clean`: Remove build artifacts and coverage files.

---

## 10. Git Branching & Commit Invariants

- **Never Work Directly on `main`**: All work, regardless of size, must occur on a dedicated branch. Direct commits or work on `main` are strictly prohibited.
- **Conventional Branch Naming**: Use standard conventional branch prefixes:
  - `feat/<short-description>`: New capabilities, models, probes, or features (e.g. `feat/m0-domain-foundation`).
  - `fix/<short-description>`: Bug fixes or correction of evaluation logic.
  - `refactor/<short-description>`: Code restructuring without functional changes.
  - `test/<short-description>`: Test suite expansion or simulation fixture updates.
  - `chore/<short-description>`: Scaffolding, CI, or tooling updates.
- **Sync Before Starting**: Before starting work or cutting a new branch, always fetch and ensure your base branch is up to date with `origin/main`.
- **Conventional Commits**: Every commit message must strictly adhere to the Conventional Commits specification:
  - Format: `<type>(<optional scope>): <description>` (e.g. `feat(model): implement core domain primitives`, `test(requirement): add exhaustive truth table tests`).
  - Imperative mood, concise summary, no trailing period.
