# Podman engine configuration assessment

`capagent --runtime podman --json` inspects candidate `containers.conf` sources
under the selected deployment identity. `--active` additionally permits the
existing bounded local Podman inspection. A successfully measured, qualified
Podman version determines which source-selection rules apply. Configuration
values describe prerequisites and defaults; they do not establish working
containers, storage, DNS, image pulls, or generated systemd units.

## Target and source selection

Configuration collection uses the same declared `HOME`, `XDG_CONFIG_HOME`,
`XDG_DATA_HOME`, and `XDG_RUNTIME_DIR` policy as runtime inspection. User sources
use `XDG_CONFIG_HOME`, falling back to `HOME/.config`. Delegated evaluation uses
the target account and credentials, as described in [target contexts](execution-context.md).
The caller's unrelated environment does not select configuration for that user.

The qualified source profiles are deliberately narrow:

| Profile | Sources, in increasing precedence |
| --- | --- |
| Podman 4.9.3 | `/usr/share/containers/containers.conf`; `/etc/containers/containers.conf`; its `.d` directory; target-user file and its `.d` directory for rootless targets |
| Podman 5.8.4 | The same vendor/system sources; for rootless targets, `/etc/containers/containers.rootless.conf`, its `.d` directory, then `.d/UID`; target-user file and its `.d` directory for both rootless and rootful targets |

Each selected directory contributes non-directory entries ending in `.conf`,
sorted by filename. Selection is one level deep. These revisions do not load
vendor drop-ins. Modules are not selected, and capagent does not inherit
`CONTAINERS_CONF`, `CONTAINERS_CONF_OVERRIDE`, or helper-selection overrides.
No new command-line override authority is introduced by this assessment.

The 4.9.3 implementation uses `WalkDir`, which skips a symlink used as the
drop-in directory itself. It also checks optional main files with `Stat` before
reading them; a failed check skips that file, while a selected file disappearing
during reading is an error. The 5.8.4 implementation follows directory symlinks
and ignores missing optional files during reading. Denied checks remain visible
as missing evidence even when upstream selection skips them.

Other or unmeasured versions receive candidate-source observations, with
`selection_complete: false` and no merged engine result. Individual successfully
parsed files remain visible. Numeric upstream versions may retain vendor
suffixes, but distribution patches to the selection algorithm require separate
qualification. capagent does not extrapolate a rule across a major or minor
version range.

