# Fixture evaluation

The first evaluation path replays offline Podman-shaped inputs through the real parser, probe scheduler, evidence resolver, capability evaluator, requirement evaluator and JSON serializer. It does not inspect the machine running capagent. The included scenarios are synthetic, and `evaluation.mode` is always `fixture`.

Build and run a scenario from the repository root:

```sh
go build -o bin/capagent ./cmd/capagent
./bin/capagent --fixture testdata/fixtures/v1/supported --json --pretty
```

`--json` is the default format. `--pretty` adds indentation and a trailing newline. `--debug` writes diagnostic codes to stderr; stdout contains only the report. `--help` and `--version` remain available. A missing fixture, unknown command or `--active` invocation fails visibly. [Passive local discovery](podman-discovery.md) is available separately; live runtime execution remains deferred; no command from a fixture can reach an OS command runner.

| Exit code | Meaning |
|---|---|
| 0 | Requirement `SATISFIED` |
| 1 | Requirement `UNSATISFIED` |
| 2 | Requirement `INDETERMINATE` |
| 64 | Invalid invocation or malformed fixture document |
| 70 | Input read, service setup, evaluation-contract, cleanup or output failure |

A failed or interrupted probe can still produce a report and exit 1 or 2 according to the requirement. Partial measurements remain in its trace. An interruption before the fixture can be read is an input failure. Diagnostic output contains selected codes and fixed messages, never raw command errors, stdout, stderr or environment values.

## First capability

`runtime.podman.netavark` describes the selected Netavark backend and helper file metadata. Its `supported` result means the runtime reports `netavark` and the referenced helper is a regular file with at least one execute bit. This does not prove the target user can execute the helper, that its dynamic dependencies are installed, or that container networking or DNS works. The resulting requirement verdict has this same narrow meaning.

| Observation | Capability | Requirement predicate |
|---|---|---|
| Netavark selected; helper metadata matches | `supported`, derived confidence | `SATISFIED` |
| CNI selected | `unsupported`, derived confidence | `UNSATISFIED` |
| Netavark selected; helper absent or metadata unsuitable | `misconfigured`, derived confidence | `UNSATISFIED` |
| Runtime inspection fails without partial-output uncertainty | `unavailable`, derived confidence | `INDETERMINATE` |
| Missing fields, unknown backend, permission uncertainty, truncation or timeout | `unknown`, unknown confidence | `INDETERMINATE` |

The original five info directories under [`testdata/fixtures/v1`](../testdata/fixtures/v1) exercise these outcomes. Version strings in them are synthetic parser input, not claims of validation on those Podman releases.

## Fixture document

Each scenario contains `fixture.json` and its reviewed `expected.json` report. The optional `probe` field selects `info` (the default) or `version`. A version fixture supplies exactly one literal `--version` command and executable metadata; all commands still use fake services. The document contains:

- `schema_version: 1`, a fixed `run_id` and RFC 3339 `timestamp`.
- Explicit `provenance.kind` (`synthetic` or `captured`) and a description. Captures must be sanitized before committing; derived negative scenarios must be labeled synthetic.
- One context ID binding current and target identities, supplementary-group presence and namespace observations. A null identity is unobserved; UID 0 explicitly means root. Fixtures currently select `runtime: podman` and `endpoint: local`.
- Filesystem entries with relative paths, kind, Go `os.FileMode` bits, optional UID/GID together, content or symlink target. Parent directories must be listed explicitly. `error` entries require a failure category. Regular permission bits use familiar octal values, represented as decimal JSON integers (0755 is 493).
- Exactly one command specification and result: absolute executable, literal argument list, explicit environment overrides, directory, timeout in milliseconds, stdout/stderr, exit code and truncation flags. Defaults reuse the platform policy: `PATH=/usr/bin:/bin`, `LC_ALL=C`, directory `/`, and the runner's bounded timeout. No ambient environment is captured. Failure categories are `unavailable`, `not_found`, `permission`, `timeout` and `cancelled`.
- An embedded `requirement` document.

