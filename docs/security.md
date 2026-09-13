# Security and support contract

## Current availability

The CLI supports [passive current-user Podman discovery](podman-discovery.md) and [offline fixture evaluation](fixture-evaluation.md), including JSON reports and requirement verdicts. It reads the selected fixture through kernel-backed confinement and simulates every fixture command through the existing fake runner. Live `--runtime podman --active` permits the bounded local inspection described below. The M1.1 platform/probe hardening contracts and full verification gate continue to apply.

Execution targets Linux 5.6+ with working `openat2` confinement on amd64 and arm64. Syscall availability and security policy matter in addition to kernel version. `NewScopedOSReader` reports unsupported syscall availability or the relevant policy error when confinement cannot be established. There is no insecure pathname fallback. Older distribution fixtures, including historical RHEL 8 data, test parsers; they do not establish support for execution on those kernels.

## Default observation and explicit active verification

Default evaluation is intended to observe host and runtime state without creating containers, pulling images, changing namespaces, writing configuration or modifying networking. Native filesystem/kernel APIs are preferred. Bounded read-only system metadata interrogation may be used through an explicit execution policy when needed.

A command called `info` or `--version` is not automatically passive. Podman 5.8.4 rootless startup performs directory creation or chmod even for the global version flag; the passive application therefore constructs no command runner. An adapter must verify that its chosen interrogation does not initialize storage or otherwise mutate host state. Network reachability, registry authentication, writable storage tests and disposable container execution belong to the separate, explicitly enabled active track. `--active` currently enables only the reviewed local version/info sequence. The application validates the current identity, selected executable and named HOME/XDG policy before constructing its runner; version failure skips info. Workload probes remain deferred.

