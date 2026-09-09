# Container Capability Engine Roadmap

This document outlines the complete architectural roadmap, milestone sequence, issue breakdown, exit criteria, and canonical capability catalog for `capagent`.

---

## 1. Milestone Progression Matrix

```text
M0   Architecture & Contract
 │
 ▼
M1   Probe Core
 │
 ▼
M2   Host Facts
 │
 ▼
M3   Execution Context
 │
 ▼
M4   Runtime Discovery
 │
 ▼
M5   Podman Core
 │
 ▼
M6   Configuration Discovery
 │
 ▼
M7   Configuration Capabilities
 │
 ▼
M8   Capability Engine
 │
 ▼
M9   Requirement Engine
 │
 ▼
M10  Ansible Integration
 │
 ▼
M11  Diagnostics
 │
 ├──────────────────────┐
 ▼                      ▼
M12  Docker         M13 containerd / CRI
 │                      │
 └──────────┬───────────┘
            ▼
M14  Historical Knowledge
 │
 ▼
M15  Compatibility Fixtures
 │
 ▼
M16  Real Compatibility Matrix
 │
 ▼
M17  Active Validation
 │
 ▼
M18  Production Hardening
 │
 ▼
M19  API Stabilization
 │
 ▼
M20  Documentation & DX
 │
 ▼
M21  v1.0 Release
```

---

## 2. Detailed Milestone Specifications

### Milestone 0 — Architecture, Semantics & Contract
**Objective**: Freeze the conceptual architecture, domain types, state definitions, requirement logic, and JSON schema contract before implementing runtime code.

- **Issue 0.1**: Define project architecture & package boundaries.
- **Issue 0.2**: Define domain terminology (`Fact`, `Observation`, `Evidence`, `Capability`, `Requirement`, `EvaluationContext`).
- **Issue 0.3**: Define canonical capability namespace hierarchy (`host.*`, `runtime.*`, `container.*`, `image.*`, `network.*`, `storage.*`, `systemd.*`).
- **Issue 0.4**: Define capability operational states (`supported`, `unsupported`, `misconfigured`, `unavailable`, `unknown`).
- **Issue 0.5**: Define capability confidence levels (`verified`, `derived`, `heuristic`, `unknown`).
- **Issue 0.6**: Define requirement evaluation states (`SATISFIED`, `UNSATISFIED`, `INDETERMINATE`).
- **Issue 0.7**: Define requirement truth tables for `AND`, `OR`, and predicate `NOT`.
- **Issue 0.8**: Define evidence precedence hierarchy (`live > runtime > config > knowledge > heuristics`).
- **Issue 0.9**: Define `EvaluationContext` (UID, GID, target user, XDG directory, user systemd, namespaces).
- **Issue 0.10**: Define passive vs active security boundary and `--active` gate.
- **Issue 0.11**: Define initial JSON Schema v1 skeleton.

**Exit Criteria**:
- [x] Domain model documented and agreed upon.
- [x] Capability states and confidence levels implemented in code.
- [x] Requirement truth tables covered by unit tests.
- [x] Execution context structure defined.
- [x] Schema v1 skeleton validates sample outputs.

---

### Milestone 1 — Portable Probe Core
**Objective**: Build the executable framework and OS abstraction layer with zero third-party runtime dependencies.

