# Podman helpers and Quadlet prerequisites

Run the assessment as the deployment account, or select that account explicitly:

```sh
capagent --runtime podman --context=user:alice --json --pretty
capagent --runtime podman --context=user:alice --active --json --pretty
capagent --runtime podman --context=uid:0 --active --json --pretty
```

Delegation requires a root launcher. The worker measures files and access under the target's UID, GID, supplementary groups and retained namespaces. An unresolved or unauthorized target produces no substitute measurements from the launcher. See [execution contexts](execution-context.md).

Passive collection inventories helper and generator metadata. It never invokes helpers or generators, creates units, enables services, starts containers, or tests networking, storage or pulls. `--active` retains the existing bounded Podman version/info and user-manager query policies. Podman's own inspection can invoke helpers and initialize runtime state; see [inspection](podman-discovery.md).

## Executable evidence

Each `evaluation.observations[].executable` records a role, source, optional source-observation ID, selected Podman path, selected helper path where known, and candidate metadata. Candidates retain nullable presence, regular-file type, permission bits, nullable ownership, native device/inode identity, and a separate target execution-access result. Memory fixtures leave native device/inode identity null. Kernel-confined symlinks are allowed.

`executable: true` means the target's kernel access check succeeded on a regular file with execute bits. It does not prove that a loader, shared libraries, architecture, configuration, or helper invocation will work. Metadata and access are snapshots from separate bounded operations; they do not pin a future execution. A permission failure, inaccessible search location, unsupported access syscall, cancellation, or incomplete source remains unknown. Existing files with unsuitable metadata or denied execution access are misconfigured prerequisites.

The trusted candidate inventory covers crun, runc, conmon, Netavark, Aardvark DNS, pasta, slirp4netns and fuse-overlayfs in these directories:

```text
/usr/local/bin  /usr/bin  /bin
/usr/local/libexec/podman  /usr/libexec/podman
/usr/local/lib/podman  /usr/lib/podman
```

This is a bounded inventory of common packaging locations, not Podman's configured helper selection algorithm. It does not search ambient PATH. A helper outside these locations requires a reported or configured selected path. A successful `podman info` can provide OCI runtime, conmon, Netavark, Aardvark, pasta and slirp4netns paths. Each reported path receives its own scoped metadata observation linked to that info observation. A missing selected path is unknown; an installed candidate elsewhere cannot repair a missing selected executable. Missing selected files are misconfigured. Qualified [engine configuration](podman-engine-config.md) can also supply OCI runtime and conmon selection, including ordered candidates and source provenance. Qualified [network configuration](podman-network-config.md) also supplies Netavark, Aardvark, pasta and slirp4netns search plans tied to the same engine sources. Runtime-reported paths take precedence. Unmeasured built-in defaults and unsupported selection effects remain unknown. Runtime-selected evidence keeps runtime precedence when combined with live file metadata, so measuring access does not promote a selection claim to live authority.

| Capability IDs | Meaning of supported |
|---|---|
| `runtime.podman.helper.ROLE.installed` | A regular candidate file exists within the bounded inventory. |
| `runtime.podman.helper.ROLE.executable` | A candidate also has executable bits and target execution access. |
| `runtime.podman.ROLE.executable` | The runtime-reported selected path has suitable metadata and target access. |

Inventory roles are `crun`, `runc`, `conmon`, `netavark`, `aardvark_dns`, `pasta`, `slirp4netns`, and `fuse_overlayfs`. Selected roles are `oci_runtime`, `conmon`, `netavark`, `aardvark_dns`, `pasta`, and `slirp4netns`. The pre-existing `runtime.podman.netavark` retains its narrower backend-plus-permission-bits meaning; it does not become proof of DNS or network operation.

## Quadlet evidence

For rootless targets the generator name is `podman-user-generator` in `user-generators`; rootful targets use `podman-system-generator` in `system-generators`. The standard search precedence is:

```text
/run/systemd/…
/etc/systemd/…
/usr/local/lib/systemd/…
/usr/lib/systemd/…
```

