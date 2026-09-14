# Portable Podman host collector

`collect-podman-host.sh` is a single-file POSIX shell launcher with embedded Python.
Requires **Linux and Python 3.8+**, with no third-party Python packages. It does not
require Bash, jq, GNU timeout, an external tar/gzip executable, or sudo.

This is a replacement implementation, not a patch committed to your repository.

## Run it

Run as the actual deployment user. Do not add sudo just to collect more output:
that selects root's execution context, not the original rootless user's context.

```sh
# Passive collection. No Podman, helper-version or user-manager execution.
sh collect-podman-host.sh --output-dir /tmp

# Local runtime inspection and user-manager queries. No canary or image pull.
sh collect-podman-host.sh --active --output-dir /tmp

# More useful error transcripts for troubleshooting; review before sharing.
sh collect-podman-host.sh --active --include-logs --output-dir /tmp

# Include your explicitly selected, trusted capagent build.
sh collect-podman-host.sh --active --include-logs \
  --capagent ./bin/capagent --output-dir /tmp

# Explicit local-image canary; this image must already be in the user's store.
sh collect-podman-host.sh --active --canary \
  --image docker.io/library/alpine:3.22 --output-dir /tmp

# Explicit permission to pull that image IF it is missing.
sh collect-podman-host.sh --active --canary --allow-pull \
  --image docker.io/library/alpine:3.22 --output-dir /tmp

# Select a nonstandard Podman installation; failure never selects another binary.
sh collect-podman-host.sh --active --podman-path /opt/podman/bin/podman

# Additional selected Quadlet/systemd source. Default is metadata only.
sh collect-podman-host.sh \
  --unit-file "$HOME/.config/containers/systemd/example.container"

sh collect-podman-host.sh --help
```

A registry digest can be used instead of a tag. A full local SHA256 image ID is
also accepted when pulling is not requested. There is no implicit default image
and no short-name fallback. The collector resolves the image to its full local ID
and always uses `--pull=never` for the actual run.

The canary overrides the entrypoint with `/bin/echo`, expects a unique output
marker, disables networking, drops capabilities, and requests a read-only root
filesystem and no-new-privileges. Choose a trusted image containing `/bin/echo`.
This is a basic execution test, **not** a DNS, network, writable-storage, or Quadlet
deployment test. Trusted Podman configuration can still inject hooks or mounts.

## Output and exit status

The archive's absolute path is the only stdout output. Progress, warnings, and the
human summary use stderr. `--quiet` suppresses progress and the human summary, not
important warnings.

```sh
archive=$(sh collect-podman-host.sh --quiet --output-dir /tmp)
tar -tzf "$archive"
tar -xOf "$archive" podman-diagnostics/summary.json
```

Exit 0 means a completed archive was published, **not** that the host is healthy
or every observation succeeded. Inspect `summary.json`, command statuses, and the
I/O manifest. Unreadable/missing/invalid sources do not erase unrelated evidence.
An interrupted collection attempts bounded canary cleanup and publishes the
available partial evidence, then exits `128 + signal` (130 for INT, 143 for TERM).
Argument errors use exit 2; fatal collection/output failures use exit 1.

Useful archive entries:

- `summary.json`: collection scope, actual query outcomes, current-user local-file
  subordinate-ID observations, budget exhaustion and cleanup state.
- `metadata.json`: version, limits, privacy notice and limitations.
- `collection/commands.json`: exit codes, timings, output limits, JSON syntax
  validity and interruption/timeout outcomes.
- `collection/io.json`: path-specific filesystem evidence and failures.
- `commands/*.stdout.json`: independently parsed, sanitized JSON. Stderr is never
  concatenated with stdout. JSON syntax validity is not full schema validation.
- `configs/`: source metadata and parsed/sanitized configuration, plus an index of
  vendor/system/user sources, drop-ins, and inventoried ambient override files.
- `executables/`, `helpers/`, `quadlet/`, `host/`, `identity/`: separate evidence.

Capagent's valid JSON output is retained even with an exit code of 1 or 2.
Individual command exit codes are not repurposed as the collector's exit status.

## Privacy

Archive creation uses private mode bits from the start: `0600` archives and `0700`
working directories. Tar member owner/group names are empty and their numeric
ownership is normalized. Names contain a timestamp and random identifier, not the
original hostname. Complete archives are atomically published without overwriting
existing files. Filesystems without hardlink support use a private subdirectory
fallback; the printed path identifies the resulting archive. Filesystems that
cannot enforce private POSIX mode bits are rejected.

