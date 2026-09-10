# Foundation fault and resource testing

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

These suites deliver the fault/resource portion of M1.1. The fuzz facility (#44)
and final integrated milestone gate (#49) remain separate deliverables.