- **Issue 1.1**: Initialize Go 1.27.1 project with `go.mod`.
- **Issue 1.2**: Build CLI skeleton (`--help`, `--version`, `--json`, `--pretty`, `--debug`).
- **Issue 1.3**: Implement `PlatformReader` filesystem abstraction (ReadFile, Stat, ReadDir, Readlink) with in-memory test doubles.
- **Issue 1.4**: Implement `ProcfsReader` for deterministic `/proc` access.
- **Issue 1.5**: Implement `SysfsReader` for deterministic `/sys` access.
- **Issue 1.6**: Implement Linux syscall abstraction wrappers (`statfs`, `uname`, `prctl`).
- **Issue 1.7**: Implement `CommandRunner` (`os/exec` production runner and `FakeCommandRunner` mock).
- **Issue 1.8**: Implement command execution limits (timeouts, context cancellation, stdout/stderr truncation, exit code capture).
- **Issue 1.9**: Implement `Probe` interface (`ID()`, `Run(context.Context)`).
- **Issue 1.10**: Implement probe registry and orchestration (dependency ordering, concurrency, isolation).
- **Issue 1.11**: Implement deterministic JSON encoder.
- **Issue 1.12**: Implement core test utilities (fake FS, fake procfs/sysfs, golden JSON fixtures).

**Exit Criteria**:
- [x] Static binary compiles with `CGO_ENABLED=0`.
- [x] Probe framework executes concurrently and safely handles timeouts.
- [x] Host access and external commands are 100% mockable in unit tests.
- [x] Output serialization is bit-for-bit deterministic.

---

### Milestone 2 — Host Fact Engine
**Objective**: Determine the actual Linux environment using direct filesystem and kernel observations.

- **Issue 2.1**: Parse `/etc/os-release` (RHEL, Fedora, CentOS, Rocky, Alma, Debian, Ubuntu).
- **Issue 2.2**: Collect kernel release, version, and CPU architecture via `uname`.
- **Issue 2.3**: Detect systemd state (installed vs running vs accessible).
- **Issue 2.4**: Parse systemd version via `systemctl --version` with fallback handling.
- **Issue 2.5**: Inspect systemd filesystem directories (system and user generator paths).
- **Issue 2.6**: Detect cgroup mode via `statfs` on `/sys/fs/cgroup` (`v1`, `v2`, `mixed`, `unavailable`).
- **Issue 2.7**: Inspect cgroup topology via `/proc/self/cgroup`.
- **Issue 2.8**: Discover available cgroup controllers and delegation settings.
- **Issue 2.9**: Inventory Linux namespaces via `/proc/self/ns/` (user, pid, net, mnt, ipc, uts, cgroup).
- **Issue 2.10**: Detect SELinux state (`disabled`, `permissive`, `enforcing`) via direct `/sys/fs/selinux` checks.
- **Issue 2.11**: Detect AppArmor status via `/sys/kernel/security/apparmor`.
- **Issue 2.12**: Detect seccomp kernel capability vs current process filter state.
- **Issue 2.13**: Inspect IPv4 networking socket availability.
- **Issue 2.14**: Inspect IPv6 networking socket availability.
- **Issue 2.15**: Discover supported kernel filesystems via `/proc/filesystems` (`overlay`, `fuse`).
- **Issue 2.16**: Inspect host DNS configuration via `/etc/resolv.conf`.

**Exit Criteria**:
- [ ] Validated against fixture data for RHEL 8/9/10, Fedora, Debian, and minimal Alpine.
- [ ] Correctly distinguishes cgroup v1 from cgroup v2.
- [ ] Operates reliably in containerized and restricted `/proc` environments.

---

### Milestone 3 — Identity & Rootless Context
**Objective**: Enable context-aware evaluation based on the target identity rather than assuming root access.

- **Issue 3.1**: Collect current identity (UID, GID, username, supplementary groups, `$HOME`).
- **Issue 3.2**: Implement target context flags (`--context=current`, `--context=user:<name>`, `--context=uid:<id>`).
- **Issue 3.3**: Parse and validate subordinate UID ranges in `/etc/subuid`.
- **Issue 3.4**: Parse and validate subordinate GID ranges in `/etc/subgid`.
- **Issue 3.5**: Detect presence and setuid/file-capability usability of `newuidmap` and `newgidmap`.
- **Issue 3.6**: Inspect unprivileged user namespace sysctl policy (`unprivileged_userns_clone`).
- **Issue 3.7**: Verify `XDG_RUNTIME_DIR` presence, ownership, and accessibility.
- **Issue 3.8**: Detect user systemd manager accessibility via user D-Bus / socket.
- **Issue 3.9**: Detect user lingering state (`loginctl show-user`).
- **Issue 3.10**: Produce composite rootless prerequisite facts.

