# capagent — Container Capability Engine

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.27.1-00ADD8.svg)](https://go.dev/)
[![CGO](https://img.shields.io/badge/CGO-disabled-success.svg)](#)

> **A portable, evidence-driven compatibility engine that determines whether a Linux environment can satisfy a container deployment requirement, explains why, and provides a stable machine-readable contract for automation.**

---

## What is `capagent`?

Managing heterogeneous container environments across diverse Linux fleets (RHEL, Fedora, Debian, Ubuntu) and varying Podman / Docker / containerd versions is fraught with brittle heuristic checks in Ansible playbooks.

Instead of writing fragile version-matching logic:
```yaml
# Fragile heuristic checks scattered across automation
when:
  - podman_version >= 4.4
  - cgroup_version == "v2"
  - quadlet_generator_exists
  - systemd_version >= 250
```

The planned report contract enables declarative, capability-oriented assertions:
```yaml
# Declarative, decoupled capability assertion
when:
  - host_capabilities['capabilities']['container.lifecycle.systemd_native']['state'] == "supported"
```

---

## Core Documentation

Explore the comprehensive design specifications:

- 📖 **[Philosophy & Core Principles](docs/philosophy.md)**: The 20 engineering principles, evidence before inference, unknown is not false, TDD requirements, and Definition of Done.
- 🏛️ **[Architecture & Contract](docs/architecture.md)**: Conceptual pipeline, package boundaries, 3-valued requirement logic truth tables, execution context, and JSON Schema v1 specification.
- 🗺️ **[Milestone Roadmap & Catalog](docs/roadmap.md)**: M0 through M21 milestone sequence, detailed issue specifications, exit criteria, and canonical capability catalog.

---

## Conceptual Pipeline

```text
Facts
  │  (Direct kernel, filesystem, and runtime inspection)
  ▼
Observations
  │  (Structured probe results)
  ▼
Evidence
  │  (Precedence: live > runtime > config > knowledge > heuristic)
  ▼
Capabilities
  │  (Canonical IDs: supported | unsupported | misconfigured | unavailable | unknown)
  ▼
Requirements
  │  (Declarative workload needs: SATISFIED | UNSATISFIED | INDETERMINATE)
  ▼
Deployment Decision
```

---

## Current Status and Roadmap

The current CLI provides `--help` and `--version`; it does not yet evaluate hosts or accept `--json`. M0 and the initial M1 scaffolding are delivered. M1.1 is the open hardening gate, followed by the M1.2 context/evaluation slice. The full [roadmap](docs/roadmap.md) links each deliverable to its GitHub issues and milestones.

Execution targets Linux 5.6+ with working `openat2` confinement on amd64/arm64. Historical distribution fixtures do not imply supported execution on older kernels. See the [security and support contract](docs/security.md).

| Milestone | Status | Deliverable |
|---|---|---|
| [M0 — Architecture & Contract](https://github.com/EpicBlackWolfZ/capagent/milestone/1) | Initial skeleton delivered | Initial models, state algebra and schema skeleton; remaining semantics have explicit follow-ups. |
| [M1 — Probe Core](https://github.com/EpicBlackWolfZ/capagent/milestone/2) | Initial foundation delivered | Static payload, OS abstractions and scheduler scaffolding; host-report CLI is not delivered. |
| [M1.1 — Hardening, Security & Performance](https://github.com/EpicBlackWolfZ/capagent/milestone/23) | Active gate | Confinement, descriptor ownership, bounded I/O, parser fidelity, execution/build policy and regression gate. |
| [M1.2 — Context & Evaluation Slice](https://github.com/EpicBlackWolfZ/capagent/milestone/24) | Queued; pure work may overlap | Typed context/evidence, minimal evaluator/requirements, reusable fixtures, application lifecycle and JSON consumer smoke. |
| [M2 — Host Facts](https://github.com/EpicBlackWolfZ/capagent/milestone/3) | Queued | First live host probes, mockable syscall adapters and real-host JSON validation. |
| [M3 — Execution Context](https://github.com/EpicBlackWolfZ/capagent/milestone/4) | Queued | Target identity validation, groups, subordinate ranges, helper metadata, XDG and user systemd. |
| [M4 — Runtime Discovery](https://github.com/EpicBlackWolfZ/capagent/milestone/5) | Queued | Explicit binary/endpoint/identity policy and runtime availability. |
| [M5 — Podman Core](https://github.com/EpicBlackWolfZ/capagent/milestone/6) | Queued | Bounded passive version/effective-info observations and first real rootless fixture. |
| [M6 — Configuration Discovery](https://github.com/EpicBlackWolfZ/capagent/milestone/7) | Queued | Effective configuration and provenance across source/user scopes. |
| [M7 — Configuration Capabilities](https://github.com/EpicBlackWolfZ/capagent/milestone/8) | Queued | Use the M1.2 evaluator for registry/storage/network mappings. |
| [M8 — Capability Catalog Expansion](https://github.com/EpicBlackWolfZ/capagent/milestone/9) | Queued | Expand lifecycle and dependency mappings; engine infrastructure already exists. |
| [M9 — Requirement Language & Explanations](https://github.com/EpicBlackWolfZ/capagent/milestone/10) | Queued | Extend the minimal AST and consumer-driven language/explanation features. |
| [M10 — Ansible Integration](https://github.com/EpicBlackWolfZ/capagent/milestone/11) | Queued | Full role and real-host gating/fallback examples; initial consumer smoke occurs in M1.2. |
| [M11 — Diagnostics](https://github.com/EpicBlackWolfZ/capagent/milestone/12) | Queued | Doctor, runtime/config inspection and evidence explanations. |
| [M12 — Docker](https://github.com/EpicBlackWolfZ/capagent/milestone/13) | Queued | Useful Docker baseline with real fixtures and target-scoped mappings. |
| [M13 — containerd & CRI](https://github.com/EpicBlackWolfZ/capagent/milestone/14) | Queued | Baseline CRI support; CRI-O/nerdctl remain optional experimental scope. |
| [M14 — Historical Knowledge](https://github.com/EpicBlackWolfZ/capagent/milestone/15) | Queued | Sourced high-impact rules that never override direct evidence. |
| [M15 — Compatibility Corpus Expansion](https://github.com/EpicBlackWolfZ/capagent/milestone/16) | Queued | Broaden the M1.2 harness and captured regression corpus. |
| [M16 — Real Compatibility Matrix](https://github.com/EpicBlackWolfZ/capagent/milestone/17) | Queued | Automate a wider supported OS/runtime matrix; first real tests occur earlier. |
| [M17 — Active Validation](https://github.com/EpicBlackWolfZ/capagent/milestone/18) | Queued; experimental | Explicit opt-in, bounded disposable verification and documented cleanup limits. |
| [M18 — Production Hardening](https://github.com/EpicBlackWolfZ/capagent/milestone/19) | Queued | Final integrated fleet checks and broader benchmark/scalability work. |
| [M19 — API Stabilization](https://github.com/EpicBlackWolfZ/capagent/milestone/20) | Queued | Freeze validated schema, evidence, requirement and ID compatibility contracts. |
| [M20 — Documentation](https://github.com/EpicBlackWolfZ/capagent/milestone/21) | Queued | Complete installation, operation, integration and troubleshooting guides. |
| [M21 — v1.0](https://github.com/EpicBlackWolfZ/capagent/milestone/22) | Queued | Release verified stable scope; optional experimental features do not block it. |


---

## Building and checking release artifacts

Build on Linux with Go 1.27.1, Bash, Python 3 (standard library), curl, and
GoReleaser 2.18.0. The build downloads only repository-pinned microfat inputs and
verifies cached inputs on every use. See the [build trust contract](docs/security.md#build-and-artifact-trust)
and [tooling update procedure](scripts/trust/README.md).

```bash
make build          # Full development launcher -> bin/capagent
make release-check  # Also package and verify minimal release launchers; never publishes
```

The release rehearsal additionally requires Syft 1.51.1. Its output includes both
architecture archives, SPDX/CycloneDX SBOMs, checksums, and `release-inputs.json` in
`dist/release/`; `dist/release-inputs.tar.gz` is the verified publisher handoff.
Both architectures receive static/integrity checks; ordinary amd64 CI executes the
amd64 binary. No native ARM64 execution claim is made by these checks.

For the full local source gate, install golangci-lint 2.13.2, govulncheck 1.7.0,
Gitleaks 8.30.1, actionlint 1.7.12, and ShellCheck 0.11.0, then run `make all`,
`make build-contracts`, and `gitleaks git --redact --no-banner`. CI provisions exact versions; missing required validators
fail rather than reducing the checks. Cosign 3.0.6 is required for trusted release
signing and for reviewing new upstream microfat trust data, not for an offline
build using an already verified cache.

PR CI and manual CI/Release dispatch run non-publishing verification. A release-tag
push runs the full gate before a separate publisher signs and uploads its verified
artifacts. Existing published releases are never silently replaced.

---

## Design Principles

1. **Passive by Default**: Never creates containers, modifies networks, or alters system files unless `--active` is explicitly passed.
2. **Evidence Before Inference**: Direct live evidence beats runtime-reported effective state, configuration, version knowledge and heuristics, in that order.
3. **Unknown is Not False**: Distinguishes `supported`, `unsupported`, `misconfigured`, `unavailable`, and `unknown`.
4. **Context is First-Class**: Evaluates capabilities under the target user's identity, subuid/subgid mapping, and runtime directory.
5. **Zero External Runtime Dependencies**: Compiles with `CGO_ENABLED=0` to a single statically linked binary; uses direct kernel APIs (`/proc`, `/sys`, syscalls) over spawning shell utilities.
