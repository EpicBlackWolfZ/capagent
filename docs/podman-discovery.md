# Local Podman discovery

capagent can passively identify the current process and discover a local Podman executable:

```sh
capagent --runtime podman --json --pretty
capagent --runtime podman --context=current --podman-path /usr/bin/podman --json
```

Run capagent as the intended deployment user. Only `--context=current` is supported. Alternate users, other runtimes, remote endpoints and `--active` are rejected. Fixture and live selection are mutually exclusive. Linux 5.6+ with working `openat2` confinement is required; see [security](security.md).

The application reads credentials, local account metadata, namespace links and executable metadata. It constructs no command runner and never launches Podman, including when capagent runs as root. It does not inspect Podman sockets or infer availability from them.

## Selection and interpretation

The fixed search order is `/usr/bin/podman`, `/usr/local/bin/podman`, then `/bin/podman`. Ambient PATH does not participate. The first existing candidate is selected, including one with unsuitable file metadata. An explicit absolute `--podman-path` selects only that path; failure never causes fallback. Permission failures remain visible and cannot establish that every candidate is absent. Normal symlinks use the existing scoped-reader confinement semantics.

The report separates these facts:

- `context` and `evaluation.current`/`target` contain the actual effective numeric identity. UID 0 is root. Current and target are separate snapshots of the same identity. Real/effective credential mismatch prevents runtime collection.
- Supplementary groups come from the kernel. Missing local passwd metadata does not erase a known UID/GID. Account lookup uses bounded local-file reads and does not provide arbitrary NSS directory-service lookup.
- `evaluation.namespaces` contains observed `user`, `mnt` and `net` namespace link identifiers. Missing identifiers stay unknown.
- `runtimes.podman.installed` is true for a selected regular file, false for a complete unsuccessful search, or null when discovery is uncertain. `path` retains the selected candidate or explicit override.
- `file.executable_bits` describes permission bits, not successful execution. Nullable ownership distinguishes an observed root owner from unknown ownership.
- `accessible` remains null. `version` and `cli_runnable` are unobserved in live mode. No rootless, storage, DNS, engine-access or Quadlet capability is inferred from file presence or a UID.

`evaluation.mode` and `provenance` are both `live`. Host fields that this probe set does not measure stay unobserved. Reports preserve observations, diagnostic codes and requirement results. `--debug` additionally prints codes on stderr.

## Why version execution is deferred

Even `podman --version` can change runtime state during startup. A traced Podman 5.8.4 rootless invocation created runtime directories on a fresh setup and changed directory metadata on an initialized setup. Upstream's default-directory construction calls `mkdir`, then `chmod` when the directory already exists. Explicit HOME/XDG values do not eliminate this operation. [Podman v5.8.4 directory initialization](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/common/pkg/config/default.go#L500-L521)

Live discovery therefore emits `version_execution_deferred` when an executable candidate is present. It creates no replacement directories, changes no credentials and supplies no environment workaround. Live version and effective-info collection await the explicit opt-in boundary. This preserves passive discovery for both fresh and initialized accounts.

The default requirement is `runtime.podman`: the selected CLI returned a recognizable version. Presence alone cannot satisfy it:

| Result | Requirement / exit |
|---|---|
| Executable candidate found; execution deferred | INDETERMINATE / 2 |
| Complete search finds no candidate | UNSATISFIED / 1 |
| Inaccessible, unsuitable or uncertain candidate | INDETERMINATE / 2 |
| Invalid selection | Usage error / 64 |
| Required platform initialization or report output fails | Execution error / 70 |

## Version fixture replay

The version parser and command observation are available through offline fixtures:

```sh
capagent --fixture testdata/fixtures/v1/version-supported --json |
  python3 examples/check-report.py runtime.podman
```

These reports remain `fixture` mode. The parser extracts numeric major/minor/patch and preserves vendor suffixes, build metadata and recognized trailing hashes/architectures. It retains a bounded version line using a restricted printable grammar. Numeric components are separate from vendor package revisions; no historical feature support is inferred. Malformed, truncated, cancelled and unsuccessful commands cannot establish a supported CLI. A successful version replay establishes no engine access.

Version transcripts under `testdata/podman/version/` distinguish captured output from synthetic edge cases. Replay documents use synthetic identity/filesystem metadata and describe their mixed provenance explicitly. Existing Netavark fixtures and the consumer's default `runtime.podman.netavark` key remain available. See [fixture evaluation](fixture-evaluation.md) for the fixture format and evidence rules.
