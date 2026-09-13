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
- 🗺️ **[Milestone Roadmap & Catalog](docs/roadmap.md)**: Podman-first delivery order, milestone ownership, assessment checkpoint and planned capability catalog.
- **[Foundation testing](docs/testing.md)**: Run the M1.1 gate, bounded fuzzing, deterministic faults, resource regressions, and nightly campaigns.

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

The CLI collects [passive host facts](docs/host-facts.md) with `--json` and evaluates offline evidence with `--fixture DIR --json --pretty`. Local `--runtime podman` assessment adds target identity, executable discovery, [OCI/helper and Quadlet prerequisites](docs/podman-prerequisites.md), [engine configuration provenance](docs/podman-engine-config.md), and [storage prerequisites](docs/podman-storage-config.md). Explicit `--runtime podman --active` collects the selected CLI version and local effective runtime information, enabling qualified configuration source selection. It permits Podman startup writes; exit 0 establishes successful inspection, not workload readiness. See [Podman discovery](docs/podman-discovery.md) for command authority and [fixture evaluation](docs/fixture-evaluation.md) for requirement outcomes and exits.

Root can delegate to a local account with `--context=user:NAME` or `--context=uid:ID`. Reports include subordinate allocations and user-session metadata; `--active` permits the bounded user-manager query. See [execution contexts](docs/execution-context.md). Schema v1 remains pre-release. The [roadmap](docs/roadmap.md) tracks broader delivery.

Execution targets Linux 5.6+ with working `openat2` confinement on amd64/arm64. Historical distribution fixtures do not imply supported execution on older kernels. See the [security and support contract](docs/security.md).

The foundation, fixture evaluation, host facts and execution-context milestones are closed. Baseline correctness fixes, OCI/helper observations, Quadlet prerequisites, engine configuration and storage assessment are delivered. The remaining delivery priorities are:

| Order | Outcome |
|---|---|
| 1 | Rootless networking configuration with mappings and provenance. |
| 2 | Existing JSON requirements over live Podman evidence, with useful explanations. |
| 3 | Registry/image-policy configuration and remaining Podman lifecycle metadata. |
| 4 | [Qualified Podman assessment](https://github.com/EpicBlackWolfZ/capagent/issues/115), focused on rootless Quadlet/user systemd with rootful coverage. |
| 5 | Ansible integration and Docker/containerd baselines, then broader qualification and stable v1.0. |

Each feature includes its tests and user documentation. The first Podman checkpoint remains prerelease; the full v1.0 scope still includes Ansible and the other runtime baselines. See the [roadmap](docs/roadmap.md) for exact issue dependencies and milestone ownership.

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