**Exit Criteria**:
- [ ] Distinguishes between "host supports rootless" and "this target user can execute rootless containers".

---

### Milestone 4 — Runtime Discovery
**Objective**: Discover container runtimes in an execution-context-aware manner across Podman, Docker, containerd, CRI-O, and nerdctl.

- **Issue 4.1**: Define generic `Runtime` interface (`ID()`, `Discover()`, `Observe()`).
- **Issue 4.2**: Implement runtime registry.
- **Issue 4.3**: Implement binary discovery in `$PATH` and known system paths.
- **Issue 4.4**: Implement socket discovery (`/run/podman/podman.sock`, `/var/run/docker.sock`, `/run/containerd/containerd.sock`).
- **Issue 4.5**: Model runtime availability states (not installed, runnable, daemon active, daemon unavailable, permission denied).
- **Issue 4.6**: Implement resilient semantic version parser handling vendor suffixes.
- **Issue 4.7**: Build runtime discovery test fixtures.

**Exit Criteria**:
- [ ] Accurately discovers and reports status for multiple co-existing or absent runtimes.

---

### Milestone 5 — Podman Core Runtime Adapter
**Objective**: Implement deep, non-invasive observation of Podman installations.

- **Issue 5.1**: Podman binary discovery.
- **Issue 5.2**: Podman version interrogation (`podman --version`).
- **Issue 5.3**: Podman version parser supporting historical and downstream vendor strings.
- **Issue 5.4**: Parse `podman info --format json` (cgroups, network backend, storage driver, graph root).
- **Issue 5.5**: Rootful vs rootless execution validation.
- **Issue 5.6**: Podman storage observation (graphroot, runroot, additional image stores).
- **Issue 5.7**: Podman network backend discovery (CNI vs Netavark, Aardvark-DNS, Pasta, slirp4netns).
- **Issue 5.8**: Podman OCI runtime discovery (`runc`, `crun`, `youki`).
- **Issue 5.9**: Podman Quadlet feature and generator probe.
- **Issue 5.10**: Podman `kube apply` capability probe.
- **Issue 5.11**: Podman `generate systemd` legacy probe.
- **Issue 5.12**: Podman infra/pause container feature probe.
- **Issue 5.13**: Podman system service and socket status.
- **Issue 5.14**: Build Podman 3.x, 4.x, 5.x, and 6.x fixture corpus.

**Exit Criteria**:
- [ ] Completely models Podman configuration and feature set across rootful and rootless contexts without running containers.

---

### Milestone 6 — Configuration Discovery Engine
**Objective**: Discover and model configuration files and precedence across system, vendor, and user scopes.

- **Issue 6.1**: Configuration source abstraction (paths, permissions, ownership, drop-ins).
- **Issue 6.2**: Configuration precedence model (vendor defaults < system defaults < drop-ins < user config < environment).
- **Issue 6.3**: Podman `containers.conf` discovery.
- **Issue 6.4**: Podman `storage.conf` discovery.
- **Issue 6.5**: Podman `registries.conf` discovery and syntax version detection.
- **Issue 6.6**: Podman `policy.json` signature policy discovery.
- **Issue 6.7**: Podman `registries.d/` directory discovery.
- **Issue 6.8**: Configuration file permission and readability checks.
- **Issue 6.9**: Separate rootless user configuration discovery (`~/.config/containers/`).

**Exit Criteria**:
- [ ] Identifies effective configuration paths and permission barriers for any given target user.

---

### Milestone 7 — Configuration Capabilities
**Objective**: Translate configuration observations into operational capability claims.