Every saved document passes the privacy boundary. JSON/TOML secret keys and opaque
environment/exec fields are removed structurally; known local identity, hostname,
IPv4/IPv6 and dotted-name strings are anonymized. Pseudonyms are consistent within
one run, with an ephemeral key that is not shipped in the archive. Numerically
useful UID/GID, subordinate-ID range and permission evidence remains visible.
Some private filenames may also be pseudonymized.

**Redaction is best effort, not a proof that arbitrary data is safe to publish.**
Review the archive before sharing it. Free-form command transcripts and kernel
command-line contents are omitted unless `--include-logs` or `--no-redact` is
explicitly requested. `--include-logs` enables best-effort text filtering;
`--no-redact` deliberately retains sensitive raw content. There is no upload step.

On Python 3.11+, the standard-library TOML parser produces sanitized JSON views.
On Python 3.8-3.10, TOML content is omitted with an explicit reason (unless raw mode
is selected). There is no unreliable regex TOML parser and no silent raw fallback
in redacted mode. Malformed, oversized, changed-during-read and final-symlink
configuration contents are also omitted; metadata remains available.

Standard credential-store files are not requested. However, explicit configuration
overrides and selected unit files are trusted inputs: raw mode can retain any
sensitive contents they contain.

## Execution policy

The default collector does not launch Podman or query the user manager. It does
use isolated, bounded Python read workers, and it may run an installed trusted
capagent without `--active`. Use `--no-capagent` to disable that integration.
Neither Podman nor capagent is auto-selected from the working directory or ambient
PATH. Explicit binary paths are resolved once relative to the launching directory.

Runtime commands receive a fixed PATH/locale and selected HOME/XDG values. Ambient
remote endpoints, proxies, authentication overrides, loader settings and
configuration-selection variables are not forwarded. Ambient config override files
are inventoried for comparison, but are **not** applied to the controlled runtime
inspection. Both capagent and direct runtime inspection select the same Podman path.

Direct Podman operations retain `--remote=false --trace=false`, following capagent's
local-build guard. Builds that reject the guard fail closed; there is no retry that
could redirect collection to a remote engine. Local configuration and selected
executables must be trusted; this is not a sandbox for compromised runtimes.

The collector inventories candidate configuration files rather than pretending to
implement every Podman version's full precedence/module rules. Effective runtime
information comes from active `podman info`. Quadlet candidate presence and socket
presence are not reported as proof of a healthy service or working deployment.
Subordinate-ID assessment is scoped to the current username/numeric UID in the local
files; external libsubid/NSS providers and range overlap are not evaluated.

## Limits and cleanup

Defaults: 8 seconds for ordinary commands, 30 seconds for Podman info/canary,
120 seconds for an explicitly permitted pull, 2 seconds per filesystem worker,
180 seconds for collection, 1 MiB per read/output stream, and a 32 MiB saved-content
budget. The aggregate manifests have reserved space outside the content budget.
Use `--help` for adjustable limits.

Commands receive no stdin, start in their own session, and are subject to bounded
capture, TERM/KILL escalation and bounded pipe draining. No shell command strings
are evaluated. Optional helper-version execution requires both `--active` and
`--helper-versions`; mapping helpers are metadata-only.

Canary cleanup checks the unique collector label and removes only the validated
full container ID, never arbitrary containers or a broad name prefix. Pulled
images are retained deliberately. Startup state changes made by Podman are not
rolled back. Failed/uncertain cleanup remains visible for manual review.

SIGKILL, power loss, uninterruptible kernel I/O and processes escaping their process
group cannot be made reliably recoverable by this portable script. Cleanup has a
separate bounded budget. Archiving/filesystem writes are outside the collection
deadline; prefer a local output filesystem. Configuration parent directories and
the selected runtime authority must be trusted.

## Validation

```sh
python3 test_collect_podman_host.py ./collect-podman-host.sh
```

The supplied test suite has 50 passing tests in the authoring environment, including
an actual passive run, a mocked complete active/canary run, a SIGTERM partial-archive
run, permission-denied reads, FIFOs, symlinks, output floods, stuck pipes, nonzero
JSON reports, archive collisions, no-hardlink fallback, and redaction fixtures.
Python 3.8 grammar compatibility was checked; actual execution was tested on Python
3.13.5, not every supported Python version or Linux distribution. No live Podman
container or image pull was used for validation. See `collector-test-results.txt`.
