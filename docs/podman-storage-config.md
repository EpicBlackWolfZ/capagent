# Podman storage configuration assessment

Local Podman assessment reads `storage.conf` and measures selected storage paths
under the same target identity and HOME/XDG policy as runtime inspection.
Configuration and kernel permission checks describe prerequisites. They do not
verify layer creation, mounts, image extraction, quotas, labeling, or storage
writes. `--active` permits the existing bounded Podman inspection; the storage
collector itself does not execute helpers or create directories.

## Selection and replacement

The source profiles cover upstream Podman 4.9.3 and 5.8.4. Other or unmeasured
versions retain candidate observations with unknown selection. The profiles use
these paths:

- Vendor: `/usr/share/containers/storage.conf`.
- System override: `/etc/containers/storage.conf`.
- Target user: `$XDG_CONFIG_HOME/containers/storage.conf`, or
  `$HOME/.config/containers/storage.conf` when XDG_CONFIG_HOME is absent.

Storage does not use the engine's drop-in or attributed-array merge algorithm.
Each selected file replaces storage options. Selection has two phases: initial
default loading, then the final rootful or per-user file. A present file under
an explicitly supplied XDG_CONFIG_HOME can supply the initial defaults; otherwise
the system file replaces the vendor file. The final rootless file is the target
user file. Rootful final selection checks the system file, then the vendor path,
separately from initial XDG default loading.

Before a rootless user file replaces options, upstream converts system defaults
into user storage: graph/run roots use the target's data/runtime directories or
a configured rootless storage path. Only the applicable driver/default subset
survives that conversion. System mount-program and additional-store settings
are not generically copied into rootless defaults. Explicit user-file values
then replace those options.

Post-reload fallback differs by version. Podman 5.8.4 restores omitted graph/run
roots for both rootless and rootful targets. Podman 4.9.3 does so only for rootless
targets: a rootful selected file omitting required roots is a configuration
problem. A present XDG baseline and a different final system file can therefore
affect fallback roots in 5.8.4 without merging their other fields.

Sources record their `phase`, selection, application, status and complete-read
SHA-256. A `selection` record with status `available` means a metadata check found
that candidate; it is not a successful parse. Separate records preserve repeated
selection/read phases. Per-file projections retain configured spellings; the
selected projection records resulting roots and options. Default root values
refer to the qualified version observation and pinned source rules rather than
a nonexistent assignment in a file. These measurements are not an atomic
transaction with Podman inspection.

