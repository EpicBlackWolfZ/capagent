# Container Capability Engine Philosophy & Core Principles

> "A portable, evidence-driven compatibility engine that determines whether a Linux environment can satisfy a container deployment requirement, explains why, and provides a stable machine-readable contract for automation."

---

## 1. Project Vision & Origin

The initial problem that motivated `capagent` is the operational pain of managing heterogeneous Podman environments through automation (such as Ansible):

- Diverse Linux distributions (RHEL 8/9/10, Fedora, CentOS Stream, Debian, Ubuntu).
- Varying kernel capabilities and systemd versions.
- cgroup v1 vs cgroup v2 semantics and controller delegations.
- Rootful vs rootless execution differences.
- Evolving Podman major releases (3.x, 4.x, 5.x, 6.x).
- Networking stack transitions (CNI vs Netavark, Aardvark-DNS, Pasta vs slirp4netns).
- Storage driver differences (`overlay`, `fuse-overlayfs`, `vfs`, `btrfs`, `zfs`).
- Registry configuration subtleties (search registries, credentials, mirrors, insecure/custom-CA registries, signature policies).
- Lifecycle mechanisms: Quadlet vs `podman kube apply` vs generated systemd units (`podman generate systemd`).

Rather than remaining a static "host probe" or a collection of ad-hoc checks scattered through Ansible playbooks, **capagent** is designed as a **capability and requirement engine**.

The deployment system should not need to know *why* a capability exists:

```yaml
# Declarative, decoupled capability assertion
when:
  - host_capabilities.capabilities.container.lifecycle.systemd_native.state == "supported"
```

instead of fragile version and heuristic checks:

```yaml
# Fragile heuristic checks scattered across automation
when:
  - podman_version >= 4.4
  - cgroup_version == "v2"
  - quadlet_generator_exists
  - systemd_version >= 250
  - ...
```

---

## 2. Core Architectural Principles

### 2.1 TDD is Mandatory

Every capability follows a rigorous development lifecycle:

```text
Define contract
      ↓
Write failing test (RED)
      ↓
Implement minimum behavior (GREEN)
      ↓
Pass test
      ↓
Add negative & edge cases
      ↓
Add simulation fixtures
      ↓
Add integration test
      ↓
Document
      ↓
Freeze behavior
```

**A capability without executable tests is not a supported capability.**

### 2.2 Evidence Before Inference

The engine strictly separates five semantic levels:

```text
Facts ──▶ Observations ──▶ Evidence ──▶ Capabilities ──▶ Requirements
```

- **Version knowledge is advisory.**
- **Direct evidence is authoritative.**

#### Mandatory Precedence:

```text
direct live evidence
        >
runtime-reported effective state
        >
configuration evidence
        >
version knowledge
        >
heuristics
```

Version knowledge must **never** override contradictory direct evidence. For example:
- *Knowledge*: Podman 5.x normally supports Quadlet.
- *Live evidence*: The Quadlet generator binary is absent or filesystem permissions deny execution.
- *Result*: Quadlet is reported as **unsupported** or **misconfigured**, never as supported.

### 2.3 Unknown is Not False

The engine models five distinct operational states for capabilities:

| State | Definition | Example |
| :--- | :--- | :--- |
| `supported` | The environment and runtime fully provide and permit the capability. | cgroup v2, systemd 252, and Quadlet generator present. |
| `unsupported` | The runtime or platform fundamentally does not provide the capability. | Attempting rootless port forwarding without rootless helper utilities. |
| `misconfigured` | The capability is supported by software, but current config blocks it. | Registry TLS certificate is invalid or `registries.conf` syntax is malformed. |
| `unavailable` | The capability exists in software, but a dependency/service is down. | Runtime daemon is stopped or runtime socket permissions are inaccessible. |
| `unknown` | The engine was unable to determine the state reliably. | Access to `/proc` or `/sys` was denied or command execution timed out. |

These represent **operational realities**, not Boolean truth values. Automation consumers can apply explicit fail-closed or fail-open policies when `unknown` is encountered.

### 2.4 Capability Confidence is Separate

Every capability determination carries an explicit confidence level:

