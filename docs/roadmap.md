# Container Capability Engine Roadmap

This roadmap separates delivered scaffolding from usable product behavior. GitHub milestones and their linked issues are the delivery record; milestone numbers are stable identifiers, not a requirement to delay shared infrastructure until its original number.

## Current availability

M0/M1 and M1.1 foundation hardening are implemented. The M1.2 fixture slice now runs typed observations through scoped evidence, capabilities, requirements and JSON, with an executable consumer. The CLI supports `--fixture DIR`, `--json`, `--pretty`, `--debug`, help and version. See [fixture evaluation](fixture-evaluation.md) for its narrow Netavark configuration semantics. It does not yet evaluate the live host or invoke Podman. Schema v1 remains pre-release.

The [permanent hardening gate](testing.md#complete-m11-gate) still applies to every extension. M1.2 supplies a reusable application path; subsequent work adds actual host/context discovery and reviewed runtime collection.

Execution support targets **Linux 5.6+ with functioning `openat2` confinement**, on amd64 and arm64. A kernel version alone does not prove the syscall is permitted. Unsupported kernels or policies must produce explicit diagnostics; there is no insecure fallback. Historical RHEL 8 data may be used for parser fixtures, but stock pre-5.6 execution is not a supported target. A broader compatibility policy requires a separate secure implementation and real-host tests.

## Delivery sequence

```text
M0/M1 initial foundation
  → M1.1 hardening gate
  → M1.2 context + evidence + requirements + JSON fixture slice
  → M2 host facts → M3 target context → M4 discovery → M5 Podman
  → M6 configuration → M7 mappings → M8 catalog → M9 language
  → M10 Ansible → M11 diagnostics
  → M12 Docker / M13 containerd baseline
  → M14 knowledge → M15 broader corpus → M16 wider real matrix
  → M18 final hardening → M19 API freeze → M20 guides → M21 release

M17 workload verification is an optional experimental track after its prerequisites. The early phase 3 `--active` slice permits only local Podman version/info inspection and its startup effects.
```

Pure parser, fixture and domain work may proceed before the hardening gate closes. Integration that executes platform or runtime operations must use the completed M1.1 contracts. Each capability gets fixtures and real validation when introduced; M15/M16 expand coverage rather than introducing testing for the first time.

| Milestone | Status | Deliverable |
|---|---|---|
| [M0 — Architecture & Contract](https://github.com/EpicBlackWolfZ/capagent/milestone/1) | Initial skeleton delivered | Initial models, state algebra and schema skeleton; remaining semantics have explicit follow-ups. |
| [M1 — Probe Core](https://github.com/EpicBlackWolfZ/capagent/milestone/2) | Initial foundation delivered | Static payload, OS abstractions and scheduler scaffolding; host-report CLI is not delivered. |
| [M1.1 — Hardening, Security & Performance](https://github.com/EpicBlackWolfZ/capagent/milestone/23) | Implemented; closure requires passing gate | Confinement, descriptor ownership, bounded I/O, parser fidelity, execution/build policy and regression gate. |
| [M1.2 — Context & Evaluation Slice](https://github.com/EpicBlackWolfZ/capagent/milestone/24) | Implemented fixture slice | Typed context/evidence, minimal evaluator/requirements, reusable fixtures, application lifecycle and JSON consumer smoke. |
| [M2 — Host Facts](https://github.com/EpicBlackWolfZ/capagent/milestone/3) | Queued | First live host probes, mockable syscall adapters and real-host JSON validation. |
| [M3 — Execution Context](https://github.com/EpicBlackWolfZ/capagent/milestone/4) | Queued | Target identity validation, groups, subordinate ranges, helper metadata, XDG and user systemd. |
| [M4 — Runtime Discovery](https://github.com/EpicBlackWolfZ/capagent/milestone/5) | Queued | Explicit binary/endpoint/identity policy and runtime availability. |
| [M5 — Podman Core](https://github.com/EpicBlackWolfZ/capagent/milestone/6) | Queued | Version discovery and explicitly gated effective-info observations and first real rootless fixture. |
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


## Foundation exit gates

### M1.1 — harden the execution foundation

| Phase | Issues | Exit evidence |
|---|---|---|
| Filesystem correctness and resource ownership | [#30](https://github.com/EpicBlackWolfZ/capagent/issues/30), [#33](https://github.com/EpicBlackWolfZ/capagent/issues/33), [#42](https://github.com/EpicBlackWolfZ/capagent/issues/42), [#56](https://github.com/EpicBlackWolfZ/capagent/issues/56), [#57](https://github.com/EpicBlackWolfZ/capagent/issues/57), [#58](https://github.com/EpicBlackWolfZ/capagent/issues/58) | Correct path/root semantics, faithful metadata, close-on-exec, bounded reads, explicit incomplete results and OS/memory conformance. |
| Execution and lifecycle | [#36](https://github.com/EpicBlackWolfZ/capagent/issues/36), [#37](https://github.com/EpicBlackWolfZ/capagent/issues/37), [#39](https://github.com/EpicBlackWolfZ/capagent/issues/39), [#40](https://github.com/EpicBlackWolfZ/capagent/issues/40), [#43](https://github.com/EpicBlackWolfZ/capagent/issues/43) | Explicit executable/environment authority, bounded pipe drain, immutable DAG, retained failure observations and terminal resolution errors. |
| Build trust and public contracts | [#31](https://github.com/EpicBlackWolfZ/capagent/issues/31), [#38](https://github.com/EpicBlackWolfZ/capagent/issues/38), [#47](https://github.com/EpicBlackWolfZ/capagent/issues/47), [#48](https://github.com/EpicBlackWolfZ/capagent/issues/48) | Verified launcher inputs/cache, provisioned release tools, precise architecture checks and documented supported behavior. |
| Permanent regression gate | [#44](https://github.com/EpicBlackWolfZ/capagent/issues/44), [#45](https://github.com/EpicBlackWolfZ/capagent/issues/45), [#46](https://github.com/EpicBlackWolfZ/capagent/issues/46), [#49](https://github.com/EpicBlackWolfZ/capagent/issues/49) | Deterministic fuzz corpus/fault/resource regressions, full lint/race/contracts and packaged-artifact verification. |

The gate requires no unresolved high-priority defect in the shipped foundation. Model and requirement coverage are checked independently above 95%, alongside the repository threshold. Small benchmark checks accompany relevant changes; broader benchmark/scalability issue [#41](https://github.com/EpicBlackWolfZ/capagent/issues/41) belongs to M18. Expensive stochastic campaigns run separately from the bounded PR gate.

### M1.2 — prove the evaluation path before expanding capabilities

| Deliverable | Issue |
|---|---|
| Typed facts, partial observations and explicit context/runtime scope | [#59](https://github.com/EpicBlackWolfZ/capagent/issues/59) |
| Reusable offline fixtures with provenance | [#60](https://github.com/EpicBlackWolfZ/capagent/issues/60) |
| Application lifecycle, report CLI and executable consumer example | [#61](https://github.com/EpicBlackWolfZ/capagent/issues/61) |
| Scoped precedence, equal-rank conflicts and explanations | [#6](https://github.com/EpicBlackWolfZ/capagent/issues/6) |
| Minimal evidence graph and capability registry/evaluator | [#62](https://github.com/EpicBlackWolfZ/capagent/issues/62) |
| Minimal bounded JSON requirement AST using existing three-valued algebra | [#63](https://github.com/EpicBlackWolfZ/capagent/issues/63) |
| Catalog, numeric schema bounds and unknown/completeness alignment | [#64](https://github.com/EpicBlackWolfZ/capagent/issues/64) |

Exit: one deterministic fixture can pass through facts, observations, evidence, capabilities, requirements and validated CLI JSON into a consumer. Positive, negative and indeterminate results are covered. A workload cannot combine incompatible runtime or target-user evidence. The CLI does not claim live support before live probes are implemented.

## Live observation milestones

| Milestone | Tracked implementation | Exit evidence |
|---|---|---|
| M2 | [#15](https://github.com/EpicBlackWolfZ/capagent/issues/15) os-release; [#16](https://github.com/EpicBlackWolfZ/capagent/issues/16) kernel; [#17](https://github.com/EpicBlackWolfZ/capagent/issues/17) systemd; [#18](https://github.com/EpicBlackWolfZ/capagent/issues/18) cgroups; [#19](https://github.com/EpicBlackWolfZ/capagent/issues/19) namespaces/security; [#65](https://github.com/EpicBlackWolfZ/capagent/issues/65) platform syscall adapters; [#83](https://github.com/EpicBlackWolfZ/capagent/issues/83) filesystem/network prerequisites | Accurate scoped observations, unknown/error preservation and a real supported-host fact-to-JSON test. Version strings and namespace-file presence do not by themselves prove capability usability. |
| M3 | [#3](https://github.com/EpicBlackWolfZ/capagent/issues/3) model validation; [#20](https://github.com/EpicBlackWolfZ/capagent/issues/20) identities; [#21](https://github.com/EpicBlackWolfZ/capagent/issues/21) subordinate IDs/helpers; [#22](https://github.com/EpicBlackWolfZ/capagent/issues/22) XDG/user systemd | Explicit current/target credentials and groups, validated ranges/ownership, and real supported rootless checks. Required subordinate range size is workload-specific. |
| M4 | [#23](https://github.com/EpicBlackWolfZ/capagent/issues/23) runtime discovery | Explicit executable and endpoint policy; binary existence, service availability and target accessibility stay distinct. |
| M5 | [#24](https://github.com/EpicBlackWolfZ/capagent/issues/24) versions; [#25](https://github.com/EpicBlackWolfZ/capagent/issues/25) effective info; [#82](https://github.com/EpicBlackWolfZ/capagent/issues/82) lifecycle/helper observations | Real Podman fixtures and bounded target-scoped observations whose command behavior and opt-in boundary are verified. |

The phase 2 current-user slices of #65/#20/#3 and local Podman discovery from #23 are implemented. Phase 3 adds live opt-in version/info collection for #24/#25, a complete inspection predicate and authentic 3.x/4.x/5.x parser captures. The narrow #67/#68/#77 slices reuse runtime evidence and explicit dispatch; their broader acceptance remains open. Version and info require `--active` because Podman startup can write state. Alternate identities, configuration merging, lifecycle helpers and workload verification remain deferred. See [local Podman discovery](podman-discovery.md).


Read-only system metadata interrogation such as `systemctl --version` must use the explicit command policy. It is not permission for arbitrary shell utilities. Active namespace creation, storage writes, network reachability tests and container execution remain outside default observation.

## Expansion and release ownership

| Milestone | Tracking issue | Scope |
|---|---|---|
| M6 | [#66](https://github.com/EpicBlackWolfZ/capagent/issues/66) | containers.conf, storage.conf, registries.conf, policy.json and drop-ins across vendor/system/user scopes; prefer runtime effective state; preserve source precedence, permissions and parse diagnostics; explicit environment policy and redaction; table-driven conflicting-source fixtures. |
| M7 | [#67](https://github.com/EpicBlackWolfZ/capagent/issues/67) | registry, storage, DNS and rootless-networking mappings using the existing evaluator; all five operational states; confidence reflects passive evidence; no network reachability or storage-write claim without direct authorized evidence. |
| M8 | [#68](https://github.com/EpicBlackWolfZ/capagent/issues/68) | systemd-native/generated lifecycle, networking/storage/image dependencies and evidence explanations; canonical IDs and per-capability Definition of Done; do not duplicate the M1.2 engine. |
| M9 | [#69](https://github.com/EpicBlackWolfZ/capagent/issues/69) | documented JSON language and optional YAML parsing adapter; robust diagnostics and bounded nesting/input; compound predicates and named requirements only with concrete consumers; reuse #5 truth tables. |
| M10 | [#70](https://github.com/EpicBlackWolfZ/capagent/issues/70) | capagent_probe role, flat-key capability gating, requirement documents, fail-closed policy, lifecycle fallback; real supported host and target-user validation. |
| M11 | [#71](https://github.com/EpicBlackWolfZ/capagent/issues/71) | doctor, runtime, config and explain commands; terminal evidence trees; versioned additive JSON diagnostics; secret redaction. |
| M12 | [#72](https://github.com/EpicBlackWolfZ/capagent/issues/72) | binary/socket discovery, version/info, daemon.json, rootless/remote endpoint policy and canonical mappings; minimum useful Docker baseline with real fixtures. |
| M13 | [#73](https://github.com/EpicBlackWolfZ/capagent/issues/73) | containerd discovery, config.toml and bounded read-only socket/CRI inspection; explicit transport dependency decision; scope results to endpoint/namespace; CRI-O and nerdctl remain experimental follow-ups. |
| M14 | [#74](https://github.com/EpicBlackWolfZ/capagent/issues/74) | high-impact version rules with official provenance, validity ranges, timestamps/confidence, and conflict fixtures; supplement missing evidence only. |
| M15 | [#75](https://github.com/EpicBlackWolfZ/capagent/issues/75) | broaden OS/runtime/config/rootless and broken-environment permutations using the existing M1.2 harness; capture provenance and redact secrets; regression corpus from real failures. |
| M16 | [#76](https://github.com/EpicBlackWolfZ/capagent/issues/76) | RHEL 9/10, Fedora, Debian and minimal supported Linux; rootful/rootless, cgroups and security modes; explicit kernel/syscall support checks; baseline real-host tests start with M2/M3 rather than waiting here. |
| M17 | [#77](https://github.com/EpicBlackWolfZ/capagent/issues/77) | separate active registry and explicit --active gate; bounded disposable container/DNS/registry/storage checks; resource ownership ledger and cleanup diagnostics; experimental until validated. |
| M18 | [#78](https://github.com/EpicBlackWolfZ/capagent/issues/78) | benchmark and resource regression budgets, stripped/read-only hosts, restricted procfs/sysfs, artifact static checks and launcher provenance; final security review of integrated feature set. |
| M19 | [#79](https://github.com/EpicBlackWolfZ/capagent/issues/79) | publish final schema, evidence and requirement contracts; ID lifecycle status and generated catalog; cross-version compatibility tests and explicit version negotiation where needed. |
| M20 | [#80](https://github.com/EpicBlackWolfZ/capagent/issues/80) | installation/build requirements, supported kernels/runtimes, CLI, requirement language, Ansible, troubleshooting and verification instructions; executable examples. |
| M21 | [#81](https://github.com/EpicBlackWolfZ/capagent/issues/81) | release checklist for supported Linux, Podman, Docker and containerd baseline; target-user contexts, evidence/requirements, diagnostics and Ansible; optional CRI-O/nerdctl/active probes remain experimental. |

### Stable v1.0 scope

Supported Linux host evaluation, explicit target contexts, Podman/Docker/containerd baseline, canonical capabilities, requirements, diagnostics, Ansible integration and a stable machine-readable contract form the release scope. CRI-O, nerdctl and active probes may remain experimental. Every shipped capability follows the [Definition of Done](philosophy.md#4-definitions-of-done).

Public documentation and executable examples are updated alongside features. M20 completes the guides; it does not postpone documentation until the end. Internal implementation plans, audits and machine-specific evidence belong in git-ignored `.work/` and are not published under `docs/`.

---

## Planned capability catalog

These IDs describe intended scope. `runtime.podman.netavark` is emitted by info fixtures. `runtime.podman` describes CLI availability: live discovery can establish absence or unsuitable metadata, while a supported version result is currently available only through fixture replay. The `context` namespace is accepted, and a contract test validates every catalog ID. Each shipped capability must satisfy the Definition of Done in [philosophy.md](philosophy.md).

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
- `runtime.podman.quadlet`: Podman Quadlet generator present and functional.
- `runtime.podman.kube_apply`: Podman `kube play`/`apply` support.
- `runtime.podman.generate_systemd`: Podman legacy systemd unit generation.
- `runtime.podman.info`: Selected local engine returned complete effective inspection fields; runtime evidence with derived confidence, no workload claim.
- `runtime.podman.netavark`: Netavark network backend configured.
- `runtime.podman.cni`: Legacy CNI network backend configured.
- `runtime.podman.aardvark`: Aardvark container DNS service active.
- `runtime.podman.rootless`: Podman rootless operation usable by target user.
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
- `image.registry.auth`: Auth credentials present for target registry.
- `image.registry.mirror`: Pull-through registry mirror configured.
- `image.registry.insecure`: Insecure (HTTP / non-TLS) registry permitted.
- `image.registry.custom_ca`: Custom TLS root certificate authority configured.
- `image.registry.signature_policy`: Cryptographic image signature verification enabled.
- `image.registry.offline`: Air-gapped / offline image store operation.
- `image.pause`: Pause / infra image available locally.