The loader caps the document at 2 MiB, files at 4,096 and explicit command timeouts at 30 seconds. It reads `fixture.json` through the kernel-confined scoped reader and constructs the existing memory reader and fake runner. Absolute symlink behavior and special filesystem cases retain the documented [OS/memory differences](security.md#filesystem-authority); the memory reader is not a Linux emulator. The existing platform conformance matrix remains shared, and an integration test replays all five scenarios through both filesystem providers and compares observations.

## Requirements and scope

The JSON AST supports exactly one of `capability`, `all`, `any` or `not` per node:

```json
{
  "all": [
    {"capability": "runtime.podman.netavark"},
    {"not": {"capability": "runtime.example_unknown"}}
  ]
}
```

An unevaluated capability is indeterminate, including under `not`. Empty `all` is satisfied and empty `any` is unsatisfied. The existing three-valued truth tables govern composition. Parsing rejects unknown operators, duplicate members and malformed branches even if Boolean short-circuiting could skip them. Bounds are 64 KiB, depth 32 and 512 AST nodes; cyclic programmatic trees are invalid. The pure evaluator accepts at most 128 candidates with 4,096 capabilities and 4,096 evidence records each.

Each candidate carries a run ID, context ID, runtime and endpoint. Context IDs uniquely bind the current/target identity and namespaces. The entire requirement is evaluated within each candidate before candidate verdicts are ORed. Capabilities from different identities, namespaces, runtimes or endpoints cannot be pooled to satisfy an `all` requirement.

Evidence resolution applies live > runtime > config > knowledge > heuristic only to the same claim and scope. Freshness requires the same run and a timestamp no later than evaluation time; there is no cross-run cache. Facts cannot postdate their observations, and observations or evidence dependencies cannot postdate the dependent claim. Complete records cannot rely on partial records. Incomplete, stale and incompatible evidence is excluded with diagnostics; lower-ranked usable evidence remains eligible. Equally ranked conflicting claims resolve to unknown. The report retains winning references, superseded references, source ranks, completeness and timestamps. It omits raw fact values and command data.

## Consumer and pre-release contract

Capabilities are a flat map. Use the full dotted key:

```python
report["capabilities"]["runtime.podman.netavark"]["state"]
```

The executable [`examples/check-report.py`](../examples/check-report.py) accepts a report on stdin and fails closed unless the Netavark configuration is supported with derived/verified confidence, evidence references and a satisfied requirement:

```sh
set -o pipefail
./bin/capagent --fixture testdata/fixtures/v1/supported --json |
  python3 examples/check-report.py
```

This is an offline consumer smoke test, not deployment authorization for the current host. Missing, unsupported, misconfigured, unavailable, unknown and malformed input all fail closed. Integration tests execute the binary and consumer, compare expected JSON and validate it against the embedded JSON Schema.

Schema v1 remains pre-release. This slice corrects UID/GID to nullable integers bounded by 0..4,294,967,295, makes unobserved booleans null, adds the `context` capability namespace and validates canonical map keys. An explicit root target is preserved. Context, host and runtime sections include completeness; empty host strings and `unknown` cgroup values carry no negative capability claim. `evaluation` adds scope, identity presence, provenance, the reference trace and the requirement result. Unknown additive report properties are accepted and discarded deterministically by the DTO decoder; fixture and requirement inputs are intentionally stricter.

`Report.Validate` checks application report invariants, not every JSON Schema rule. The wire-contract test suite performs full schema validation. Domain records and output DTOs remain separate, and the final compatibility freeze stays with M19.

Version replay scenarios additionally cover successful output, a nonzero command, malformed output and timeout. Their descriptions identify captured command text and synthetic context or failure data; fixture replay never becomes live evidence. See [Podman discovery](podman-discovery.md#version-fixture-replay).