- **`verified`**: Directly proven by live probing or inspection of effective runtime state.
- **`derived`**: Inferred from authoritative configuration or system dependencies.
- **`heuristic`**: Inferred based on standard distribution conventions or historical patterns.
- **`unknown`**: Lacking verifiable evidence.

```json
{
  "id": "container.lifecycle.systemd_native",
  "state": "supported",
  "confidence": "verified"
}
```

### 2.5 Capability IDs Must Be Canonical

Capabilities represent high-level architectural requirements rather than runtime-specific flags:

- **Prefer**: `container.lifecycle.systemd_native`
- **Over**: `runtime.podman.quadlet`

This enables Podman Quadlet, Docker/systemd integration, or a future runtime to satisfy the identical conceptual requirement. Runtime-specific evidence paths remain preserved in the evidence graph.

### 2.6 Capability State & Requirement Logic Are Separate

Do not conflate capability states with requirement evaluation.

- **Capabilities** report: `supported`, `unsupported`, `misconfigured`, `unavailable`, `unknown`.
- **Requirements** evaluate to: `SATISFIED`, `UNSATISFIED`, `INDETERMINATE`.

A capability that is `supported` satisfies a positive requirement. A capability that is `unsupported` or `misconfigured` marks it `UNSATISFIED`. An inability to determine the state (`unknown` or `unavailable`) maps to `INDETERMINATE`.

### 2.7 Passive by Default

By default, the binary must be non-invasive:
- It does **not** create or destroy containers.
- It does **not** pull images.
- It does **not** modify configuration or networking.
- It does **not** restart services or mutate system state.

Active probing (disposable container runs, network handshakes) requires explicit opt-in via `--active`.

### 2.8 Execution Context is First-Class

Capabilities are rarely properties of the machine alone. They depend intimately on the target identity and context:

- UID / GID / Supplementary groups.
- Root vs rootless execution.
- Allocation in `/etc/subuid` and `/etc/subgid`.
- Accessibility of `XDG_RUNTIME_DIR`.
- Availability of a user systemd manager (`systemd --user`) and D-Bus session.
- Subordinate user namespace policies (`kernel.unprivileged_userns_clone`).
- Whether `capagent` is running inside an existing container.

All evaluations take an explicit `EvaluationContext`.

### 2.9 Minimal Dependencies

- Target: **Go 1.27.1**
- Build requirement: **`CGO_ENABLED=0`** (strictly static binaries).
- Standard library first.
- Limited low-level dependencies (e.g. `golang.org/x/sys/unix`) for direct Linux syscalls.
- No heavy third-party container runtime SDKs.

### 2.10 Host Probing Prefers Direct APIs

To guarantee reliability on minimal, stripped-down systems:
- Query `/proc`, `/sys`, `statfs`, `prctl`, `uname`, and filesystem paths directly.
- Avoid spawning external shells (`sh`, `bash`) or userland utilities (`grep`, `stat`, `awk`).
- Subprocess execution is reserved strictly for interrogating runtime binaries (`podman`, `docker`, `containerd`).

---

## 3. The 20 Final Engineering Principles

1. **Facts are observations.** Raw measurements are preserved immutably.
2. **Observations produce evidence.** Evidence contextualizes facts.
3. **Evidence produces capabilities.** Capabilities evaluate capability state.
4. **Requirements consume capabilities.** Workloads declare requirements against capabilities.
5. **Execution context matters.** Capabilities are evaluated per-context, not just per-host.
6. **Unknown is not false.** Indeterminate state is never silently collapsed into negative truth.
7. **Capability state is separate from requirement truth.** Operational state differs from Boolean logic.
8. **Direct evidence beats version assumptions.** What is observed on disk/kernel overrides release notes.
9. **Knowledge supplements evidence; it never overrides it.** Historical knowledge cannot contradict live state.
10. **Runtime implementations map into canonical capabilities.** Decouple workloads from runtime specifics.
11. **Passive probing is the default.** Zero side effects during routine scanning.
12. **Active validation is explicitly opt-in.** Mutation and ephemeral execution require `--active`.
13. **Host probing prefers native APIs.** Read `/proc` and `/sys` directly without shell utilities.
14. **Runtime probing may use bounded CLI/API interrogation.** Subprocess commands are subject to timeouts and buffer limits.
15. **Every capability starts with a failing test.** True TDD is non-negotiable.
16. **Every supported runtime gets fixtures and real integration tests.** Simulation must reflect reality.
17. **The public JSON contract evolves additively.** Backward compatibility is guaranteed.
18. **Ansible is validated early, not at the end.** Design for the consumer from milestone 10 onward.
19. **Minimal dependencies are a design constraint.** Static CGO-free Go binary.
20. **v1.0 promises a stable model, not universal runtime coverage.** Robust semantics over broad half-baked runtime adapters.

