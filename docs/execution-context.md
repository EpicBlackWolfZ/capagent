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

Current-identity metadata, groups and namespace completeness remain separate. Subordinate ID lists retain original order; validation rejects zero-length, overflow and overlapping ranges without mutating their source. Host-only collection exits 0 when host and context collection are complete, 2 for incomplete observations, 64 for invalid options and 70 for a failed worker or report transport. These are collection outcomes, not deployment approval.

The private worker protocol binds bounded JSON to a run, target credentials and namespace scope. It uses a pinned executable descriptor, including for sealed memfd and deleted-cache payloads, then closes that descriptor before dropping privilege. It does not change ordinary command-runner authority. Cancellation reaches the worker and its owned command groups; a bounded fallback terminates an unresponsive worker. Processes deliberately escaping command ownership are outside the cleanup guarantee.
