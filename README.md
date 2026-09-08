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

`capagent` enables declarative, capability-oriented assertions:
```yaml
# Declarative, decoupled capability assertion
when:
  - host_capabilities.capabilities.container.lifecycle.systemd_native.state == "supported"
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

## Roadmap & Milestones

| Milestone | Title | Focus Area | Status |
| :--- | :--- | :--- | :--- |
| **M0** | [Architecture & Contract](docs/roadmap.md#milestone-0--architecture-semantics--contract) | Domain types, 3-valued truth tables, schema skeleton | 🏗️ Planned |
| **M1** | [Probe Core](docs/roadmap.md#milestone-1--portable-probe-core) | Static binary, mockable OS readers, command runner | ⏳ Queued |
| **M2** | [Host Facts](docs/roadmap.md#milestone-2--host-fact-engine) | Direct /proc, /sys, cgroups v1/v2, systemd, namespaces | ⏳ Queued |
| **M3** | [Execution Context](docs/roadmap.md#milestone-3--identity--rootless-context) | Target user, subuid/subgid, user-systemd, rootless | ⏳ Queued |
| **M4** | [Runtime Discovery](docs/roadmap.md#milestone-4--runtime-discovery) | Podman, Docker, containerd, socket discovery | ⏳ Queued |
| **M5** | [Podman Core](docs/roadmap.md#milestone-5--podman-core-runtime-adapter) | Version parser, podman info, Quadlet, network backend | ⏳ Queued |
| **M6** | [Configuration Discovery](docs/roadmap.md#milestone-6--configuration-discovery-engine) | containers.conf, registries.conf, storage.conf precedence | ⏳ Queued |
| **M7** | [Configuration Capabilities](docs/roadmap.md#milestone-7--configuration-capabilities) | Registries, storage drivers, container DNS capabilities | ⏳ Queued |
| **M8** | [Capability Engine](docs/roadmap.md#milestone-8--canonical-capability-engine) | Canonical capability registry, dependency DAG, evidence graph | ⏳ Queued |
| **M9** | [Requirement Engine](docs/roadmap.md#milestone-9--requirement-engine) | 3-valued requirement evaluation (AND, OR, predicate NOT) | ⏳ Queued |
| **M10** | [Ansible Integration](docs/roadmap.md#milestone-10--early-ansible-integration) | Playbooks, gating examples, capagent_probe role | ⏳ Queued |
| **M11** | [Diagnostics](docs/roadmap.md#milestone-11--diagnostics--doctor) | CLI doctor, runtime inspection, terminal evidence tree | ⏳ Queued |
| **M12** | [Docker Adapter](docs/roadmap.md#milestone-12--docker-runtime-adapter) | Docker engine observation and canonical mapping | ⏳ Queued |
| **M13** | [containerd & CRI](docs/roadmap.md#milestone-13--containerd--cri--cri-o--nerdctl) | CRI plugins, containerd sockets, CRI-O, nerdctl | ⏳ Queued |
| **M14** | [Historical Knowledge](docs/roadmap.md#milestone-14--historical-knowledge-base) | Provenance tracking, version rules without overriding live evidence | ⏳ Queued |
| **M15** | [Compatibility Fixtures](docs/roadmap.md#milestone-15--compatibility-fixture-framework) | Offline simulation corpus across OS and runtime versions | ⏳ Queued |
| **M16** | [Real Compatibility Matrix](docs/roadmap.md#milestone-16--real-os--runtime-compatibility-matrix) | Validation against RHEL 8/9/10, Fedora, Debian | ⏳ Queued |
| **M17** | [Active Validation](docs/roadmap.md#milestone-17--active-validation) | Safe opt-in active probing (`--active`) with cleanup | ⏳ Queued |
| **M18** | [Production Hardening](docs/roadmap.md#milestone-18--production-hardening) | Security audit, resource limits, stripped-host execution | ⏳ Queued |
| **M19** | [API Stabilization](docs/roadmap.md#milestone-19--api-stabilization) | JSON Schema v1 freeze, capability ID stability | ⏳ Queued |
| **M20** | [Documentation & DX](docs/roadmap.md#milestone-20--documentation--developer-experience) | Contributor guides, authoring playbooks | ⏳ Queued |
| **M21** | [v1.0 Release](docs/roadmap.md#milestone-21--v10-release) | Production release with stable contract | ⏳ Queued |

---

## Design Principles

1. **Passive by Default**: Never creates containers, modifies networks, or alters system files unless `--active` is explicitly passed.
2. **Evidence Before Inference**: Direct live evidence beats configuration files, which beat version knowledge, which beats heuristics.
3. **Unknown is Not False**: Distinguishes `supported`, `unsupported`, `misconfigured`, `unavailable`, and `unknown`.
4. **Context is First-Class**: Evaluates capabilities under the target user's identity, subuid/subgid mapping, and runtime directory.
5. **Zero External Runtime Dependencies**: Compiles with `CGO_ENABLED=0` to a single statically linked binary; uses direct kernel APIs (`/proc`, `/sys`, syscalls) over spawning shell utilities.