The rules are pinned to [4.9.3 storage loading](https://github.com/containers/podman/blob/v4.9.3/vendor/github.com/containers/storage/types/options.go),
[4.9.3 final selection](https://github.com/containers/podman/blob/v4.9.3/vendor/github.com/containers/storage/types/utils.go),
[5.8.4 storage loading](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/storage/types/options.go),
and [5.8.4 final selection](https://github.com/containers/podman/blob/v5.8.4/vendor/go.podman.io/storage/types/utils.go).
Distribution changes to these algorithms need separate qualification.

## Fields and limits

The bounded projection covers driver and driver priority, graph/run roots,
rootless storage path, separate writable image-store path, transient storage,
additional read-only image/layer stores, and relevant overlay/vfs options.
Recognized options include mount program, ignore-chown behavior, skip-mount-home,
force mask and overlay composefs selection in 5.8.4. Podman 4.9.3 treats
composefs as an unprojected field; the adapter does not validate it as a newer
recognized option. Driver-specific options retain their
own provenance and take precedence over applicable general options. Additional
layer-store `:ref` syntax is retained in configuration; metadata checks address
the directory itself. Other options are counted as unprojected, with names and
values omitted. Parsing success does not validate every driver option.

The collector reuses the [bounded TOML parser](podman-engine-config.md#parsing-and-resource-contract).
Storage arrays replace earlier values; `{append=true}` is not accepted. Path
expansion uses only the declared environment, including the upstream literal
`$UID` replacement. There is no ambient CONTAINERS_STORAGE_CONF, STORAGE_DRIVER
or STORAGE_OPTS inheritance. Unmodeled `engine.env` effects prevent configured
storage selection from being asserted; runtime-effective fields remain separate
evidence.

Writable-root spellings containing `..` are unsupported because cleaning them
before resolving symlinks can select a different inode. They remain unknown.
The collector does not claim to reproduce upstream symlink-canonicalized path
strings; each supported path is resolved by the kernel within the scoped reader.
Mount-program metadata requires a canonical absolute path. Additional read-only
store paths use upstream's lexical cleaning behavior.

A missing explicit rootless runtime directory leaves that default unmeasured;
capagent does not create or select a temporary fallback directory. Each path
probe measures at most 32 paths and checks at most 32 ancestors per path. Bounds,
cancellation, denied traversal and unsupported kernel queries preserve missing
evidence. No mode-bit heuristic replaces the target's kernel access result.

## Capabilities and evidence

| Capability | Proof level |
| --- | --- |
| `runtime.podman.config.storage.parsed` | Qualified selected sources and recognized settings are valid; other options remain unvalidated |
| `runtime.podman.storage.overlay` | Configured or runtime-reported driver choice is overlay |
| `runtime.podman.storage.roots.accessible` | Primary graph/run roots exist and allow target write/search access on a filesystem not reported read-only |
| `runtime.podman.storage.image_store.accessible` | Any explicitly configured separate writable image-store path has directory access |
| `runtime.podman.storage.additional_stores.accessible` | Configured additional read-only stores exist and allow target read/search access |
| `runtime.podman.storage.mount_program.executable` | The declared mount-program path has suitable executable metadata |
| `runtime.podman.storage.kernel.overlay` | Existing host facts show overlay currently registered by the kernel |
| `runtime.podman.storage.kernel.fuse` | Existing host facts show FUSE currently registered by the kernel |

Runtime-reported driver and primary roots outrank conflicting configuration in
the same scope. Live host filesystem registration remains live evidence. Path
access measurements retain the precedence of their selection source: measuring
an inode does not promote a configured path to runtime-effective selection.
Optional configured image-store access stays separate when runtime inspection
does not report that path. Podman's `store.imageStore` count object is not a
writable image-store path.

Existing suitable directories support the access prerequisite. Wrong file types,
denied kernel access and read-only writable roots are misconfigured. A missing
writable root remains unknown: an accessible existing ancestor cannot prove
creation of the requested directory. Missing required read-only stores are
misconfigured. Denied traversal, incomplete reads and unmeasured access remain
unknown. A malformed selected file or missing version-required root is a
configuration problem, even when separate runtime evidence is usable. A separate
image-store path equal to the expanded graph root is also misconfigured.

A reported mount program comes from typed graph options; an omitted field does
not prove that no helper is used. A missing custom path cannot be repaired by an
installed inventory candidate elsewhere. Neither a helper's executable access
nor filesystem registration proves successful helper execution or mounting.
FUSE device opening, device-cgroup policy, filesystem-specific compatibility,
rootless mount behavior and storage writes remain unverified. Unregistered
filesystems may become available after module loading; the kernel predicates
describe the current registration snapshot.


## Examples and qualification

The existing JSON requirement language evaluates these prerequisites in offline
assessment fixtures. These commands respectively demonstrate SATISFIED/0,
UNSATISFIED/1 and INDETERMINATE/2:

```sh
capagent --fixture testdata/fixtures/v1/storage-rootless --json
capagent --fixture testdata/fixtures/v1/storage-helper-missing --json
capagent --fixture testdata/fixtures/v1/storage-helper-denied --json
```

`storage-rootful` supplies a rootful counterpart. The fixtures contain complete
requirements over the eight delivered storage IDs. Separate regressions exercise
source replacement, runtime/configuration disagreement, read-only mounts,
missing writable roots, missing additional stores and legacy required roots.
They are synthetic prerequisites, not deployment certifications.

Sanitized native storage measurements in `testdata/podman/storage/` retain typed
source projections, path metadata and runtime reports. The Nobara Podman 5.8.4
rootless capture executes natively on amd64. Replay runs the real Podman info
parser and capability mappings. It reconstructs info JSON from typed fields and
omits raw configuration and unprojected graph options. The supported native capture matrix for these prerequisite meanings is:

| Host / Podman | Target | Execution |
| --- | --- | --- |
| Nobara / 5.8.4 | rootless UID 1000 | native amd64 |
| Ubuntu 24.04 / 4.9.3 | rootless UID 1001 | native amd64 |
| Ubuntu 24.04 / 4.9.3 | rootful UID 0 | native amd64 |

The Ubuntu captures come from [CI run 34770862020](https://github.com/EpicBlackWolfZ/capagent/actions/runs/34770862020),
which also validates delegated identities, passive native traces, bounded
user-manager queries and packaged launchers. All three captures report overlay
and accessible primary roots. None reports a selected mount program, so that
predicate remains unknown. Synthetic fixtures provide the positive, missing and
denied declared-helper cases. Arm64 artifacts receive static checks; this matrix
contains no native arm64 storage execution. These source/build snapshots do not
qualify every distribution patch or storage backend.