---

## 4. Definitions of Done

### 4.1 Capability Definition of Done

A capability is complete only when:
- [ ] Canonical ID defined in the hierarchy.
- [ ] Clear human description and operational rationale.
- [ ] Evidence dependencies declared.
- [ ] Evaluator implemented.
- [ ] Positive test passing (supported).
- [ ] Negative tests passing (unsupported, misconfigured).
- [ ] Unknown / error test passing.
- [ ] Static fixture added.
- [ ] Real integration test verified.
- [ ] JSON contract serialization tested.
- [ ] Diagnostic explanations generated.
- [ ] Documented in the capability catalog.

### 4.2 Runtime Adapter Definition of Done

A runtime is supported only when:
- [ ] Binary and daemon discovery implemented.
- [ ] Robust version parsing (including vendor suffixes).
- [ ] Availability state modeled (installed, running, accessible).
- [ ] Execution context awareness (root vs rootless).
- [ ] Runtime observations extracted.
- [ ] Configuration discovery implemented.
- [ ] Canonical capability mappings tested.
- [ ] Diagnostics integrated.
- [ ] Unit tests passing.
- [ ] Simulation fixture corpus established.
- [ ] Real host integration test executed.

### 4.3 Knowledge Base Definition of Done

Every historical version rule requires:
- [ ] Explicit version range.
- [ ] Concrete behavior being described.
- [ ] Verified source provenance (official docs, release notes, changelog).
- [ ] Explicit confidence assignment.
- [ ] Fixture representation.
- [ ] Executable unit test.
- [ ] Conflict-resolution test ensuring live evidence takes precedence.

---

## 5. Test Pyramid & Fixture Philosophy

```text
               ┌────────────────────────┐
               │ Level 4: Compatibility │  Real OS matrix (RHEL, Fedora, Debian)
               ├────────────────────────┤
               │ Level 3: Integration   │  Live Podman, Docker, containerd
               ├────────────────────────┤
               │ Level 2: Probe Contract│  Fake procfs, sysfs, fake command runner
               ├────────────────────────┤
               │ Level 1: Unit Tests    │  Parsers, models, truth tables, evaluators
               └────────────────────────┘
```

### Fixture Philosophy
- **Mock external variability, never the logic under test.**
- Real parsers must parse synthetic fixture outputs.
- Real evaluators must evaluate synthetic evidence graphs.
- Never mock the evaluator to return a pre-canned capability state.

---

## 6. Major Architectural Risks & Mitigations

| Risk | Consequence | Mitigation Strategy |
| :--- | :--- | :--- |
| **Runtime Explosion** | Spreading effort across too many runtimes leads to shallow, buggy adapters. | Target Podman, Docker, and containerd baseline for v1.0. Keep CRI-O and nerdctl experimental. |
| **Knowledge Base Explosion** | Re-implementing every runtime changelog into code creates an unmaintainable codebase. | Encode only high-impact behavioral divergences. Live direct evidence always takes precedence. |
| **Config Parser Explosion** | Reverse-engineering complex configuration inheritance trees is fragile. | Prefer runtime-reported effective state (e.g. `podman info --format json`). Parse raw files only when necessary. |
| **Rootless Complexity** | Assuming root can probe rootless accurately causes silent deployment failures. | Require explicit `EvaluationContext` with user, subuid, subgid, and runtime directory checks. |
| **Boolean Capability Traps** | Collapsing `unknown` or `misconfigured` to `false` creates silent security or operational hazards. | Enforce 5 capability operational states and 3 requirement evaluation outcomes. |
