# Local Podman discovery and inspection

capagent can passively identify the current process and discover a local Podman executable:

```sh
capagent --runtime podman --json --pretty
capagent --runtime podman --context=current --podman-path /usr/bin/podman --json
```

Run capagent as the intended deployment user. Only `--context=current` is supported. Alternate users, other runtimes and remote endpoints are rejected. Add `--active` only when Podman startup writes are acceptable. Fixture and live selection are mutually exclusive. Linux 5.6+ with working `openat2` confinement is required; see [security](security.md).

Without `--active`, the application reads credentials, local account metadata, namespace links and executable metadata. It constructs no command runner and never launches Podman, including when capagent runs as root. It does not inspect Podman sockets or infer availability from them.

## Selection and interpretation

The fixed search order is `/usr/bin/podman`, `/usr/local/bin/podman`, then `/bin/podman`. Ambient PATH does not participate. The first existing candidate is selected, including one with unsuitable file metadata. An explicit absolute `--podman-path` selects only that path; failure never causes fallback. Permission failures remain visible and cannot establish that every candidate is absent. Normal symlinks use the existing scoped-reader confinement semantics.

The report separates these facts:

- `context` and `evaluation.current`/`target` contain the actual effective numeric identity. UID 0 is root. Current and target are separate snapshots of the same identity. Real/effective credential mismatch prevents runtime collection.
- Supplementary groups come from the kernel. Missing local passwd metadata does not erase a known UID/GID. Account lookup uses bounded local-file reads and does not provide arbitrary NSS directory-service lookup.
- `evaluation.namespaces` contains observed `user`, `mnt` and `net` namespace link identifiers. Missing identifiers stay unknown.
- `runtimes.podman.installed` is true for a selected regular file, false for a complete unsuccessful search, or null when discovery is uncertain. `path` retains the selected candidate or explicit override.
- `file.executable_bits` describes permission bits, not successful execution. Nullable ownership distinguishes an observed root owner from unknown ownership.
- `accessible` remains null. `version` and `cli_runnable` are unobserved in passive mode. No rootless, storage, DNS, engine-access or Quadlet capability is inferred from file presence or a UID.

`evaluation.mode` and `provenance` are both `live`; `evaluation.collection` is `passive` or `active`. Host fields that this probe set does not measure stay unobserved. Reports preserve observations, diagnostic codes and requirement results. `--debug` additionally prints codes on stderr.

## Why passive discovery defers version execution

Even `podman --version` can change runtime state during startup. A traced Podman 5.8.4 rootless invocation created runtime directories on a fresh setup and changed directory metadata on an initialized setup. Upstream's default-directory construction calls `mkdir`, then `chmod` when the directory already exists. Explicit HOME/XDG values do not eliminate this operation. [Podman v5.8.4 directory initialization](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/common/pkg/config/default.go#L500-L521)

Live discovery therefore emits `version_execution_deferred` when an executable candidate is present. It creates no replacement directories, changes no credentials and supplies no environment workaround. Version and effective-info collection require the explicit `--active` opt-in. This preserves passive discovery for both fresh and initialized accounts.

The default requirement is `runtime.podman`: the selected CLI returned a recognizable version. Presence alone cannot satisfy it:

| Result | Requirement / exit |
|---|---|
| Executable candidate found; execution deferred | INDETERMINATE / 2 |
| Complete search finds no candidate | UNSATISFIED / 1 |
| Inaccessible, unsuitable or uncertain candidate | INDETERMINATE / 2 |
| Invalid selection | Usage error / 64 |
| Required platform initialization or report output fails | Execution error / 70 |

## Active inspection

```sh
capagent --runtime podman --active --json --pretty
capagent --runtime podman --active --context=current --podman-path /usr/bin/podman --json
```

This permits the selected local Podman CLI to collect its version and effective runtime information. Podman can initialize directories and databases, change metadata, discover a local D-Bus service and retain a rootless pause process. capagent does not request containers, image pulls, DNS/registry tests or storage-write tests. It does not undo Podman startup state, run reset/prune or promise that escaped processes disappear after cancellation. Run it as the intended user and treat the selected executable and its configuration as trusted runtime authority.

