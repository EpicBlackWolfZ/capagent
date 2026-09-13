# Foundation verification and fuzz testing

The M1.1 regression suites cover implemented platform and scheduler contracts.
They run on the supported Linux 5.6+ baseline with working `openat2`, using Go
1.27.1 and Python 3's standard library for reporting. Python is development tooling;
the capagent binary gains no runtime dependency or fault-injection interface.

## Run and replay

```bash
go test -race ./...          # Fixed regressions and the short seeded campaign
make hardening-regressions  # Selected regressions plus a verified evidence report
make hardening-stress       # Longer campaign with the same input envelopes
```

The Make targets write `events.jsonl`, `stderr.log`, and `summary.json` under
`.work/hardening/regressions/` or `.work/hardening/stress/`. The report identifies
the commit, Go version, required tests, resource counters, seed, completed
iterations, scenario checkpoints, normalized result digests, and replay command.
Inspect the event log for assertion details and the last watchdog checkpoint.

```bash
CHAOS_SEED=123 CHAOS_ITERATIONS=32 CHAOS_PROBES=64 \
  CHAOS_CONCURRENCY=4 CHAOS_DURATION=60s make hardening-regressions
```

Use the **same commit and Go version**, together with all five controls from a
failed report, to replay its generated cases. Iteration numbers start at zero.
To stop after failure iteration `n`, set `CHAOS_ITERATIONS` to `n+1`; earlier
iterations do not change the generated input for that case.

| Control | Ordinary tests | Stress default | Accepted range |
|---|---:|---:|---|
| `CHAOS_SEED` | 1 | 1 locally; workflow run ID nightly | Unsigned 64-bit integer |
| `CHAOS_ITERATIONS` | 32 | 2,048 | 1–4,096 |
| `CHAOS_PROBES` | 64 | 256 | 1–4,096 |
| `CHAOS_CONCURRENCY` | 4 | 8 | 1–64 |
| `CHAOS_DURATION` | 60s | 15m | Positive Go duration, at most 20m |

Explicit controls override profile defaults. Invalid controls fail before the
campaign starts. Duration is a watchdog: expiry fails the run, even if some
iterations passed. The workflow has a separate 20-minute deadline, so keep the
campaign watchdog below that to leave room for setup and artifact upload.

## Scenarios and limits

The seeded campaign varies fault locations and release order across filesystem,
command, scheduler, and registry profiles. A coordinator awaits complete frontiers
and releases channel barriers in the generated order. Frontiers have at most four
nodes and fit within the configured worker budget; generated graphs have at most
four edges per node. Scheduler stress separately exercises worker budgets 1, 4,
and 16 on 4,096-node flat, chain, fan-out, fan-in, and diamond graphs, bounded to
16,384 edges.

Every seeded case runs twice and compares probe IDs, states, observations, error
classes, logical checkpoints, and peak worker counts. Durations and incidental
goroutine arrival order are not replay invariants. Concurrent registry access
tests assert legal terminal outcomes rather than assuming a scheduling winner.

Fixed filesystem cases cover errno propagation, short/changing reads, metadata
failures, oversized input, giant lines, diagnostic storms, long paths, and symlink
hop limits. Only disappearing directory entries may be skipped. OS and memory
symlink depth limits remain distinct. Existing FIFO, conformance, descriptor
inheritance, and FD-zero regressions remain in the required inventory.

Command cases check each 1 MiB output stream below, at, and above its limit, then
exercise failure, finite flooding, slow output, timeout, and caller cancellation.
Flood producers send at most 16 MiB per stream. The existing detached-pipe suite
uses a subreaper to own and clean up its escaped test descendants.

Potentially blocking cases run in supervised test processes. New scenarios have
a 10-second watchdog and 15-second parent deadline; the campaign also has its
configured whole-run deadline. Contract helpers retain at most 4 MiB of synthetic
diagnostics. The command payload watchdog is a finite fallback if its owner
crashes; normal assertion cleanup cancels and joins the runner and verifies child
reaping. Process-group cleanup is not a claim to kill arbitrary escaped sessions.
Filesystem cancellation remains cooperative between syscalls.

Resource checks assert retained bytes, consumed bytes, records, diagnostic counts,
workers, descriptors, and child ownership. Isolated graph cases additionally cap
Go allocations at 128 MiB per run, excluding fixture construction. This is an
allocation regression ceiling, not an OS memory limit or a performance benchmark.
Repeated lifecycle cases check for surviving probe workers and leaked descriptors.

## CI and adding regressions

