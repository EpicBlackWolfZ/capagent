# Live Podman requirements and explanations

Use `--requirement FILE` to evaluate the existing bounded JSON requirement
language against the selected live Podman target. `--explain` adds a human
explanation on stderr; JSON remains on stdout with the same schema and exit.

```sh
capagent --runtime podman --active --json --explain \
  --requirement examples/requirements/rootless-quadlet.json > report.json
```

Run this as the intended rootless user. A root caller can select a local target
with `--context user:alice` or `--context uid:1000`. Run the
[`rootful-quadlet.json`](../examples/requirements/rootful-quadlet.json) example
as root for system services. Each assessment binds the entire requirement to
one target, runtime, endpoint and namespace context. Evidence from the caller
cannot fill a delegated target's missing measurements.

`--active` permits the bounded user-manager query and local Podman version/info
inspection, including Podman's startup writes. It does not run workload,
networking, DNS, storage-write or image-pull experiments. A custom requirement
does not grant additional command authority. Without `--active`, unmeasured
inspection and user-manager prerequisites remain indeterminate.

## Input and delegation

The input is one JSON node containing exactly one of `capability`, `all`, `any`
or `not`; see [the language and truth tables](fixture-evaluation.md).
The limits remain 64 KiB, 32 requirement-node levels and 512 nodes. Duplicate
or unknown members, invalid identifiers and invalid operator combinations are
rejected. Unknown but syntactically valid capability IDs remain indeterminate.
There is no YAML adapter, stdin convention or named-profile mechanism.

The caller reads the selected file once before target delegation. Its parent
directory is the explicit document root, and the final subpath uses the same
kernel confinement as other reads. A symlink can resolve within that root;
escaping links cannot read a neighboring document. Missing, denied, oversized
or incomplete reads produce no assessment. The worker receives the selected
document, checks its bounds and parses it again after credential setup. It
never reopens the caller's pathname as the target user. Requirement selection
does not alter the target HOME/XDG policy used by configuration and inspection.

`--requirement` requires `--runtime podman` and cannot override a fixture's
embedded requirement. The existing default remains CLI availability in passive
mode and `all(runtime.podman, runtime.podman.info)` in active mode.

## Outcomes and exits

| Result | Exit | Meaning |
| --- | --- | --- |
| `SATISFIED` | 0 | The selected evidence satisfies this particular requirement |
| `UNSATISFIED` | 1 | A definite failure exists under the requirement's Boolean logic |
| `INDETERMINATE` | 2 | Missing, unavailable, incomplete or conflicting evidence prevents a definite result |
| Invalid options or requirement syntax | 64 | No assessment was emitted |
| Document read, collection or output failure | 70 | Assessment delivery failed |

The five capability states stay distinct: supported predicates can satisfy a
requirement; unsupported and misconfigured predicates fail it; unknown and
unavailable predicates remain indeterminate. Negation preserves indeterminacy.

The delivered [`inspection.json`](../examples/requirements/inspection.json)
document provides a small reproducible demonstration:

```sh
# Exit 0 when local version/info inspection completes successfully.
capagent --runtime podman --active --requirement examples/requirements/inspection.json --json

# Exit 1 when this explicitly selected executable is absent.
capagent --runtime podman --podman-path /nonexistent/capagent-example-podman \
  --requirement examples/requirements/inspection.json --json

# Exit 2 when Podman is present but its version/info inspection is deferred.
capagent --runtime podman --requirement examples/requirements/inspection.json --json
```

These outcomes describe observed conditions, not an assumption that every host
has a working local Podman installation. Fixture examples remain available for
deterministic offline replay.

## Reading the explanation

The explanation shows the selected target, the requirement tree and its verdict.
Failed or unknown requested predicates appear first, followed by supported
predicates. Evidence is labeled selected, superseded or excluded and retains its
rank and observation references. Configuration details include source order,
selection/application status, complete-read digests, field provenance and
redacted invalid or unmodeled settings. Helper candidates show observed presence
and executable access; user-manager and storage-directory measurements retain
their missing values.

Shared observations are described once and referenced afterward. Other
misconfigured capabilities and incomplete configuration projections appear as
additional findings outside the chosen requirement. They do not change that
requirement's result. In particular, successful inspection can coexist with a
configuration problem.

Explanations are bounded to 64 KiB with an explicit truncation marker. Individual
text fields are bounded and terminal controls escaped. Raw diagnostic messages,
environment values and opaque helper arguments are excluded. The complete
structured report remains the source for further inspection. `--debug` can still
add diagnostic codes on stderr.

## Example prerequisites and limits

The rootless and rootful examples use only delivered IDs. They combine
OCI/conmon and Quadlet metadata, cgroup v2, the relevant manager, systemd
cgroup-manager selection, engine/storage/network/DNS projections, primary
storage-root access, Netavark/Aardvark executables and the selected rootless
pasta or slirp helper. The rootful requirement omits rootless session predicates.
The separate
[`rootless-quadlet-without-login.json`](../examples/requirements/rootless-quadlet-without-login.json)
additionally requires the observed linger marker.

These are explicit prerequisite examples, not exhaustive deployment profiles.
They do not establish controller delegation, generator directive compatibility,
unit activation, boot persistence, working networking/DNS, storage writes or
image access. Add workload-specific driver and helper requirements where needed;
for example, a deployment that selects a FUSE mount program needs
`runtime.podman.storage.mount_program.executable` and
`runtime.podman.storage.kernel.fuse`. A satisfied file/configuration requirement
does not prove that a helper works. Registry/image-policy coverage is delivered
separately.

See [Quadlet prerequisites](podman-prerequisites.md),
[engine selection](podman-engine-config.md), [storage](podman-storage-config.md)
and [network/DNS](podman-network-config.md) for exact evidence thresholds and
the qualified version/source contracts. Future systemd service environments can
differ from the explicit environment assessed here and require separate review.

For unusually large requirement trees, JSON output may omit repeated diagnostic
aggregates at intermediate Boolean nodes. The complete tree, outcomes, scopes,
leaf reasons and diagnostics, and root diagnostic aggregate remain available.
The `requirement_diagnostics_compacted` diagnostic identifies this representation;
ordinary small reports retain their existing form. This prevents diagnostic
repetition from exhausting the delegated worker's bounded response transport.