The literal commands are `podman --version`, then `podman --remote=false --trace=false info --format json`. The hidden local-only trace option is explicitly false, its normal default. A remote-only build or configuration-selected tunnel mode rejects this option before transport setup; capagent never retries without the guard. `--remote=false` alone is insufficient for these cases. Source checks cover [3.4.4](https://github.com/containers/podman/blob/v3.4.4/cmd/podman/root.go), [4.4.4](https://github.com/containers/podman/blob/v4.4.4/cmd/podman/root.go) and [5.8.4](https://github.com/containers/podman/blob/v5.8.4/cmd/podman/root.go). Other builds that reject the guard produce an unavailable inspection result.

Both commands use an immutable policy: `PATH=/usr/bin:/bin`, `LC_ALL=C`, directory `/`, and named snapshots of HOME, XDG_CONFIG_HOME, XDG_DATA_HOME and XDG_RUNTIME_DIR. If HOME is absent, known current-user account metadata supplies it. Supplied directory values must be absolute, canonical and valid; HOME must exist. Rootless inspection requires an existing runtime directory owned by the current UID with owner read/write/search permissions. Missing optional config/data directories may be created by Podman. The application checks these prerequisites without creating directories itself. Unknown supplementary groups or mismatched real/effective credentials prevent command dispatch.

The policy excludes ambient endpoint, proxy, authentication, configuration-selection, storage-override, loader, D-Bus-address and Podman-internal environment variables. Podman reads normal configuration under the declared HOME/XDG policy, so the result describes that policy rather than every shell-specific override. Local D-Bus discovery can fail or fall back according to Podman's configuration; diagnostics never fabricate a universal missing-socket explanation for daemonless local Podman.

Version has a 5-second budget, info 30 seconds, and overall collection 40 seconds. The runner caps each output stream at 1 MiB; the version parser additionally caps input at 4 KiB. Version failure skips info. Timeout, cancellation, malformed output, unknown flag, nonzero exit and truncation remain visible through fixed diagnostic codes without raw stdout/stderr or environment contents, including under `--debug`.

Active reports add:

- `version_details` and `cli_runnable` from the CLI version observation. Info failure does not erase them; conflicting CLI/info versions receive a diagnostic and cannot satisfy inspection.
- `accessible` for a successful recognizable local info operation. False means completed command unavailability; null means unobserved or incomplete collection. It does not promise a container can start.
- `network_backend`, `storage_driver`, `cgroup_version`, `cgroup_manager`, `rootless`, `graph_root` and `run_root` from typed effective info fields. Explicit `false` and known empty roots/cgroup strings remain distinct from missing values. Missing fields produce diagnostics; no backend is guessed from a version or `networkBackendDir`.
- `runtime.podman.info`, supported with runtime evidence and derived confidence only when all requested field groups and a recognizable version are present. Partial measurements remain unknown. Historical 3.x captures can therefore parse successfully while lacking fields needed to satisfy this predicate.
- `runtime.podman.netavark`, a separate prerequisite combining a selected backend and executable helper metadata. Missing/unreadable helper metadata cannot erase otherwise successful engine inspection. It does not establish network or DNS operation.

Runtime cgroups are reported under `runtimes.podman`; they are not direct host measurements. Collection completeness describes the observation transport, while each capability retains its own missing-field semantics. The active requirement is `all(runtime.podman, runtime.podman.info)`:

| Active result | Requirement / exit |
|---|---|
| Recognizable CLI and complete local info | SATISFIED / 0 |
| Good CLI; failed, malformed, partial or missing-field info | INDETERMINATE / 2 |
| Version failure or invalid inspection environment | INDETERMINATE / 2; no info command |
| No executable candidates | UNSATISFIED / 1; no commands |
| Complete info with CNI or a missing Netavark helper | Inspection may be SATISFIED / 0; separate Netavark verdict remains negative/unknown |

Exit 0 authorizes no deployment. Rootless suitability, user systemd, Quadlet and workload requirements remain future work. Consumers can require both inspection predicates:

```sh
set -o pipefail
capagent --runtime podman --active --json |
  python3 examples/check-report.py runtime.podman.info
```

## Version fixture replay

The same parsers and command observations are available through offline fixtures:

```sh
capagent --fixture testdata/fixtures/v1/version-supported --json |
  python3 examples/check-report.py runtime.podman
```

These reports remain `fixture` mode. The parser extracts numeric major/minor/patch and preserves vendor suffixes, build metadata and recognized trailing hashes/architectures. It retains a bounded version line using a restricted printable grammar. Numeric components are separate from vendor package revisions; no historical feature support is inferred. Malformed, truncated, cancelled and unsuccessful commands cannot establish a supported CLI. A successful version replay establishes no engine access.

Version transcripts under `testdata/podman/version/` distinguish captured output from synthetic edge cases. Replay documents use synthetic identity/filesystem metadata and describe their mixed provenance explicitly. Existing Netavark fixtures and the consumer's default `runtime.podman.netavark` key remain available. See [fixture evaluation](fixture-evaluation.md) for the fixture format and evidence rules.

Combined `inspection-*` replay scenarios exercise the version → info sequence, helper metadata, partial output and conservative verdicts. `--fixture` plus `--active` remains a usage error.