Ordinary CI keeps JSON events from the same race/coverage test execution and
requires the complete hardening inventory and campaign/resource records. Missing
cases, unexpected required-case skips, crashes, duplicates, and incomplete
campaigns fail the report. Artifacts are retained for 14 days, including available
failure output. The separate nightly workflow runs at 02:23 UTC and supports manual
dispatch with replay controls.

Add regressions beside their owning package, or in `tests/contract` for composed
scenarios and process supervision. Use existing interfaces and private callback
seams; do not introduce production fault switches or weaken the host-I/O boundary.
Register cleanup before starting workers. Add required cases to the inventory in
`scripts/hardening.py` and retain minimized reproductions as permanent tests.

## Bounded fuzz facility

```bash
make fuzz-regressions  # Permanent corpus with race detection
make fuzz-smoke        # Every target, 5 seconds each
make fuzz-stress       # Every target, 60 seconds each
python3 -B scripts/fuzzing.py stress --target FuzzRegistryDAG
```

There are 14 required targets: mountinfo, cgroups, filesystems, sysfs state,
cgroup names, subpaths, scoped symlink graphs, command specifications,
environment policy, bounded buffers, registry DAGs, orchestrator outcomes,
report JSON, and Podman version/info input. os-release belongs to its future parser implementation (#15).
JSON fuzzing tests the shipped DTO/serialization and schema contracts; it does
not assume the partial DTO validator implements the entire schema (#64).

| Envelope | Limit |
|---|---|
| Parser, buffer and JSON input | 64 KiB; JSON nesting at most 64 |
| Paths, names and symlink graph input | 4 KiB; 12 symlinks plus owned fixture directories/files; symlink targets at most 256 bytes |
| Command/environment input | 8 KiB; at most 16 arguments and 16 overrides |
| Registry/scheduler input | 4 KiB; at most 32 nodes, 128 edges and 4 workers |
| Callback | 32 MiB cumulative Go allocation assertion and 10-second fatal watchdog |
| Native exploration | One worker; 5 seconds per target in PRs, 60 seconds nightly |
| Minimization and process | 15-second minimization budget; 180-second process watchdog |
| Whole invocation | 10 minutes for corpus/smoke; 30 minutes for stress |

Oversized inputs return before fixture construction. Callback allocation
accounting is serialized within each test process and includes fixture generation
and replay, but excludes framework cleanup and one-time schema compilation.
This assertion is not an OS memory limit. The watchdog terminates a stuck worker;
a goroutine timeout is not presented as cancellation of arbitrary host I/O.
The supervisor caps stdout at 64 MiB and stderr at 1 MiB per command and reaps its
owned process group on interruption or failure. It also owns and removes the
worker/compiler temporary tree, including after worker crashes. All fixture writes use owned,
fixed paths. Generated executable strings are validated or sent to a fake runner.

`-fuzz-deterministic=true` is the default and required mode. All target decisions
come from input bytes, including scheduler barriers and cancellation checkpoints.
Wall time is a watchdog, not a source of outcome randomness. Native Go fuzz
exploration itself is random; replay uses the saved corpus input, not a promised
reproduction of the mutator's complete exploration sequence.

Targets require permanent files in package-local `testdata/fuzz/<target>/`.
The inventory report includes corpus hashes and exact race-enabled replay
commands. Native Go fuzzing writes minimized failures to that same directory;
retain the resulting file with its fix. Failure artifacts also copy available
corpus files. A killed process may not produce a minimized input; retain its
logs and replay context without claiming that it did. Mutation-cache entries
are exploration aids, not substitutes for checked-in regressions.

The runner reports corpus membership, limits, execution counts, commands and
outcomes under `.work/fuzz/`. Missing targets, missing permanent corpora,
skipped targets, missing exploration evidence and incomplete executions fail.
Nightly fuzzing is a separate 30-minute job alongside the existing 20-minute
fault/resource job, with optional exact-target selection for manual replay.

## Complete M1.1 gate

```bash
make hardening-gate   # Requires the pinned verification/build tools
make benchmark-smoke # Fixed inventory, one iteration per case
```

The complete gate uses six shared stages: lint/contracts, tests/coverage/benchmark,
govulncheck, Gitleaks, build/artifact verification, and bounded fuzz smoke. It runs
ordinary tests and race tests, Go vet, module checks, full lint, architectural and
script/workflow contracts, exact coverage checks, and the existing hardening
report over the same race-test events. Coverage is at least 95% overall and
strictly greater than 95% separately for model and requirement.

The benchmark inventory has 152 cases across buffer, filesystem, execution-plan
and concurrency families. Each runs once with allocation reporting. Missing or
duplicated cases fail; elapsed times are retained as smoke evidence without
shared-runner performance thresholds. Broad performance qualification stays in #41.

Build verification reuses the full/minimal microfat launcher, static payload,
archive, SBOM and provenance checks. amd64 smoke runs natively; arm64 verification
is structural and integrity-based. Govulncheck reports reachable findings
separately from package-only and module-only advisories. Gitleaks output is
redacted. A passing scanner is not a security certification; known P1/high/critical
foundation defects must be resolved before milestone closure.

Each stage writes a versioned report with commit, source-content digest, Go/tool
versions, workflow run/attempt, command outcomes and detailed evidence. The final
`M1.1 Hardening Gate` CI check requires every dependency and complete matching
reports. Missing, skipped, failed, duplicated or stale evidence fails. CI uploads
available stage reports/logs and the final JSON/Markdown summary for 14 days.
A local dirty-tree report explicitly identifies development evidence and cannot
be substituted for clean-commit completion evidence.

The release workflow runs the same stages and final aggregation before handing
artifacts to the existing isolated tag publisher. Local verification and manual
release rehearsals do not sign or publish. Milestone closure follows a passing
merged-commit gate; repository branch-protection settings are a separate
maintainer policy.


## Fixture evaluation contracts

Run `go test -race ./...` to exercise the M1.2 fixture path alongside the complete M1.1 suite. `tests/contract/evaluation_test.go` builds the actual CLI, checks all five capability states and three requirement outcomes, validates every expected report against JSON Schema, and executes `examples/check-report.py`. It also checks additive unknown fields and the full published capability catalog.

`tests/contract/fixture_conformance_test.go` replays the same fixture definitions through the existing scoped OS and memory readers and compares real parser observations. The shared platform conformance tests remain authoritative for symlinks, partial reads, descriptor lifetime and intentional OS/memory differences; this harness does not replace them.

Application tests cover concurrent borrowed services, close-after-join ownership, failures and cancellation. Pure evaluator tests cover precedence, equal-rank conflict, stale/incomplete evidence, graph integrity, bounded ASTs and candidate isolation. Fixture timestamps are fixed; generated `expected.json` files are committed regression expectations and must be reviewed when behavior changes. The original info fixtures are synthetic. Version fixtures use synthetic identity/filesystem metadata and identify the captured Fedora version transcript in their provenance descriptions.

## Passive Podman discovery

`tests/contract/podman_live_test.go` checks live-mode schema/provenance pairs, version replay goldens and the CLI availability consumer. Unit tests exercise missing/denied paths, current credentials, fresh timestamps, command failure sanitization and immutable observation transfer. The discovery smoke script traces the actual CGO-disabled binary under the current user and, in CI, host root; it rejects subprocess execution, write-capable opens and filesystem mutations. Live version execution remains disabled following the startup review documented in [Podman discovery](podman-discovery.md).

## Native Podman inspection evidence

CI keeps the passive syscall smoke check for both the runner account and host root. It also builds a CGO-disabled capagent binary and runs `scripts/podman-inspection-smoke.py` on the disposable hosted machine. The active harness requires root, `GITHUB_ACTIONS=true` and `--ephemeral-host`; do not use it on a persistent host. It temporarily supplies owned rootful storage configuration and restores the original file even on failure. Rootless state uses dedicated HOME/XDG directories. Runtime state and traced pause processes remain until runner teardown; the harness never runs reset/prune.

The root tracer uses `strace -u <account>` so UID/GID/groups and setuid mapping helpers retain their normal semantics. Both real accounts run fresh and initialized inspection. A delayed `execve` exceeds the version deadline and traces the runner's cancellation/reaping path. Configured remote mode and a checksum-pinned Podman 5.8.4 remote-only client must fail before any connection attempt. Local D-Bus attempts are recorded separately from remote transports. Captured reports, syscall traces, environment policy, selected argv, process remnants and package identity are uploaded under `.work/gate/podman/`. These checks establish inspection behavior on the tested runner, not general workload readiness or a distribution support matrix.

Authentic parser captures and their redaction/source notes live in `testdata/podman/info/` and `testdata/podman/version/`. The historical 3.x/4.x info sources are published YAML captures transcoded into the equivalent JSON shape; the 5.x capture came from a native local command. Missing source fields stay missing. Combined fixtures use synthetic contexts and failure mutations explicitly labeled as such.
