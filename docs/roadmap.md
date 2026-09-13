# Container Capability Engine Roadmap

Podman probes and configuration are the next priority. The first assessment focuses on rootless Quadlet with user systemd, with rootful coverage alongside it. Ansible integration follows a qualified Podman assessment. GitHub issues own actionable acceptance; milestone numbers remain stable identifiers rather than a mandatory execution sequence.

## Current availability

The foundation, fixture evaluation path, passive host facts and execution contexts are delivered: M0, M1, M1.1, M1.2, M2 and M3 are closed. The shared pipeline supports typed observations, scoped evidence, five-state capabilities, bounded JSON requirements and reports. Schema v1 remains prerelease.

The CLI provides:

- [Passive host facts](host-facts.md) and [local target identities](execution-context.md), including root delegation, subordinate allocations, helper metadata, runtime-directory and user-session observations.
- [Passive local Podman executable discovery](podman-discovery.md) with explicit path and target selection.
- Explicit `--runtime podman --active` version and local effective-info inspection. Podman startup may write state; successful inspection does not establish workload readiness.
- An explicitly active bounded user-manager query, including host-only collection.
- [Offline fixture evaluation](fixture-evaluation.md), the existing JSON requirement AST and an executable report consumer.

Live Podman inspection currently evaluates `all(runtime.podman, runtime.podman.info)`. User-supplied live requirements and basic evidence explanations are planned in [#112](https://github.com/EpicBlackWolfZ/capagent/issues/112). Configuration-family discovery, broader helper/lifecycle mappings and workload verification remain planned. The [baseline follow-up milestone](https://github.com/EpicBlackWolfZ/capagent/milestone/25) tracks known correctness and validation defects in the shipped behavior.

Execution targets **Linux 5.6+ with functioning `openat2` confinement**, on amd64 and arm64. A version string does not prove that the syscall is permitted; unsupported kernels or policies receive explicit diagnostics without an insecure fallback. Historical pre-5.6 distribution data can support parser fixtures without establishing execution support. See the [security and support contract](security.md).

## Delivery sequence

Each feature batch includes its observations/parsing, affected capability mappings, explanations, fixtures, relevant native validation and user documentation. Configuration discovery, mappings and catalog milestones progress together. Wider corpus and matrix milestones expand testing that already accompanies features.

| Order | User-visible outcome | Delivery issues |
|---|---|---|
| 0 | Correct incomplete-result reporting and strengthen delegated passive validation | [#102](https://github.com/EpicBlackWolfZ/capagent/issues/102)–[#107](https://github.com/EpicBlackWolfZ/capagent/issues/107); roadmap alignment [#116](https://github.com/EpicBlackWolfZ/capagent/issues/116) |
| 1 | Explain selected OCI/helper and target-scoped Quadlet prerequisites | [#82](https://github.com/EpicBlackWolfZ/capagent/issues/82), [#108](https://github.com/EpicBlackWolfZ/capagent/issues/108) |
| 2 | Explain engine, storage and rootless networking configuration with source provenance | [#109](https://github.com/EpicBlackWolfZ/capagent/issues/109), [#110](https://github.com/EpicBlackWolfZ/capagent/issues/110), [#111](https://github.com/EpicBlackWolfZ/capagent/issues/111) |
| 3 | Evaluate existing JSON requirements against live Podman evidence and show useful explanations | [#112](https://github.com/EpicBlackWolfZ/capagent/issues/112), the early shared slice of M9/M11 |
| 4 | Add registry/image-policy configuration and remaining lifecycle metadata | [#113](https://github.com/EpicBlackWolfZ/capagent/issues/113), [#114](https://github.com/EpicBlackWolfZ/capagent/issues/114); specific sourced rules from [#74](https://github.com/EpicBlackWolfZ/capagent/issues/74) when needed |
| 5 | Qualify the Podman assessment on a documented rootless/rootful support envelope | [#115](https://github.com/EpicBlackWolfZ/capagent/issues/115), bringing forward Podman portions of M15/M16/M18 |
| 6 | Deliver Ansible integration and useful Docker/containerd baselines | [#70](https://github.com/EpicBlackWolfZ/capagent/issues/70), remaining shared discovery [#23](https://github.com/EpicBlackWolfZ/capagent/issues/23), [#72](https://github.com/EpicBlackWolfZ/capagent/issues/72), [#73](https://github.com/EpicBlackWolfZ/capagent/issues/73) |
| 7 | Expand fleet qualification, finish consumer-driven UX, freeze the contract and complete stable v1.0 | Remaining M9/M11/M14–M16/M18 scope, then [#79](https://github.com/EpicBlackWolfZ/capagent/issues/79)–[#81](https://github.com/EpicBlackWolfZ/capagent/issues/81) |

The remaining generic discovery issue does not block local Podman probes or configuration. Optional YAML, named profiles and a complete doctor command suite do not block the first live assessment. Within a phase, independently testable parser or domain work may proceed together; issue dependencies define the integration order.

[M17 workload verification](https://github.com/EpicBlackWolfZ/capagent/issues/77) is an optional experimental track after the Podman checkpoint and the specific prerequisites its checks consume. It does not wait for every runtime/distribution matrix entry. Existing active inspection remains a separate, already delivered operation; selecting it will not implicitly authorize future container/DNS/registry/storage experiments.

## First Podman assessment checkpoint

The initial useful workflow answers which prerequisites a selected target user has for rootless Quadlet deployment and which engine, storage, network or image-policy settings are missing, conflicting or unobserved. It includes a rootful comparison and uses only delivered capability definitions in its requirement examples.

The checkpoint requires the baseline fixes, helper/Quadlet observations, configuration batches, live JSON requirement input and integrated qualification in [#115](https://github.com/EpicBlackWolfZ/capagent/issues/115). Each result must identify its evidence level: file presence, configured intent, runtime-effective state or directly verified operation. A generator/helper file or successful `podman info` cannot establish a functioning workload.

This is a prerelease readiness checkpoint. It neither freezes schema v1 nor replaces the broader stable v1.0 scope. Publishing a prerelease is a separate release action.

## Configuration delivery contract

Configuration batches share source provenance and explicit target/environment selection, while keeping family-specific semantics:

- `containers.conf` uses layered fields and version-applicable drop-in/array rules. Modules and overrides require explicit selection. See the [upstream engine configuration contract](https://github.com/containers/common/blob/main/docs/containers.conf.5.md).
- `storage.conf` uses its own selected-file/replacement rules; a universal field-by-field TOML overlay would be incorrect. See the [upstream storage configuration contract](https://github.com/containers/storage/blob/main/storage.conf).
- `registries.conf` and `policy.json` need their own source-selection and override rules, covered by version-specific fixtures in #113.

Source applicability is pinned when a feature is implemented; current upstream documentation is not proof that every historical Podman release behaves identically. Parsed configuration and runtime inspection must use compatible declared source/environment policy. Ambient config-selection overrides and remote endpoints cannot silently change one side of the assessment.

Runtime-effective state outranks configuration within the same target/runtime scope. Missing, denied, malformed and unsupported inputs remain distinguishable. Configuration can establish selected settings and prerequisites; registry reachability, authentication, image pulls, network/DNS operation and storage writes require separately authorized direct evidence.

## Milestone ownership

Existing milestone URLs and numbers are preserved. The table describes delivery ownership; it does not impose an all-or-nothing gate between neighboring rows.

| Milestone | Current status and remaining ownership |
|---|---|
| [M0 — Architecture & Contract](https://github.com/EpicBlackWolfZ/capagent/milestone/1) | Closed historical domain/schema foundation. |
| [M1 — Probe Core](https://github.com/EpicBlackWolfZ/capagent/milestone/2) | Closed platform/scheduler foundation. |
| [M1.1 — Hardening, Security & Performance](https://github.com/EpicBlackWolfZ/capagent/milestone/23) | Closed foundation; the permanent gate continues to apply. |
| [M1.2 — Context & Evaluation Slice](https://github.com/EpicBlackWolfZ/capagent/milestone/24) | Closed fixture-to-consumer pipeline, now reused by live collection. |
| [M2 — Host Facts](https://github.com/EpicBlackWolfZ/capagent/milestone/3) | Closed passive host observation baseline. |
| [M3 — Execution Context](https://github.com/EpicBlackWolfZ/capagent/milestone/4) | Closed target/delegation/subordinate-ID/user-session baseline. |
| [Baseline correctness follow-up](https://github.com/EpicBlackWolfZ/capagent/milestone/25) | Phase 0: #102–#107 repairs and #116 public alignment. |
| [M4 — Runtime Discovery](https://github.com/EpicBlackWolfZ/capagent/milestone/5) | Local Podman discovery delivered; #23 shared discovery follows the Podman checkpoint. |
| [M5 — Podman Core](https://github.com/EpicBlackWolfZ/capagent/milestone/6) | Active version/info delivered; #82/#108 next, #114 later. |
| [M6 — Configuration Discovery](https://github.com/EpicBlackWolfZ/capagent/milestone/7) | #66 roll-up; #109/#110/#113 own family discovery with mappings. |
| [M7 — Configuration Capabilities](https://github.com/EpicBlackWolfZ/capagent/milestone/8) | Narrow runtime mappings delivered; #67 roll-up and #111 network batch progress with M6. |
| [M8 — Capability Catalog Expansion](https://github.com/EpicBlackWolfZ/capagent/milestone/9) | #68 definitions and dependency explanations ship with each producing batch. |
| [M9 — Requirement Language & Explanations](https://github.com/EpicBlackWolfZ/capagent/milestone/10) | #112 live JSON first; #69 broader consumer-driven language later. |
| [M10 — Ansible Integration](https://github.com/EpicBlackWolfZ/capagent/milestone/11) | #70 follows #115; retain the existing consumer smoke now. |
| [M11 — Diagnostics](https://github.com/EpicBlackWolfZ/capagent/milestone/12) | Basic explanations in #112; #71 full inspection/doctor UX later. |
| [M12 — Docker](https://github.com/EpicBlackWolfZ/capagent/milestone/13) | #72 baseline after the Podman checkpoint. |
| [M13 — containerd & CRI](https://github.com/EpicBlackWolfZ/capagent/milestone/14) | #73 baseline after the checkpoint; CRI-O/nerdctl optional. |
| [M14 — Historical Knowledge](https://github.com/EpicBlackWolfZ/capagent/milestone/15) | #74 specific sourced rules with features, broader coverage later. |
| [M15 — Compatibility Corpus Expansion](https://github.com/EpicBlackWolfZ/capagent/milestone/16) | Feature fixtures now; #115 Podman corpus, #75 wider coverage later. |
| [M16 — Real Compatibility Matrix](https://github.com/EpicBlackWolfZ/capagent/milestone/17) | #115 Podman qualification first; #76 wider runtime/OS matrix later. |
| [M17 — Active Validation](https://github.com/EpicBlackWolfZ/capagent/milestone/18) | Inspection gate delivered; #77 optional bounded workload experiments later. |
| [M18 — Production Hardening](https://github.com/EpicBlackWolfZ/capagent/milestone/19) | Small checks with features/#115; #78 final fleet qualification and #41 broader benchmarks. |
| [M19 — API Stabilization](https://github.com/EpicBlackWolfZ/capagent/milestone/20) | #79 freeze after stable runtime/consumer/matrix validation. |
| [M20 — Documentation](https://github.com/EpicBlackWolfZ/capagent/milestone/21) | Docs ship with features; #80 completes stable-scope guides. |
| [M21 — v1.0](https://github.com/EpicBlackWolfZ/capagent/milestone/22) | #81 release acceptance for the full stable scope. |

## Validation and stable release scope

The [permanent hardening gate](testing.md#complete-m11-gate) applies to every extension: confinement, explicit command authority, bounded I/O, retained partial observations, race/lint/contracts and artifact validation. Model and requirement coverage remain independently above 95%. Small deterministic checks accompany changes; deeper stochastic, matrix and benchmark campaigns remain separately replayable.

The stable v1.0 scope remains supported Linux host evaluation, explicit target contexts, Podman/Docker/containerd baseline, canonical capabilities, requirements, diagnostics, Ansible integration and a stable machine-readable contract. CRI-O, nerdctl and workload experiments may remain experimental. Every shipped capability follows the [Definition of Done](philosophy.md#4-definitions-of-done).

Public docs and executable examples ship with each feature. M20 completes coverage; internal implementation plans, audits and machine-specific evidence stay in git-ignored `.work/`.

---

## Planned capability catalog

This is a design inventory, not a list of fully implemented or operationally verified features. The shipped Podman definitions are `runtime.podman`, `runtime.podman.info` and the narrow `runtime.podman.netavark` prerequisite. Host/context observations do not automatically imply corresponding capability mappings. The `context` namespace and bounded nullable identity contract are implemented; schema v1 remains prerelease.

The roadmap retains intended IDs while #68 defines each shipped predicate's exact evidence threshold. Entries that describe working deployment, storage, networking, DNS or image access require evidence appropriate to that operation. A passive implementation must expose only its measured prerequisites, or leave the operational result unknown. It cannot silently redefine operational support as file presence.

### 1 Host Capabilities

- `host.os`: Host distribution identification.
- `host.kernel`: Kernel version and capabilities.
- `host.architecture`: CPU architecture (`amd64`, `arm64`).
- `host.systemd`: Systemd init system presence and version.
- `host.systemd.user`: User systemd manager accessibility.
- `host.cgroups.v1`: Legacy cgroup v1 support.
- `host.cgroups.v2`: Modern cgroup v2 support.
- `host.cgroups.controllers`: Delegated cgroup controllers (cpu, memory, io, pids).
- `host.namespaces.user`: User namespace support.
- `host.namespaces.net`: Network namespace support.
- `host.namespaces.mount`: Mount namespace support.
- `host.namespaces.pid`: PID namespace support.
- `host.selinux`: SELinux enforcement state.
- `host.apparmor`: AppArmor security status.
- `host.seccomp`: Kernel seccomp syscall filtering.
- `host.ipv4`: IPv4 networking capability.
- `host.ipv6`: IPv6 networking capability.
- `host.overlayfs`: Kernel OverlayFS support.
- `host.fuse`: FUSE userland filesystem support.
- `host.root`: Current execution is root (`EUID == 0`).
- `host.rootless`: Current execution is unprivileged.
- `host.dns`: Host resolver reachability.
- `host.firewall`: Host firewall presence (`nftables`, `iptables`, `firewalld`).

### 2 Execution Context Capabilities

- `context.root`: Process running as root.
- `context.rootless`: Process running unprivileged.
- `context.user`: Valid target user resolution.
- `context.user_namespaces`: Unprivileged user namespace creation permitted.
- `context.user_systemd`: Target user has active systemd instance.
- `context.user_runtime_dir`: Target user has accessible `XDG_RUNTIME_DIR`.
- `context.subuid`: Target user has allocated subordinate UID range.
- `context.subgid`: Target user has allocated subordinate GID range.

### 3 Lifecycle Capabilities

- `container.lifecycle.systemd_native`: Native systemd unit generation / Quadlet.
- `container.lifecycle.systemd_generated`: Generated unit deployment (`podman generate systemd`).
- `container.lifecycle.daemon_managed`: Traditional daemon restart policy management (Docker).
- `container.lifecycle.cri`: CRI pod lifecycle management.
- `container.lifecycle.kubernetes`: `podman kube apply` manifest execution.

### 4 Runtime Specific Capabilities

- `runtime.podman`: Selected Podman CLI returned a recognizable version; engine access remains unverified.
- `runtime.podman.quadlet`: Quadlet support under a defined target-scoped evidence contract; generator presence and functional generation remain distinct.
- `runtime.podman.kube_apply`: Podman `kube play`/`apply` support.
- `runtime.podman.generate_systemd`: Podman legacy systemd unit generation.
- `runtime.podman.info`: Selected local engine returned complete effective inspection fields; runtime evidence with derived confidence, no workload claim.
- `runtime.podman.netavark`: Netavark network backend configured.
- `runtime.podman.cni`: Legacy CNI network backend configured.
- `runtime.podman.aardvark`: Aardvark DNS support; configured helper prerequisites and verified DNS operation remain distinct.
- `runtime.podman.rootless`: Rootless Podman operation for the selected target; prerequisites alone cannot establish usability.
- `runtime.podman.pasta`: Pasta rootless networking backend.
- `runtime.podman.infra_image`: Infra/pause image configured for pods.
- `runtime.docker`: Docker daemon running and accessible.
- `runtime.docker.rootless`: Docker Rootless daemon accessible.
- `runtime.docker.custom_dns`: Docker custom bridge embedded DNS.
- `runtime.containerd`: containerd gRPC socket connectable.
- `runtime.containerd.cri`: containerd CRI plugin active.

### 5 Storage Capabilities

- `container.storage.overlay`: Native kernel OverlayFS storage driver.
- `container.storage.fuse_overlayfs`: FUSE-overlayfs storage driver (unprivileged).
- `container.storage.vfs`: Fallback VFS storage driver.
- `container.storage.btrfs`: Btrfs native storage driver.
- `container.storage.zfs`: ZFS native storage driver.
- `container.storage.additional_store`: Read-only additional image stores configured.
- `container.storage.rootless`: Storage configured properly for unprivileged user.

### 6 Networking Capabilities

- `container.network`: General container network creation.
- `container.network.ipv4`: IPv4 container addressing.
- `container.network.ipv6`: IPv6 container addressing.
- `container.network.cni`: CNI plugin networking.
- `container.network.netavark`: Netavark bridge networking.
- `container.network.dns`: Container-to-container DNS resolution.
- `container.network.custom_dns`: Custom upstream DNS forwarding in containers.
- `container.network.rootless`: Unprivileged userland networking.
- `container.network.pasta`: Pasta network translation.
- `container.network.slirp4netns`: Slirp4netns userland network translation.
- `container.network.port_forwarding`: Host-to-container port mapping.

### 7 Image & Registry Capabilities

- `image.pull`: Image pull functionality.
- `image.registry`: Unauthenticated public registry access.
- `image.registry.private`: Authenticated private registry access.
- `image.registry.custom`: Custom registry hostname resolution.
- `image.registry.auth`: Target registry authentication prerequisites; credential-source metadata does not establish successful authentication.
- `image.registry.mirror`: Pull-through registry mirror configured.
- `image.registry.insecure`: Insecure (HTTP / non-TLS) registry permitted.
- `image.registry.custom_ca`: Custom TLS root certificate authority configured.
- `image.registry.signature_policy`: Selected image signature policy; configured policy and successful verification remain distinct.
- `image.registry.offline`: Air-gapped / offline image store operation.
- `image.pause`: Pause / infra image available locally.