The info invocation is `podman --remote=false --trace=false info --format json`. The local-only `--trace=false` option is supplied at its default value: remote-only builds and configuration-selected remote mode reject it during flag parsing. Unknown-flag failure is final; there is no unguarded or remote retry. Source checks cover Podman 3.4.4, 4.4.4 and 5.8.4; native smoke tests reject configured-remote and a pinned remote-only client before connection attempts. See [inspection policy](podman-discovery.md#active-inspection).

Active Podman runs with the process's current credentials and namespaces. The scoped reader confines capagent file operations; it does not sandbox Podman or its helpers. Opt-in permits runtime initialization, directory/metadata changes, database access and runtime-owned processes such as the rootless pause process. Podman may execute mapping/version helpers and package queries, including their local identity-cache, logging and D-Bus calls. It neither pulls images nor requests containers, workload networking or storage-write tests. Version/info have 5/30 second budgets inside a 40 second evaluation deadline, with bounded output and pipe draining. Cancellation reaps the direct command and signals its process group; escaped descendants and initialized runtime state may remain. capagent never runs reset/prune or removes pre-existing storage. An uncatchable termination or host crash can prevent cleanup.

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

`CommandRunner.Run` accepts a `CommandSpec` with an absolute executable path and
literal arguments. Bare/relative executable names and embedded NUL are rejected
before starting a child; the runner never performs ambient `PATH` lookup or shell
interpretation. Executable discovery is a separate future adapter responsibility.
An empty working directory means `/`; a supplied directory must be absolute.
The parent working directory is never changed. An absolute path does not pin an
executable inode or prevent its owner from replacing it; the caller must trust
the executable and its filesystem location.

The zero `EnvPolicy` supplies only `PATH=/usr/bin:/bin` and `LC_ALL=C`.
`NewEnvPolicy(inherit, overrides)` captures only named inherited variables, then
applies explicit overrides. Missing inherited values are omitted and explicit
empty values are retained. Variable names use ASCII letters, digits and `_`,
with no leading digit; values cannot contain NUL. The snapshot is immutable and
sorted by key. Repeated runs do not reread ambient variables. Explicit overrides
may change defaults, including the child's `PATH`, but cannot steer the runner's
absolute executable selection. No `HOME`, `PWD`, proxy, loader or runtime endpoint
variable is inherited implicitly.

Bounded `systemctl --version` interrogation is eligible read-only host metadata.
This does not authorize general production shell/utility probes. The runner
provides no shell-command-string API, but it is not an executable allowlist or
sandbox: adapters must select reviewed commands and literal arguments whose
passive behavior is established. Inspection that initializes storage or mutates
runtime state must not be classified as passive. No live probe is added by these
execution primitives.

Credentials, supplementary groups, namespaces, umask and other process attributes
still come from the executing process. Target-user switching belongs to the
context/identity work, without changing global credentials inside concurrent
probes. Explicit endpoint/config/proxy settings require an adapter policy and
matching evaluation scope; selecting a remote endpoint must never label its
observations as local. Typed scope and target switching remain separate work.

Ordinary formatting of specifications, policies and runner-owned errors omits
command/configuration values. `errors.Is` and `errors.As` preserve underlying
startup/exit/drain error identity. Raw stdout/stderr, `EnvPolicy.Variables()`,
fake-call fields and explicitly unwrapped OS errors remain sensitive; consumers
must sanitize them before diagnostics or fixture capture. This is not automatic
redaction of arbitrary runtime output.

Root being able to read a user file or connect to a socket does not establish the target user's access. Evidence for different users, namespaces or runtime endpoints must stay separate. Runtime/configuration output and diagnostics may contain sensitive information; redaction and fixture sanitization are prerequisites for adapter delivery.

## Resource ownership and cancellation

The runner caps retained stdout and stderr at 1 MiB each and applies a default 30-second execution timer. A zero specification timeout uses the runner default; a positive value overrides it and a negative value is rejected. Already-cancelled contexts and invalid specifications start no child. Each buffer starts with 4 KiB capacity and grows within its cap. Temporary growth allocations and returned output copies are additional memory; these limits are not a total process-memory budget. Probe concurrency defaults to `min(runtime.NumCPU(), 8)`, with a minimum of one. Explicit positive `WithMaxConcurrency` values can exceed that default; zero and negative overrides are rejected.

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

`Orchestrator.Run` receives a caller-owned environment and waits for cooperative probes. It does not construct or close that environment. A scoped reader must not be closed concurrently with reads. The application layer owns production construction and closes its resources after workers finish. Process-group signaling cannot promise termination of every deliberately escaped descendant, and cleanup cannot be guaranteed after power loss or an uncatchable process termination. Future active probes must identify owned resources, bound cleanup attempts and report incomplete cleanup.

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

### Download and cache authority

The build uses microfat v0.2.2 with repository-pinned SHA-256 digests for both host
CLI archives and all four full/minimal launcher archives. The initial trust data
was derived from the upstream checksum file after verification of its Cosign
signature against the exact release workflow/tag identity and GitHub OIDC issuer.
The [trust manifest and update procedure](../scripts/trust/README.md) are reviewed
source inputs. Downloaded checksum files and cached binaries cannot replace them.

Archive verification precedes extraction. Extraction accepts the expected regular
executable exactly once, validates its size and digest, and rejects unsafe member
paths, links, excessive expansion and incomplete downloads. A complete set is
installed atomically under a version/host-architecture/manifest-digest cache key.
Installers use a bounded lock wait. Every cache use verifies ownership, file type,
permissions, size and digest; execution uses a freshly verified private snapshot.
Repository scripts never fall back to an ambient `microfat` executable.

A valid cache works offline. Corruption fails closed with the affected path; remove
that specific cache generation and retry to download it again. Loose historical
`bin/microfat*` files are ignored. `MICROFAT_VERSION` can select only versions with
complete committed trust manifests. There is no arbitrary mirror or trust override.

These are build-time controls implemented using Bash and Python's standard
library, with HTTPS downloads through curl. They trust the reviewed checkout,
system toolchain and current process owner. They do not isolate a hostile process
with the same user authority. Catchable termination cleans staging; forced process
or machine termination can leave an unused staging directory but cannot publish a
partial generation. These helpers add no dependency to the shipped capagent binary.

### Exact artifact verification and provenance

`make build` uses full development launchers. `make release-check` additionally
builds minimal launchers, verifies both bundled architectures, and packages the
exact verified bytes without signing or publishing. Compilation and packaging use
separate GoReleaser configurations and output directories: packaging cannot erase
or rebuild the verified payloads.

All seven payload variants and both selected launchers receive ELF architecture
and static-linking checks. Embedded launcher bytes must match the pinned stub;
embedded variant hashes must match the compiled payloads. Microfat verifies all
payload integrity checks, and archive verification checks the final executable
bytes again. Native amd64 `--help`/`--version` smoke is required in CI. ARM64 checks
are structural and integrity checks; they do not establish native ARM64 execution.

Each release includes actual SPDX and CycloneDX documents and `release-inputs.json`.
The latter records source commit/version, build tool versions, authenticated
microfat source identity, archive/member digests, launcher mode, payload hashes and
bundle/archive hashes. Archive SBOMs may not enumerate every compressed embedded
payload dependency; the input manifest explicitly identifies all payloads and the
launcher. Checksums cover the release archives, SBOMs, provenance and release notes.

### Workflow and publishing authority

External actions use full commit SHAs and explicit tool versions. Validation jobs
have read-only repository tokens, do not retain checkout credentials, and publish
JUnit/results through artifacts and job summaries. PR and manual release rehearsals
have neither signing identity nor release-write permissions. Tag builds perform
strict lint, race coverage, dependency verification, vulnerability and secret scans,
shell/workflow checks, and the same artifact rehearsal. Required tool absence is an
error; full lint does not fall back to `go vet`.

A separate publisher runs only for canonical-repository release-tag pushes whose
commit is reachable from `main`. It downloads the immutable artifact ID from the
same run, checks the independent job-output digest, accepts only the expected flat
regular-file set, verifies its checksums and source identity, and rechecks tag
provenance. It performs no checkout or compilation and executes no handoff scripts.
Only this job receives release-write and OIDC signing authority. It signs and
verifies the checksum file before publishing, and refuses to replace an existing
release. A maintainer-created release tag remains the publishing trigger; manual
dispatch only rehearses the pipeline.

This closes the build/release trust work in #31/#38 when its delivery checks pass.
M1.1 remains gated by its separate fuzz, fault, resource, and final verification
issues (#44–#46 and #49).

The hardening gate combines race tests, full lint, architectural contracts, vulnerability and secret scanning, resource regressions, bounded fuzz/fault tests and packaged-artifact verification. A clean dependency scan is not a source audit or proof that downloaded launcher binaries are trustworthy. Architecture checks enforce declared imports and recognizable host-I/O patterns; they are not a security sandbox for arbitrary code.

## Documentation and working material

`docs/` contains durable documentation for readers. Internal audits, implementation plans, raw evidence, benchmark runs and reconciliation logs live in the git-ignored `.work/` directory or CI artifacts. GitHub issues retain actionable public acceptance criteria so implementation does not depend on access to a developer's local notes.