- **Issue 7.1**: Registry capability model (custom, private, mirrors, insecure, custom CA).
- **Issue 7.2**: Implement canonical registry capabilities (`image.registry.*`).
- **Issue 7.3**: Storage capability model (`container.storage.overlay`, `fuse-overlayfs`, `vfs`).
- **Issue 7.4**: Storage driver configuration validation.
- **Issue 7.5**: Container DNS capability model (`container.network.dns`, `container.network.custom_dns`).
- **Issue 7.6**: Rootless networking capabilities (`container.network.pasta`, `slirp4netns`, `port_forwarding`).
- **Issue 7.7**: Infra/pause image capability model.
- **Issue 7.8**: Configuration capability test matrix covering all 5 operational states.

**Exit Criteria**:
- [ ] Accurately determines if features are practically usable or blocked by configuration.

---

### Milestone 8 — Canonical Capability Engine
**Objective**: Introduce the central capability registry, dependency DAG, and evidence evaluation engine.

- **Issue 8.1**: Implement `CapabilityRegistry` (IDs, descriptions, dependencies, evaluators).
- **Issue 8.2**: Construct and validate capability dependency DAG (detect cycles, missing dependencies).
- **Issue 8.3**: Implement evidence graph linking capabilities to underlying observations.
- **Issue 8.4**: Implement capability evaluator evaluating operational states and confidence.
- **Issue 8.5**: Implement evidence precedence resolution (`live > runtime > config > knowledge > heuristic`).
- **Issue 8.6**: Implement canonical `container.lifecycle.systemd_native` capability.
- **Issue 8.7**: Implement canonical `container.lifecycle.systemd_generated` capability.
- **Issue 8.8**: Implement canonical networking capabilities (`container.network.*`).
- **Issue 8.9**: Implement canonical storage capabilities (`container.storage.*`).
- **Issue 8.10**: Implement canonical image capabilities (`image.*`).
- **Issue 8.11**: Guarantee unknown state propagation (probe errors never become false).
- **Issue 8.12**: Implement capability diagnostic generation (reasons, evidence, dependency trace).

**Exit Criteria**:
- [ ] Engine evaluates high-level canonical capabilities decoupled from runtime specifics.

---

### Milestone 9 — Requirement Engine
**Objective**: Enable workloads to declare capability requirements and evaluate them deterministically.

- **Issue 9.1**: Define declarative requirement model (supporting `all`, `any`, `none`, expressions).
- **Issue 9.2**: Implement YAML/JSON requirement parser.
- **Issue 9.3**: Implement 3-valued evaluation engine (`SATISFIED`, `UNSATISFIED`, `INDETERMINATE`).
- **Issue 9.4**: Implement `AND` truth table evaluation.
- **Issue 9.5**: Implement `OR` truth table evaluation.
- **Issue 9.6**: Implement predicate `NOT` negation.
- **Issue 9.7**: Suite of composite requirement tests.
- **Issue 9.8**: Implement requirement explanation renderer (identifying blocking failure causes).

**Exit Criteria**:
- [ ] Deployment requirements evaluate with formal 3-valued semantics and actionable explanations.

---

### Milestone 10 — Early Ansible Integration
**Objective**: Validate real-world consumer workflows in Ansible early in the development lifecycle.

- **Issue 10.1**: Basic Ansible playbook integration example (`command: capagent --json`).
- **Issue 10.2**: Ansible capability gating examples (`when: host_capabilities...`).
- **Issue 10.3**: Ansible requirement document evaluation example.
- **Issue 10.4**: Quadlet-to-generated-systemd fallback workflow playbook.
- **Issue 10.5**: Document fail-closed policy guidelines for Ansible automation.
- **Issue 10.6**: Ansible user experience and JSON structure review.
- **Issue 10.7**: Build reusable `capagent_probe` Ansible role.

