# Security and support contract

## Current availability

The current CLI provides help, version information and a startup banner. Host evaluation and JSON report commands are planned in [M1.2](https://github.com/EpicBlackWolfZ/capagent/milestone/24). The existing platform/probe packages are infrastructure, and their [M1.1 hardening gate](https://github.com/EpicBlackWolfZ/capagent/issues/49) remains open. Their presence is not a claim of production-ready host scanning.

Execution targets Linux 5.6+ with working `openat2` confinement on amd64 and arm64. Syscall availability and security policy matter in addition to kernel version. `NewScopedOSReader` reports unsupported syscall availability or the relevant policy error when confinement cannot be established. There is no insecure pathname fallback. Older distribution fixtures, including historical RHEL 8 data, test parsers; they do not establish support for execution on those kernels.

## Default observation and explicit active verification

Default evaluation is intended to observe host and runtime state without creating containers, pulling images, changing namespaces, writing configuration or modifying networking. Native filesystem/kernel APIs are preferred. Bounded read-only system metadata interrogation may be used through an explicit execution policy when needed.

A command called `info` is not automatically passive. An adapter must verify that its chosen interrogation does not initialize storage or otherwise mutate host state. Network reachability, registry authentication, writable storage tests and disposable container execution belong to the separate, explicitly enabled active track. `--active` is not currently implemented; future active dispatch must be opt-in and isolated from default evaluation.

Passive inspection may establish configuration or prerequisites without proving end-to-end usability. Confidence and evidence must reflect that distinction.

## Filesystem authority

Host files, `/proc`, `/sys`, `/etc`, user configuration and runtime paths are inputs whose readability and contents can change during evaluation. Root identity and path scope must be explicit. Untrusted subpaths use `ScopedReader` with relative-path validation and kernel-backed `RESOLVE_IN_ROOT | RESOLVE_NO_MAGICLINKS` opens. Subsequent I/O uses the acquired descriptor; resolving a path string and reopening it is not a substitute for confinement.

The OS scoped reader interprets absolute symlink targets relative to its configured root. `Readlink` inspects a link's stored target; returning that target does not authorize following it. `/proc/self` resolves through its textual PID target, unlike the special descriptor/executable magic links. Embedded NUL bytes are rejected, not silently truncated into a different Go pathname.

Containment limits path resolution; it is not a general sandbox or a guarantee that reading every contained file is harmless. File kinds, ownership, privilege bits, resource limits and mount policy need separate treatment. Follow-up work remains open for adapter path semantics, OS/memory conformance, faithful metadata, close-on-exec and bounded file/directory reads: [#30](https://github.com/EpicBlackWolfZ/capagent/issues/30), [#33](https://github.com/EpicBlackWolfZ/capagent/issues/33), [#56](https://github.com/EpicBlackWolfZ/capagent/issues/56), [#57](https://github.com/EpicBlackWolfZ/capagent/issues/57), [#58](https://github.com/EpicBlackWolfZ/capagent/issues/58).

## Executables, environment and target identity

Runtime discovery and execution must identify the executable, arguments, allowed environment, working directory, target credentials, endpoint and namespace. Setting a child's environment alone does not control executable lookup in the parent. Ambient `PATH`, proxy/config variables and remote endpoint settings must not silently redirect a supposedly local evaluation.

The current runner passes an argument vector to `os/exec`; it does not implicitly parse a shell command. It still inherits process context and lacks the explicit execution policy planned in [#36](https://github.com/EpicBlackWolfZ/capagent/issues/36). That policy does not itself implement target-user credential switching. Target identities and supplementary groups are handled through the context work, without changing global credentials inside concurrent probes.

Root being able to read a user file or connect to a socket does not establish the target user's access. Evidence for different users, namespaces or runtime endpoints must stay separate. Runtime/configuration output and diagnostics may contain sensitive information; redaction and fixture sanitization are prerequisites for adapter delivery.

## Resource ownership and cancellation

The runner currently caps retained stdout and stderr at 1 MiB each and applies a default 30-second execution timer. It uses a separate process group and signals that group on cancellation. This does not yet guarantee a bounded return when a detached descendant retains an output pipe; bounded drain and cleanup semantics are tracked in [#37](https://github.com/EpicBlackWolfZ/capagent/issues/37).

Filesystem calls currently lack the resource/cancellation contract required by [#57](https://github.com/EpicBlackWolfZ/capagent/issues/57). Do not assume a Go context can forcibly interrupt arbitrary blocking kernel I/O. Limits must be defined for supported filesystems, file types, bytes, entries and parser inputs.

`Orchestrator.Run` receives a caller-owned environment and waits for cooperative probes. It does not construct or close that environment. A scoped reader must not be closed concurrently with reads. The planned application layer owns production construction and closes its resources after workers finish. Process-group signaling cannot promise termination of every deliberately escaped descendant, and cleanup cannot be guaranteed after power loss or an uncatchable process termination. Future active probes must identify owned resources, bound cleanup attempts and report incomplete cleanup.

## Failure and evidence semantics

| Outcome | Meaning |
|---|---|
| Unknown capability | The available evidence cannot reliably establish its operational state. |
| Unavailable capability | A required service/dependency is unavailable; a positive requirement remains indeterminate. |
| Failed probe | A non-cancellation error prevented successful completion. |
| Skipped probe | A prerequisite failed, was cancelled or was skipped. |
| Cancelled probe | The probe was directly interrupted by the evaluation context. |
| Incomplete observation | Some measurements exist, but missing data cannot be treated as a complete negative result. |

Operational states and requirement truth remain distinct: supported satisfies a positive predicate; unsupported or misconfigured does not; unknown or unavailable is indeterminate. Consumer policy decides how to handle indeterminate results explicitly.

The current parser and scheduler require fixes to preserve completeness, failed-resolution errors and partial observations: [#42](https://github.com/EpicBlackWolfZ/capagent/issues/42), [#43](https://github.com/EpicBlackWolfZ/capagent/issues/43). These fixes are part of the foundation gate. Live evidence outranks runtime reports, configuration, historical knowledge and heuristics only when claims concern compatible evaluation scopes. Equal-ranked conflicts and stale evidence must remain visible.

## Build and artifact trust

Go payloads are built with `CGO_ENABLED=0` and checked for dynamic linker dependencies. The packaged executable also includes a microfat launcher, so payload checks alone do not verify the complete release artifact.

Release inputs need independently pinned digest/signature trust, verified versioned caches and launcher provenance. Workflow actions and tool versions need controlled updates, minimal job permissions and isolated publishing credentials. These controls are being completed in [#31](https://github.com/EpicBlackWolfZ/capagent/issues/31) and [#38](https://github.com/EpicBlackWolfZ/capagent/issues/38). Current tooling does not yet establish all these guarantees.

The hardening gate combines race tests, full lint, architectural contracts, vulnerability and secret scanning, resource regressions, bounded fuzz/fault tests and packaged-artifact verification. A clean dependency scan is not a source audit or proof that downloaded launcher binaries are trustworthy. Architecture checks enforce declared imports and recognizable host-I/O patterns; they are not a security sandbox for arbitrary code.

## Documentation and working material

`docs/` contains durable documentation for readers. Internal audits, implementation plans, raw evidence, benchmark runs and reconciliation logs live in the git-ignored `.work/` directory or CI artifacts. GitHub issues retain actionable public acceptance criteria so implementation does not depend on access to a developer's local notes.