These rules are pinned to upstream source, including the corresponding helper
selection implementations: [4.9.3 source selection](https://github.com/containers/podman/blob/v4.9.3/vendor/github.com/containers/common/pkg/config/new.go),
[5.8.4 source selection](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/common/pkg/config/new.go),
[OCI selection](https://github.com/containers/podman/blob/v5.8.4/libpod/oci_conmon_common.go), and
[conmon selection](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/common/pkg/config/config.go).

## Parsed fields and provenance

The engine projection includes `runtime`, `runtimes`, `conmon_path`,
`helper_binaries_dir`, `cgroup_manager`, deprecated `runtime_path`, and redacted
engine-environment cardinality. Other engine fields are counted as unprojected;
their names and values are omitted. Network, storage, registry, and image-policy
assessment have separate delivery owners. Successful engine projection is not
validation of every setting accepted by Podman.

Scalars replace earlier values. Entries in `engine.runtimes` merge by runtime
name, with each ordinary path array replacing that entry. Attributed arrays,
such as `conmon_path`, normally replace earlier arrays. An `{append=true}` item
changes subsequent assignments to append until an explicit `{append=false}`
resets that behavior. Its position within the array does not matter. Appending
to an unmeasured built-in default retains `inherited_default: true`; those
unknown earlier entries cannot be discarded when choosing a helper.

Every source records its path, order, selection, status, and SHA-256 of a
complete read when available. `applied` distinguishes a successfully merged
source from a later source inspected after a preceding failure. Per-file
projections, scalar source IDs, and per-element array origins explain which
assignment produced a value. A digest identifies the bytes inspected, not a
pinned inode or an atomic transaction with Podman inspection.

Absent, denied, malformed, invalid, unsupported, and incomplete sources remain
distinct. Partial reads are neither parsed nor hashed as complete documents.
Diagnostics contain fixed codes, source references, and recognized field names;
they never include raw files, decoder messages, arbitrary keys, or environment
values. Engine environment entries and platform-specific runtime overrides
are visible limitations: their unmodeled effects prevent configured helper
selection from being asserted. Runtime-reported effective paths remain separate
evidence.

## Capabilities and proof limits

| Capability | Meaning |
| --- | --- |
| `runtime.podman.config.engine.parsed` | The qualified source set and recognized engine fields parsed completely; missing files leave built-in defaults unmeasured |
| `runtime.podman.cgroup_manager.systemd` | The configured or runtime-effective cgroup-manager choice is `systemd` |
| `runtime.podman.oci_runtime.executable` | The selected OCI path has suitable metadata and executable access for the target |
| `runtime.podman.conmon.executable` | The selected conmon path has suitable metadata and executable access for the target |

The last two IDs extend the [prerequisite contract](podman-prerequisites.md).
An explicit absolute OCI runtime or a fully configured runtime path list can
provide selection evidence. An unmeasured default runtime map cannot. Configured
OCI lists choose the first regular file; conmon lists skip directories. A
present, non-executable configured choice is not silently replaced by a later
candidate. The reviewed `/usr/bin:/bin` PATH is the final fallback. At most 16
metadata candidates are checked per configured helper; exhausted bounds leave
selection unknown. No helper is executed by these checks.

Runtime-effective evidence outranks configuration. For example, an effective
`cgroupfs` report makes the systemd-choice capability `unsupported`, even if a
file says `systemd`. A completely observed malformed selected file makes the
engine-parsing capability `misconfigured`; denied, unsupported, incomplete,
or unqualified selection leaves it `unknown`.

Selecting systemd as the cgroup manager does not prove manager availability or
controller delegation. Combine it with the relevant cgroup and session IDs in
the prerequisite guide. Rootful services use the system manager; rootless
Quadlet uses the target user's manager and runtime directory. The separate
linger observation addresses persistence after logout.

The bounded JSON requirement language can exercise these IDs through the
existing offline assessment interface. The live custom-requirement interface
is delivered separately. From a repository checkout, these examples produce
`SATISFIED` (exit 0), `UNSATISFIED` (exit 1), and `INDETERMINATE` (exit 2):

```sh
capagent --fixture testdata/fixtures/v1/engine-rootless --json
capagent --fixture testdata/fixtures/v1/engine-malformed --json
capagent --fixture testdata/fixtures/v1/engine-denied --json
```

`engine-rootful` provides the corresponding rootful example. Each fixture's
`requirement` member contains its complete JSON requirement over delivered IDs.
Fixtures are explicitly synthetic and do not assert deployment success.

Sanitized native measurements in `testdata/podman/engine/` cover Nobara Podman
5.8.4 rootless and Ubuntu Podman 4.9.3 rootless/rootful on amd64. Their replay
uses the real info parser and capability mappings, retaining typed configuration
projections and source digests. Raw configuration contents are not captured;
the info JSON is reconstructed from published fields. These captures establish
inspection and mapping behavior for those builds. They do not qualify other
distribution patches, architectures, or workload operations. CI separately
checks delegated contexts, passive traces and packaged launchers; arm64 artifact
verification is static rather than native execution.

Storage has its own [selection and replacement rules](podman-storage-config.md),
with shared source identity and target environment binding.

## Parsing and resource contract

The pure-Go compile-time parser dependency is `go-toml/v2` v2.4.3, confined to
`internal/config`. There is no external parser executable or runtime dependency.
It parses full TOML syntax, followed by explicit rejection of TOML 1.1 additions
that the qualified Podman builds do not accept: newer string escapes, omitted
time seconds, and inline-table trailing commas, comments, or newlines outside
values. TOML 1.0 multiline values remain valid.

Each document is limited to 64 KiB, 4096 syntax nodes and 32 levels of key/value
depth before typed decoding. The underlying parser separately bounds expression
construction at 10000 nested arrays/inline tables. An assessment reads at most
64 configuration files and accepts at most 1 MiB of aggregate file data. A
single declared list has at most 64 entries; accumulated values retain the
aggregate bound. All filesystem operations use the platform confinement and
read limits. Limit exhaustion is explicit missing evidence, never a successful
empty configuration.