**Exit Criteria**:
- [ ] Validated against real Ansible playbooks on target hosts.

---

### Milestone 11 — Diagnostics / Doctor
**Objective**: Provide human-friendly CLI inspection and troubleshooting tooling.

- **Issue 11.1**: Implement `capagent doctor` overview.
- **Issue 11.2**: Implement `capagent runtime <id>` command.
- **Issue 11.3**: Implement `capagent config` command.
- **Issue 11.4**: Implement `capagent explain <capability-id>` command.
- **Issue 11.5**: Implement terminal evidence-tree renderer.
- **Issue 11.6**: Expose diagnostic trees in JSON schema.

**Exit Criteria**:
- [ ] Engineers can diagnose deployment blockers interactively from the command line.

---

### Milestone 12 — Docker Runtime Adapter
**Objective**: Implement the Docker adapter to prove runtime decoupling.

- **Issue 12.1**: Docker binary discovery.
- **Issue 12.2**: Docker version parsing.
- **Issue 12.3**: Docker daemon socket availability and ping.
- **Issue 12.4**: Parse `docker info` (storage driver, cgroup driver, rootless, runtimes).
- **Issue 12.5**: Docker DNS configuration discovery.
- **Issue 12.6**: Docker registry configuration discovery (`daemon.json`).
- **Issue 12.7**: Docker alternative runtime discovery.
- **Issue 12.8**: Map Docker observations to canonical capabilities.
- **Issue 12.9**: Docker fixture corpus (17.x, 20.x, 24.x, 27.x).
- **Issue 12.10**: Cross-runtime capability equivalence tests.

**Exit Criteria**:
- [ ] Canonical capabilities evaluate identically whether satisfied by Podman or Docker.

---

### Milestone 13 — containerd / CRI / CRI-O / nerdctl
**Objective**: Extend runtime abstractions to daemonless CRI environments and Kubernetes node agents.

- **Issue 13.1**: containerd binary discovery.
- **Issue 13.2**: containerd version parsing.
- **Issue 13.3**: containerd socket discovery.
- **Issue 13.4**: containerd gRPC socket connectivity check.
- **Issue 13.5**: Parse containerd `config.toml`.
- **Issue 13.6**: containerd CRI plugin status discovery.
- **Issue 13.7**: CRI API capability discovery.
- **Issue 13.8**: Sandbox/pause image configuration detection.
- **Issue 13.9**: containerd registry mirrors and configs.
- **Issue 13.10**: OCI runtime discovery (`runc`, `kata`).
- **Issue 13.11**: CRI-O adapter implementation.
- **Issue 13.12**: nerdctl adapter implementation.
- **Issue 13.13**: Cross-runtime CRI capability test suite.

**Exit Criteria**:
- [ ] Baseline containerd and CRI environments evaluate canonical capabilities.

---

### Milestone 14 — Historical Knowledge Base
**Objective**: Incorporate declarative version intelligence without corrupting live evidence precedence.

- **Issue 14.1**: Define declarative `KnowledgeRule` model.
- **Issue 14.2**: Structured knowledge definitions in `internal/knowledge/`.
- **Issue 14.3**: Provenance tracking (official docs, changelogs, CVEs).
- **Issue 14.4**: Podman historical feature rules (Quadlet, Netavark, cgroup v2, Pasta).
- **Issue 14.5**: Docker historical feature rules.
- **Issue 14.6**: containerd historical feature rules.
- **Issue 14.7**: Netavark/Aardvark feature evolution rules.
- **Issue 14.8**: Knowledge conflict tests (live evidence strictly overrides knowledge).
- **Issue 14.9**: Transparent diagnostic reporting of historical inferences (`confidence: derived`).

**Exit Criteria**:
- [ ] Knowledge enhances missing details while live evidence always wins in conflicts.

---

### Milestone 15 — Compatibility Fixture Framework
**Objective**: Comprehensive simulation framework covering dozens of OS and runtime permutations without needing real machines.