The first existing candidate wins, including an unsuitable file. An earlier denied lookup prevents selection of a later vendor generator. Empty regular files and `/dev/null` masks disable the generator, including null-device symlink chains. This follows the standard [systemd v255 generator contract](https://github.com/systemd/systemd/blob/v255/man/systemd.generator.xml). Custom compiled search directories and alternative generator names are outside the inventory. Presence does not establish a working generator or support for a particular unit directive.

The `quadlet` observation lists metadata for candidate input roots from the pinned [Podman v5.8.4 implementation](https://github.com/podman-container-tools/podman/blob/v5.8.4/pkg/systemd/quadlet/unitdirs.go). Rootless roots are `$XDG_RUNTIME_DIR/containers/systemd` when explicitly set, `$XDG_CONFIG_HOME/containers/systemd` (otherwise `$HOME/.config/containers/systemd`), `/etc/containers/systemd/users`, then `/etc/containers/systemd/users/UID`. Rootful roots are `/run/containers/systemd`, `/etc/containers/systemd`, and `/usr/share/containers/systemd`.

These are documented candidate roots, not a claim that every historical generator consumes them. Collection does not recurse through units, resolve unit overrides, inspect unit contents, or implement generator dry-run behavior. `QUADLET_UNIT_DIRS` and manager-specific environment changes are not inherited. The target's single explicit HOME/XDG capture selects both these roots and the Podman inspection environment; delegated workers use target HOME and runtime-directory values. A future systemd manager invocation can have a different environment, which must be assessed separately.

| Capability | Prerequisite and limits |
|---|---|
| `runtime.podman.quadlet.generator` | Selected generator is unmasked and has suitable executable metadata/access. Generation is unverified. |
| `runtime.podman.cgroup_v2` | Unified cgroup v2, required by [Quadlet v5.8.4](https://github.com/podman-container-tools/podman/blob/v5.8.4/docs/source/markdown/podman-systemd.unit.5.md). Live host evidence outranks runtime information. Controller delegation is unverified. |
| `runtime.podman.quadlet.manager` | Rootless: successful bounded target user-manager query. Rootful: observed running system manager. No unit activation is verified. |
| `runtime.podman.quadlet.runtime_directory` | Rootless: existing directory owned by the target UID with mode 0700. It does not prove a login session. |
| `runtime.podman.quadlet.linger` | Rootless: observed regular linger marker. Relevant to startup without login; neither boot behavior nor running services are verified. |

User runtime-directory and linger predicates are unsupported for rootful system services and should be omitted from rootful requirements. Disabled linger does not invalidate an already accessible interactive user manager. A socket alone cannot satisfy the manager predicate; passive reports keep accessibility unknown. Completed failed queries yield unavailable, while cancelled, timed-out, truncated or incomplete queries remain unknown.

## Bounded requirement examples

The existing JSON requirement language can express the initial rootless prerequisite set:

```json
{
  "all": [
    {"capability": "runtime.podman"},
    {"capability": "runtime.podman.info"},
    {"capability": "runtime.podman.oci_runtime.executable"},
    {"capability": "runtime.podman.conmon.executable"},
    {"capability": "runtime.podman.quadlet.generator"},
    {"capability": "runtime.podman.cgroup_v2"},
    {"capability": "runtime.podman.quadlet.manager"},
    {"capability": "runtime.podman.quadlet.runtime_directory"}
  ]
}
```

For rootful services, omit `runtime.podman.quadlet.runtime_directory`. For rootless startup without login, additionally require `runtime.podman.quadlet.linger`. These examples assess only delivered prerequisites; they do not include all storage, network, image-policy, controller-delegation or workload conditions. No operational Quadlet capability is published from these prerequisites.

The `assessment-rootless` and `assessment-rootful` v1 fixtures embed these requirements. Live custom requirement input is delivered separately; the current default live requirement still checks CLI version and, with `--active`, effective info.

```sh
capagent --fixture testdata/fixtures/v1/assessment-rootless --json
capagent --fixture testdata/fixtures/v1/assessment-generator-absent --json
capagent --fixture testdata/fixtures/v1/assessment-helper-denied --json
```

These produce SATISFIED/0, UNSATISFIED/1 and INDETERMINATE/2 respectively. Generator masks and invalid runtime directories are definite configuration failures. Denied metadata and deferred manager queries cannot establish a positive or negative result. Unavailable predicates remain indeterminate under the existing requirement semantics.

## Native capture and replay

`testdata/podman/prerequisites` contains sanitized native typed measurements. Their provenance identifies the host, Podman version and execution mode. The capture exporter reconstructs an info JSON subset from published typed fields and restores shared fact scope; it does not preserve arbitrary command output or executable contents. Replay tests re-run the info parser and compare all helper/Quadlet prerequisite states with the native sample. Synthetic `assessment-*` fixtures separately exercise collection, source selection, failures, report schema and requirement exits.

```sh
capagent --runtime podman --active --json > .work/prerequisites-report.json
python3 -B scripts/capture-podman-prerequisites.py \
  --report .work/prerequisites-report.json --output .work/prerequisites-capture.json \
  --description 'Native architecture, OS, Podman version, and selected target'
```

Review capture provenance and redaction before committing. Home account names are anonymized automatically; custom installation paths, scope selectors and other identifiers may require additional review. Passive traces, native delegated checks and packaged-artifact validation are described in [testing](testing.md). A static build for an architecture is not native execution evidence.

The prerequisite sample matrix currently contains native amd64 execution:

| OS / Podman | Target and observed result |
|---|---|
| Nobara/Fedora / 5.8.4 | UID 1000 rootless: selected OCI/conmon and generator access, cgroup v2, runtime directory and user manager supported; linger disabled. |
| Ubuntu 24.04 / 4.9.3 | UID 0 rootful: selected OCI/conmon and generator access, cgroup v2 and system manager supported. |
| Ubuntu 24.04 / 4.9.3 | UID 1001 rootless with fresh isolated runtime directory: helper/generator metadata supported; user manager unavailable in that directory despite a linger marker. |

The Ubuntu samples come from [native CI run 34762163053](https://github.com/EpicBlackWolfZ/capagent/actions/runs/34762163053). That run also passed delegated username/numeric-UID context traces, bounded user-manager queries, and full/minimal packaged launchers. These samples qualify the documented prerequisite meanings, not unit generation, networking, storage, image pulls or the complete assessment checkpoint. The v5.8.4 candidate input-root reference does not claim that the 4.9.3 generator consumes identical roots. Other architectures receive separate static checks until native evidence is recorded.
