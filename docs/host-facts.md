# Passive host facts

`capagent --json --pretty` collects facts in the executing user's current mount,
PID, user and network namespaces. Bare invocation uses the same JSON mode.
`--runtime podman` includes these facts in its existing runtime report.
Alternate target identities remain outside this mode.

The initial host slice reports OS identity, kernel release and architecture,
and systemd installation, PID-1 state, utility presence and version. Later M2
slices add topology, security and network/filesystem prerequisites.

Host-only reports have empty runtime/capability maps and no
`evaluation.requirement`. Exit 0 means collection completed; exit 2 means required
observations are incomplete. Confirmed absence is a measurement, while inaccessible
sources remain unknown. Optional OS fields and untested manager accessibility do
not determine completeness. Usage errors return 64; confinement or report failures
return 70. A host-only report is not a deployment decision. Existing capability
consumers reject it. Podman reports retain their existing requirement exit codes,
independently of host collection completeness.

The existing `host` object provides summary fields. Typed values appear under
`evaluation.observations[].host`, alongside source references, timestamps,
completeness and sanitized diagnostics. Raw command/file contents are not emitted.
Host-only scope has paired empty `runtime` and `endpoint` strings; they are never
wildcards for runtime evidence. Schema v1 remains pre-release.

OS release parsing follows the [systemd contract](https://github.com/systemd/systemd/blob/main/man/os-release.xml).
Only a missing `/etc/os-release` permits the `/usr/lib/os-release` fallback.
Unreadable or malformed primary data remains incomplete. Repeated keys use the
last valid assignment with a diagnostic. Kernel numeric components are metadata;
vendor versions do not prove feature availability. Architecture aliases are
normalized to amd64/arm64 while retaining the original machine name.

Systemd running state refers to PID 1 in the evaluation namespace. Metadata
collection may run only `/usr/bin/systemctl --version` or `/bin/systemctl --version`,
with a two-second deadline, working directory `/`, fixed PATH and C locale, and
bounded output. There is no bus interrogation. Podman version/info execution
continues to require `--active`. Native trace tests check the exact metadata
command and reject filesystem mutations, namespace changes, communication and
unexpected child activity. Go's bounded PIDFD feature-probe child is verified to
only exit before the metadata command is launched.

Replay a synthetic host report with:

```sh
capagent --fixture testdata/fixtures/v1/host-basic --json --pretty
```

Host fixtures use `probe: "host"`, explicit syscall responses and optional exact
systemctl command responses. Replay performs no native host-query or command calls.
Corpus provenance distinguishes synthetic distributions from captured userland
files. Linux 5.6+ and functioning openat2 confinement remain mandatory, including
when parsing fixtures representing older distributions.