- **Issue 15.1**: Standardized fixture layout specification.
- **Issue 15.2**: Fixture-backed `FakeCommandRunner`.
- **Issue 15.3**: Fixture-backed `FakePlatformReader` (procfs and sysfs).
- **Issue 15.4**: Podman version fixture corpus (3.x, 4.x, 5.x, 6.x).
- **Issue 15.5**: Docker version fixture corpus.
- **Issue 15.6**: containerd version fixture corpus.
- **Issue 15.7**: Malformed and edge-case configuration fixtures.
- **Issue 15.8**: Broken-environment fixtures (missing Quadlet, cgroup v1 mismatch, inaccessible socket).
- **Issue 15.9**: Golden file test harness verifying JSON reports.

**Exit Criteria**:
- [ ] Full regression test suite runs in seconds during standard CI.

---

### Milestone 16 — Real OS / Runtime Compatibility Matrix
**Objective**: Validate fixture assumptions against real operating systems and runtime packages.

- **Issue 16.1**: Automated test harness for real host environments.
- **Issue 16.2**: Validate on RHEL 8 environments.
- **Issue 16.3**: Validate on RHEL 9 environments.
- **Issue 16.4**: Validate on RHEL 10 environments.
- **Issue 16.5**: Validate on Fedora environments.
- **Issue 16.6**: Validate across Podman package versions.
- **Issue 16.7**: Validate rootless configurations under real users.
- **Issue 16.8**: Validate cgroup v1 vs v2 hosts.
- **Issue 16.9**: Validate CNI vs Netavark networking.
- **Issue 16.10**: Validate SELinux enforcing vs permissive hosts.
- **Issue 16.11**: Validate intentionally misconfigured test hosts.
- **Issue 16.12**: Automated drift detection between fixture expectations and real-world behavior.

**Exit Criteria**:
- [ ] Real-world matrix confirms fidelity of simulation fixtures.

---

### Milestone 17 — Active Validation
**Objective**: Safe, opt-in end-to-end operational validation.

- **Issue 17.1**: Active probe registry isolated from default passive pipeline.
- **Issue 17.2**: Explicit activation gate (`--active`).
- **Issue 17.3**: Disposable container lifecycle probe.
- **Issue 17.4**: Active container DNS resolution probe.
- **Issue 17.5**: Active registry authentication and reachability probe.
- **Issue 17.6**: Active storage driver write and mount probe.
- **Issue 17.7**: Active Quadlet dry-run generation probe.
- **Issue 17.8**: Strict cleanup guarantees (timeouts, deferred cleanup, signal trapping).

**Exit Criteria**:
- [ ] Active validation operates safely with guaranteed resource cleanup.

---

### Milestone 18 — Production Hardening
**Objective**: Fleet safety, resilience on stripped-down systems, and static binary verification.

- **Issue 18.1**: Performance benchmarking across host and runtime probes.
- **Issue 18.2**: Probe concurrency tuning while preserving deterministic output.
- **Issue 18.3**: Hard resource bounds (memory ceilings, command output truncation).
- **Issue 18.4**: Validation on minimal hosts lacking standard userland utilities.
- **Issue 18.5**: Validation running inside container environments.
- **Issue 18.6**: Validation on read-only root filesystems.
- **Issue 18.7**: Validation under restricted `/proc` and `/sys` mount flags.
- **Issue 18.8**: Security audit (argument injection defense, symlink traversal prevention).
- **Issue 18.9**: Static binary verification (`CGO_ENABLED=0`, zero libc dependencies).

**Exit Criteria**:
- [ ] Robust, secure, static binary ready for fleet deployment.

---

### Milestone 19 — API Stabilization
**Objective**: Freeze public schema, capability IDs, and backward compatibility contracts.

