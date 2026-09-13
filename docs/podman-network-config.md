# Podman network and DNS prerequisites

Local Podman assessment projects the network, container DNS and rootless helper
settings from the same selected `containers.conf` reads as the
[engine configuration](podman-engine-config.md). The target identity, declared
HOME/XDG environment, qualified Podman 4.9.3/5.8.4 profiles, source order, drop-ins,
TOML bounds and complete-read digests are shared. Each family keeps its own
recognized-field projection and diagnostics. No DNS query, interface lookup,
network namespace, firewall change or helper execution is performed by this
collector.

## Selection and configuration

Network scalars replace earlier assignments. DNS lists, plugin directories,
pasta options and slirp options use the attributed-array append/reset behavior
of `containers.conf`. An invalid string that a later source replaces cannot
invalidate the final selected value. Wrong TOML types remain source errors.
Invalid or unmodeled list elements are redacted to empty strings with explicit
index markers; the markers and their origins follow the same merge operation.

The projection covers these settings:

| Section | Settings |
| --- | --- |
| `network` | `network_backend`, `default_rootless_network_cmd`, `network_config_dir`, `cni_plugin_dirs`, `netavark_plugin_dirs`, `dns_bind_port`, `pasta_options`, and the 5.8.4 `firewall_driver` field |
| `containers` | `dns_servers`, `dns_options`, `dns_searches` |
| `engine` | `network_cmd_path` and `network_cmd_options` for slirp4netns |

The qualified rootless command defaults are slirp4netns in 4.9.3 and pasta in
5.8.4. An explicitly empty rootless command retains the legacy slirp4netns
alias in both profiles. Empty DNS/slirp option lists and DNS bind port zero are source defaults;
zero does not establish a bound listener or cause a configuration error.
Unconfigured backend selection remains unknown: it can depend on existing
storage/network state and build-time CNI support. An explicit CNI choice is
reported without inferring that this Podman build supports CNI. No sentinel
file is created or migrated by assessment.

Pasta arguments retain only attributed-array cardinality and source provenance;
arbitrary helper arguments and their values are not published. Their compatibility
with the installed helper is unverified. Slirp options project the recognized
CIDR, port handler, loopback, IPv6 and MTU values. Outbound addresses that require
an interface-name lookup remain unmodeled. Helper flag/version compatibility,
network-config directory contents and firewall/plugin implementation behavior
are separate missing evidence, even when the recognized projection is complete.
DNS addresses and search settings describe configuration; they do not establish
name resolution or the resolver configuration of an eventual workload.

## Helper search and evidence

Netavark and Aardvark use the selected engine helper directories without a PATH
fallback. Pasta and slirp4netns also search the declared `/usr/bin:/bin` PATH.
An explicit `engine.network_cmd_path` selects the slirp path directly. Unknown
built-in helper directories, inherited unmeasured array entries and `$BINDIR`
expansion prevent a complete configured search plan. `$BINDIR` depends on the
actual executable and symlink resolution; the discovery path alone is insufficient.

Each configured helper observation records its source, candidates, selected path,
file metadata and the target's kernel executable-access result. At most 16
candidates are checked per helper. A denied or incomplete measurement remains
unknown; an observed missing or unsuitable required helper is a configuration
problem. Inventory findings cannot substitute for selected-helper evidence.
An executable special file encountered during a helper search makes selection
unknown: Go's lookup can select it, but capagent cannot establish executable
suitability or safely assume that a later regular file would be selected.
Runtime-reported effective paths and backend choices outrank configured choices
within the same target/runtime scope.

## Capability contract

| Capability | Meaning |
| --- | --- |
| `runtime.podman.config.network.parsed` | Qualified network sources and recognized selected settings are interpreted within the documented bounds |
| `runtime.podman.config.dns.parsed` | The selected container DNS projection is complete within those bounds |
| `runtime.podman.network.backend.netavark` | Configured or runtime-effective backend choice is Netavark |
| `runtime.podman.network.rootless.pasta` | The selected rootless default command is pasta; not applicable to rootful services |
| `runtime.podman.network.rootless.slirp4netns` | The selected rootless default command is slirp4netns; not applicable to rootful services |

The existing `runtime.podman.netavark` retains its narrower backend-plus-helper
file-metadata meaning. The existing `runtime.podman.netavark.executable`,
`runtime.podman.aardvark_dns.executable`, `runtime.podman.pasta.executable` and
`runtime.podman.slirp4netns.executable` additionally require executable access to
the selected path. Combine the relevant choice, helper and DNS predicates in a
requirement. A satisfied prerequisite requirement does not prove working DNS,
forwarding, port publication, reachability or container-network creation.

Replay the bounded rootless requirement with:

```sh
capagent --fixture testdata/fixtures/v1/network-rootless --json --pretty
```

Use `network-rootful` for the rootful prerequisite example. The missing-Aardvark
fixture `network-helper-missing` exits 1; `network-helper-denied` exits 2 because
executable access could not be measured. These fixture requirements use only the
published IDs above and retain source, helper and superseded runtime evidence.

Sanitized native network measurements are retained in `testdata/podman/network/`.
The capture exporter preserves typed configuration, source digests and effective
rootless command metadata, while reconstructing info JSON from published fields.
It does not retain raw configuration contents or command output. Native execution
evidence is Linux amd64; arm64 builds and packaging are checked statically.

The search and defaults are pinned to upstream sources:
[4.9.3 helper selection](https://github.com/containers/podman/blob/v4.9.3/vendor/github.com/containers/common/pkg/config/config.go),
[5.8.4 helper selection](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/common/pkg/config/config.go),
[Netavark selection](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/common/libnetwork/network/interface.go),
[slirp options](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/common/libnetwork/slirp4netns/slirp4netns.go),
[container resolver construction](https://github.com/containers/podman/blob/v5.8.4/libpod/container_internal_common.go),
and [Go executable lookup](https://github.com/golang/go/blob/go1.22.12/src/os/exec/lp_unix.go).
