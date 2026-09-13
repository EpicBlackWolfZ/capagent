# Execution contexts

The default `--context=current` evaluates the current effective UID, GID and kernel supplementary groups. Missing local account metadata does not erase numeric credentials. Explicit `--context=user:NAME` and `--context=uid:ID` select a deployment identity from bounded `/etc/passwd` and `/etc/group` reads. UID 0 is valid; the reserved all-ones UID/GID is rejected. Duplicate or malformed account records prevent resolution. Remote NSS and directory-service lookup are not provided by this statically linked binary.

```sh
capagent --context=current --json
sudo capagent --context=user:alice --json
sudo capagent --context=uid:1000 --runtime=podman --active --json
```

A selection with the exact current credentials can run directly. Otherwise only root can delegate: a separate process re-executes the pinned running Go payload, installs the target supplementary groups, then drops real/effective/saved GID and UID before collection. The worker verifies all credentials, checks that non-root retained no effective, permitted, inheritable or ambient capabilities, and verifies namespace identity. Failure terminates that worker. The launcher never changes its credentials.

Delegation inherits namespaces, mounts, cgroups and process restrictions. It does not enter a login session, run PAM, create a user namespace, start a user manager or prove that a fresh login has the same environment. Target commands receive explicit target HOME and a candidate `/run/user/UID`; no launcher HOME, XDG or bus-address values are inherited. A candidate runtime path is not proof of an active session.

The JSON trace separates `current` (launcher), `target` (requested local credentials) and `execution` (verified measuring credentials). `selection` preserves the requested selector, even when resolution fails. Observations carry `authority`: `execution` for measurements in the selected context or `current` for launcher-only identity evidence. Unknown target execution suppresses target probes. An unresolved explicit target stays null and never silently becomes root or the launcher.

Current-identity metadata, groups and namespace completeness remain separate. Subordinate ID lists retain original order; validation rejects zero-length, overflow and overlapping ranges without mutating their source. Host-only collection exits 0 when host and context collection are complete, 2 for incomplete observations, 64 for invalid options and 70 for a failed worker or report transport. A planned context probe that fails, is cancelled before dispatch or is skipped after a dependency failure keeps context collection partial, even when it returns no observation. Retained observations remain in the report. Unrequested probes and intentionally deferred active queries do not count as missing required work. These are collection outcomes, not deployment approval.

With `--runtime=podman --requirement FILE`, the launcher reads and validates the
bounded requirement before delegation. The worker receives those owned JSON
bytes and validates them again; it never reopens the caller's file. Requirement
outcomes describe the selected target's evidence. `--explain` formats the returned
report in the launcher without running additional probes or commands. See
[Podman assessment](podman-assessment.md) for input bounds, examples and exits.

The private worker protocol binds bounded JSON to a run, target credentials and namespace scope. It uses a pinned executable descriptor, including for sealed memfd and deleted-cache payloads, then closes that descriptor before dropping privilege. It does not change ordinary command-runner authority. Cancellation reaches the worker and its owned command groups; a bounded fallback terminates an unresponsive worker. Processes deliberately escaping command ownership are outside the cleanup guarantee.

## Subordinate IDs and mapping helpers

Live reports include `context.subids` observations with independent UID/GID pools, selected source records, actual ranges and totals. Username and numeric owner keys both participate. Adjacent ranges are allowed; duplicates, overlaps (including conflicts with another owner), zero-length ranges and overflow remain invalid. The parser retains selected input order and invalid range values. It bounds file bytes, lines and records; malformed or incomplete input cannot produce a trusted total. Missing files produce observed empty local allocations; permission errors and unresolved account metadata preserve unknown totals.

The `provider` field distinguishes the local `files` default from an explicit external `subid` NSS provider or an unreadable/ambiguous configuration. Local ranges remain visible when an external provider is configured, but are not advertised as effective allocations. Workload requirements decide the necessary mapping size; a small allocation is not automatically invalid.

`newuidmap` and `newgidmap` use fixed `/usr/bin`, `/usr/local/bin`, `/bin` discovery in that order. The first existing or inaccessible candidate stops the search. Reports retain file kind, ownership, mode, setuid/setgid bits, decoded permitted/inheritable mapping capability bits from security.capability revisions 1–3, mount nosuid/noexec flags, and current process no-new-privileges. `executable` comes from a kernel access query in the verified execution identity, including ACL and mount policy; unsupported access-query kernels leave it unknown. File-read permission is never substituted for target execution permission.

Passive collection executes neither mapping helper and creates no namespaces or containers. `usable` is false for a measured execution obstacle and otherwise unknown: file privilege metadata alone cannot prove successful mapping under namespace, capability bounding-set, LSM and allocation policy. `privilege_blocked` records measured restrictions on gaining privilege, independently of privileges the caller already holds.

Replay the bounded synthetic rootless context with `capagent --fixture testdata/fixtures/v1/context-rootless --json --pretty`. Its allocations and helper access responses are explicit fixture measurements; they are not evidence about the machine replaying it.

## Runtime directory and user manager

Without `--active`, `context.user` inspects runtime-directory metadata, `$XDG_RUNTIME_DIR/systemd/private`, and `/var/lib/systemd/linger/NAME`. An explicit current-user XDG value is honored only when it is canonical and absolute. An unset value or delegated target uses `/run/user/UID` as a candidate. Empty/invalid explicit values do not silently fall back. Directory ownership must match the target UID and permissions must be exactly 0700, with no extra privilege bits. This is the same check used before Podman inspection. The [XDG specification](https://specifications.freedesktop.org/basedir/latest/) defines the ownership and permission requirement.

A private socket must have socket type and target ownership. Its presence leaves `accessible` null in passive mode. Lingering is an independent observed configuration marker, including when no user manager is running. Missing account metadata or denied reads remain unknown. Namespace and session restrictions are inherited from the executing process; a valid directory alone cannot establish a functioning login session.

```sh
capagent --active --json --pretty
sudo capagent --context=user:alice --active --json
```

Active mode can issue the fixed `systemctl --user --no-pager --no-ask-password show --property=Version --value` operation with a two-second command timeout. Its environment includes only fixed PATH/locale defaults, the selected XDG directory and an explicit local private-bus address, escaped according to the [D-Bus address specification](https://dbus.freedesktop.org/doc/dbus-specification.html#addresses). It cannot inherit a remote bus address or start a manager. `query_attempted`, nullable `accessible` and `manager_version` preserve the difference between deferred collection, a successful query, a completed unavailable query, and an incomplete/invalid reply.

A completed nonzero query records `accessible: false`. Startup failure, cancellation, timeout, truncated output and incomplete output draining retain `accessible: null` with partial collection. This includes a nonzero child exit whose output pipe remains open in a descendant. Diagnostics expose fixed codes and messages only.

A successful manager query is not proof of Quadlet or container support. Without `--runtime`, even active collection produces no workload requirement verdict. Replay the successful query without live I/O with `capagent --fixture testdata/fixtures/v1/context-user-active --json`.