- **Issue 19.1**: Finalize and publish JSON Schema v1.
- **Issue 19.2**: Audit canonical capability ID stability.
- **Issue 19.3**: Freeze requirement evaluation semantics.
- **Issue 19.4**: Freeze evidence graph schema.
- **Issue 19.5**: Backward compatibility regression tests.
- **Issue 19.6**: Capability lifecycle status annotations (`stable`, `experimental`, `deprecated`).
- **Issue 19.7**: Support `--schema-version` negotiation.
- **Issue 19.8**: Automatic capability catalog documentation generator.

**Exit Criteria**:
- [ ] Machine-readable contract guaranteed backward compatible for v1.x series.

---

### Milestone 20 — Documentation & Developer Experience
**Objective**: Comprehensive contributor guides and operational playbooks.

- **Issue 20.1**: Complete Architecture Guide.
- **Issue 20.2**: Probe Authoring Guide.
- **Issue 20.3**: Runtime Adapter Authoring Guide.
- **Issue 20.4**: Capability Authoring Guide.
- **Issue 20.5**: Requirement Language Specification.
- **Issue 20.6**: Knowledge Base Authoring Guide.
- **Issue 20.7**: Ansible Integration Guide.
- **Issue 20.8**: Troubleshooting & FAQ Guide.

**Exit Criteria**:
- [ ] Full developer and operator documentation available.

---

### Milestone 21 — v1.0 Release
**Objective**: Stable general availability release of the core capability engine.

- **Scope**:
  - Stable: Podman, Docker, containerd baseline, Linux host fact engine, rootful/rootless context, configuration capabilities, requirement engine, diagnostics, Ansible role, JSON Schema v1.
  - Experimental: CRI-O, nerdctl, active validation probes.

**Exit Criteria**:
- [ ] All foundational and stable milestones closed.
- [ ] Complete CI pass on real matrix.
- [ ] Zero known critical issues.

---

## 3. Initial Canonical Capability Catalog

### 3.1 Host Capabilities
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

### 3.2 Execution Context Capabilities
- `context.root`: Process running as root.
- `context.rootless`: Process running unprivileged.
- `context.user`: Valid target user resolution.
- `context.user_namespaces`: Unprivileged user namespace creation permitted.
- `context.user_systemd`: Target user has active systemd instance.
- `context.user_runtime_dir`: Target user has accessible `XDG_RUNTIME_DIR`.
- `context.subuid`: Target user has allocated subordinate UID range.
- `context.subgid`: Target user has allocated subordinate GID range.

### 3.3 Lifecycle Capabilities
- `container.lifecycle.systemd_native`: Native systemd unit generation / Quadlet.
- `container.lifecycle.systemd_generated`: Generated unit deployment (`podman generate systemd`).
- `container.lifecycle.daemon_managed`: Traditional daemon restart policy management (Docker).
- `container.lifecycle.cri`: CRI pod lifecycle management.
- `container.lifecycle.kubernetes`: `podman kube apply` manifest execution.

### 3.4 Runtime Specific Capabilities
- `runtime.podman`: Podman installed and runnable.
- `runtime.podman.quadlet`: Podman Quadlet generator present and functional.
- `runtime.podman.kube_apply`: Podman `kube play`/`apply` support.
- `runtime.podman.generate_systemd`: Podman legacy systemd unit generation.
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

### 3.5 Storage Capabilities
- `container.storage.overlay`: Native kernel OverlayFS storage driver.
- `container.storage.fuse_overlayfs`: FUSE-overlayfs storage driver (unprivileged).
- `container.storage.vfs`: Fallback VFS storage driver.
- `container.storage.btrfs`: Btrfs native storage driver.
- `container.storage.zfs`: ZFS native storage driver.
- `container.storage.additional_store`: Read-only additional image stores configured.
- `container.storage.rootless`: Storage configured properly for unprivileged user.

### 3.6 Networking Capabilities
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

### 3.7 Image & Registry Capabilities
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
