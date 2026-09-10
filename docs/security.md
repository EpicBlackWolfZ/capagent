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

Procfs/sysfs adapters derive their root from the scoped reader. They reject
invalid original subpaths before I/O and forward valid paths without cleaning
away symlink or trailing-slash semantics. `.` is the explicit root alias; empty
strings and `/` are rejected. `ReadSelf` prefixes a validated argument with
`self/`; its security boundary remains the proc root, not a distinct self subtree.

Directory reads return sorted, eagerly captured metadata that remains usable
after the reader closes. A disappearing child (`ENOENT`) may be skipped; other
metadata failures return an error and no entries. These reads are not atomic
filesystem snapshots. The memory fixture reader traverses intermediate links,
but deliberately retains stricter containment and backing-tree absolute targets
rather than emulating every `RESOLVE_IN_ROOT` behavior. It does not emulate Linux
magic links.

File kinds, special permission bits, and ownership are measured separately from
capability decisions. FIFO/socket/device metadata does not look like a regular
file. Ownership is returned as an immutable value; unknown fixture ownership is
not UID/GID zero. File-capability inspection returns bounded raw
`security.capability` bytes with explicit absence/error distinctions, and never
executes a privilege helper. The raw value does not establish effective privileges.

File reads preflight the target with a confined `O_PATH` descriptor, reject
observed special types, then perform a second confined nonblocking read-only open
and check type and identity before reading. A replacement is rejected before its
data is read. This check is not an atomic regular-only open: a hostile device
replacement can still enter a device open handler. Containment is not a general
sandbox or proof that every contained file is harmless. Unsupported hostile
filesystem/device behavior must not be advertised as safe passive observation.

## Executables, environment and target identity

Runtime discovery and execution must identify the executable, arguments, allowed environment, working directory, target credentials, endpoint and namespace. Setting a child's environment alone does not control executable lookup in the parent. Ambient `PATH`, proxy/config variables and remote endpoint settings must not silently redirect a supposedly local evaluation.

The current runner passes an argument vector to `os/exec`; it does not implicitly parse a shell command. It still inherits process context and lacks the explicit execution policy planned in [#36](https://github.com/EpicBlackWolfZ/capagent/issues/36). That policy does not itself implement target-user credential switching. Target identities and supplementary groups are handled through the context work, without changing global credentials inside concurrent probes.

Root being able to read a user file or connect to a socket does not establish the target user's access. Evidence for different users, namespaces or runtime endpoints must stay separate. Runtime/configuration output and diagnostics may contain sensitive information; redaction and fixture sanitization are prerequisites for adapter delivery.

## Resource ownership and cancellation

The runner caps retained stdout and stderr at 1 MiB each and applies a default 30-second execution timer. Each buffer starts with 4 KiB capacity and grows within its cap. Temporary growth allocations and returned output copies are additional memory; these limits are not a total process-memory budget. Probe concurrency defaults to `min(runtime.NumCPU(), 8)`, with a minimum of one. Explicit positive `WithMaxConcurrency` values can exceed that default; zero and negative overrides are rejected.

The runner signals its separate process group on cancellation and reaps its direct child. A 250 ms pipe-drain budget starts on cancellation or observed direct-child exit, whichever occurs first. Expiry closes lingering runner pipes, including those held by descendants that escape into another session. This budget is subject to kernel and scheduling delays, rather than a hard real-time guarantee. Closing pipes does not prove escaped descendants have terminated; successful completion does not trigger an extra group signal.

Retained output remains available on errors. A successful child exit with expired drain returns `exec.ErrWaitDelay`; a nonzero exit retains its exit error and code. Observable caller cancellation takes precedence over internal timeout, which takes precedence over execution errors. `TimedOut` identifies only the selected internal timeout. Byte-cap truncation flags mean actual bytes were discarded beyond the cap; they do not certify completeness after a timeout or drain error. ExitCode zero alone is not success: startup failures also retain the zero-value code and return an error.

Scoped root and operation descriptors are opened atomically with `O_CLOEXEC`, including when the kernel allocates FD 0. They remain usable by their parent owner and cannot survive exec as implicitly inherited handles. Explicitly duplicating or transferring a descriptor is outside this guarantee.

Filesystem reads retain at most 8 MiB per file by default, inspect one extra byte
to detect overflow, and limit directory enumeration to 8,192 encountered names
and 1 MiB of aggregate name bytes. Context cancellation is checked before and
between I/O calls; interrupted-call retries are finite. Limits are immutable,
validated constructor options. Directory limits/cancellation return a sorted
incomplete subset; ordinary metadata errors other than disappearing-entry ENOENT
still discard the enumeration. Scratch buffers, allocation growth, parsed data,
and concurrency are additional memory, not part of a total RSS guarantee.

The supported I/O contract covers cooperative operation on responsive host
filesystems and the intended procfs/sysfs metadata files. `O_NONBLOCK` does not
turn regular-file I/O into a cancellable kernel call. A context cannot forcibly
interrupt an arbitrary blocked open, stat, read, or xattr call. Stalled remote or
userspace filesystems and hostile device drivers are outside this guarantee.
Readers are never closed concurrently to simulate cancellation, and no abandoned
background reader goroutine is used as a timeout mechanism.

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

Parsers retain complete measured records alongside typed diagnostics. Malformed
records and oversized lines do not become successful empty results. The procfs
defaults are 8 MiB input, 8,192 accepted records, and 32 retained diagnostics;
line limits are 64 KiB, or 1 MiB for mountinfo. Additional diagnostics are counted
without retaining raw input. An unterminated trailing fragment from an incomplete
read is discarded, while clean EOF may terminate a valid final record.
`errors.Is` preserves errno/context identity and incomplete/limit categories.
These are transport semantics; capability inference remains in the evaluation
layer. Invalid SELinux text is an error, never successful permissive state.

Live evidence outranks runtime reports, configuration, historical knowledge and heuristics only when claims concern compatible evaluation scopes. Equal-ranked conflicts and stale evidence must remain visible.

## Build and artifact trust

Go payloads are built with `CGO_ENABLED=0` and checked for dynamic linker dependencies. The packaged executable also includes a microfat launcher, so payload checks alone do not verify the complete release artifact.

Release inputs need independently pinned digest/signature trust, verified versioned caches and launcher provenance. Workflow actions and tool versions need controlled updates, minimal job permissions and isolated publishing credentials. These controls are being completed in [#31](https://github.com/EpicBlackWolfZ/capagent/issues/31) and [#38](https://github.com/EpicBlackWolfZ/capagent/issues/38). Current tooling does not yet establish all these guarantees.

The hardening gate combines race tests, full lint, architectural contracts, vulnerability and secret scanning, resource regressions, bounded fuzz/fault tests and packaged-artifact verification. A clean dependency scan is not a source audit or proof that downloaded launcher binaries are trustworthy. Architecture checks enforce declared imports and recognizable host-I/O patterns; they are not a security sandbox for arbitrary code.

## Documentation and working material

`docs/` contains durable documentation for readers. Internal audits, implementation plans, raw evidence, benchmark runs and reconciliation logs live in the git-ignored `.work/` directory or CI artifacts. GitHub issues retain actionable public acceptance criteria so implementation does not depend on access to a developer's local notes.
