#!/usr/bin/env python3
"""Run bounded foundation regressions and fail closed on incomplete test evidence."""
import argparse
import json
import os
from pathlib import Path
import selectors
import shlex
import signal
import subprocess
import sys
import time

MODULE = 'github.com/EpicBlackWolfZ/capagent'
REQUIRED = {
    MODULE + '/internal/platform': [
        'TestAdversarialPlatform/paths', 'TestAdversarialPlatform/parsers', 'TestAdversarialPlatform/reads',
        'TestReadBudgetConformance', 'TestFIFOReadIsBounded', 'TestBoundedByteLoop',
        'TestBoundedBufferGrowthBudget', 'TestBoundedBufferBoundary', 'TestBoundedBufferSingleWriteAndZeroCapacity',
        'TestParserLimitsAndDiagnostics', 'TestControllerCompleteness', 'TestDirectoryInterruptedAndIncomplete',
        'TestScopedDescriptorsCloseOnExec', 'TestScopedDescriptorsZeroExec', 'TestScopedOSReader_FDZeroLifecycle',
        'TestFilesystemPathConformance', 'TestDirectorySnapshotConformance', 'TestMemorySymlinkHopBoundary',
        *['TestRunnerDetachedPipeBudget/' + case for case in ('success', 'failure', 'timeout', 'cancel', 'deadline')],
    ],
    MODULE + '/internal/probe': [
        'TestRegistry_TerminalFailure', 'TestOrchestratorWorkerBudgetBarrier',
        'TestOrchestrator_ConcurrentLifecycleContract', 'TestRegistry_CapturesDeclarationsOnce',
        'TestOrchestrator_SnapshotPreservesPrerequisite',
    ],
    MODULE + '/tests/contract': [
        'TestHardeningHarnessContracts', 'TestChaosCampaign', 'TestChaosFilesystem', 'TestChaosRegistry',
        'TestResourceCommands', 'TestResourceOwnership',
        'TestFuzzEnvelopeFailures', 'TestWorkflow_FinalHardeningGate', 'TestArchitecture_FuzzHelpersStayTestOnly',
        *['TestResourceGraphs/' + shape + '/' + workers
          for shape in ('flat', 'chain', 'fan-out', 'fan-in', 'diamond') for workers in ('1', '4', '16')],
    ],
}
PREFIX = 'HARDENING '
EVENT_LIMIT = 64 << 20
STDERR_LIMIT = 1 << 20
RESOURCE_CASES = [('graph/' + shape + '/' + mode, workers)
                  for shape in ('flat', 'chain', 'fan-out', 'fan-in', 'diamond')
                  for mode in ('success', 'failed-root', 'cancel-before', 'cancel-running')
                  for workers in (1, 4, 16)] + [('commands', None), ('ownership', None), ('descriptors', None)]


def summarize(events, commit, go_version, test_exit=0):
    states, failures, records = {}, [], []
    for event in events:
        package, name, action = event.get('Package', ''), event.get('Test', ''), event.get('Action')
        if action in ('pass', 'fail', 'skip') and name:
            key = package, name
            if states.get(key) not in ('fail', 'skip'):
                states[key] = action
        if action == 'fail':
            failures.append(f'{package}/{name}: failed')
        if action == 'skip' and any(name == required or name.startswith(required + '/')
                                    for required in REQUIRED.get(package, [])):
            failures.append(f'{package}/{name}: unexpected skip')
        output = event.get('Output', '')
        if PREFIX in output:
            try:
                record = json.loads(output.split(PREFIX, 1)[1].strip())
                if not isinstance(record, dict):
                    raise ValueError('record must be an object')
                records.append(record)
            except (ValueError, TypeError):
                failures.append('invalid hardening record')
    inventory = []
    for package, names in REQUIRED.items():
        for name in names:
            status = states.get((package, name), 'missing')
            inventory.append(dict(package=package, test=name, status=status))
            if status != 'pass':
                failures.append(f'{package}/{name}: {status}')
    starts = [r for r in records if r.get('kind') == 'campaign_start']
    completed = [r for r in records if r.get('kind') == 'campaign_complete']
    begins = [r for r in records if r.get('kind') == 'chaos_begin']
    cases = [r for r in records if r.get('kind') == 'chaos_case']
    config = starts[0].get('config', {}) if len(starts) == 1 else {}
    if not isinstance(config, dict):
        config = {}
    for key, low, high in [('Seed', 0, (1 << 64) - 1), ('Iterations', 1, 4096),
                           ('Probes', 1, 4096), ('Concurrency', 1, 64)]:
        value = config.get(key)
        if type(value) is not int or not low <= value <= high:
            failures.append(f'invalid or missing replay control: {key}')
    iterations = config.get('Iterations')
    indices = [r.get('iteration') for r in cases]
    if (not isinstance(iterations, int) or isinstance(iterations, bool) or not 1 <= iterations <= 4096
            or len(starts) != 1 or len(completed) != 1
            or completed[0].get('iterations') != iterations
            or indices != list(range(iterations))
            or [r.get('iteration') for r in begins] != indices
            or any(r.get('seed') != config.get('Seed') for r in cases)):
        failures.append('campaign evidence is incomplete, inconsistent, or duplicated')
    if test_exit:
        failures.append(f'test process exited {test_exit}')
    duration = starts[0].get('duration', '') if starts else ''
    if not isinstance(duration, str) or not duration:
        failures.append('missing campaign duration')
    controls = {'CHAOS_SEED': config.get('Seed'), 'CHAOS_ITERATIONS': iterations,
                'CHAOS_PROBES': config.get('Probes'), 'CHAOS_CONCURRENCY': config.get('Concurrency'),
                'CHAOS_DURATION': duration}
    replay = ' '.join(f'{k}={shlex.quote(str(v))}' for k, v in controls.items()) + ' make hardening-regressions'
    resources = [r for r in records if r.get('kind') == 'resource_case']
    resource_ids = [(r.get('scenario'), r.get('workers')) for r in resources]
    for required in RESOURCE_CASES:
        if resource_ids.count(required) != 1:
            failures.append(f'resource evidence missing or duplicated: {required}')
    return dict(schema_version=1, status='fail' if failures else 'pass', commit=commit, go_version=go_version,
                inventory=inventory, failures=failures,
                campaign=dict(controls=controls, completed_iterations=len(cases), replay=replay, cases=cases, checkpoints=begins,
                              duration_ns=completed[0].get('duration_ns') if len(completed) == 1 else None),
                resources=resources,
                logs=dict(events='events.jsonl', stderr='stderr.log'))


def selection():
    names = sorted({name.split('/')[0] for tests in REQUIRED.values() for name in tests})
    return '^(' + '|'.join(names) + ')$'


def run_command(command, output, env, timeout):
    """Bound captured bytes, wall time, and cleanup, including keyboard interruption."""
    output.mkdir(parents=True, exist_ok=True)
    with (output / 'events.jsonl').open('wb') as events, (output / 'stderr.log').open('wb') as stderr:
        process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   env=env, start_new_session=True)
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ, [events, EVENT_LIMIT])
                selector.register(process.stderr, selectors.EVENT_READ, [stderr, STDERR_LIMIT])
                deadline = time.monotonic() + timeout
                while selector.get_map():
                    if time.monotonic() >= deadline:
                        raise TimeoutError('hardening process watchdog expired')
                    for key, _ in selector.select(timeout=min(0.1, max(0, deadline - time.monotonic()))):
                        data = os.read(key.fileobj.fileno(), 65536)
                        if not data:
                            selector.unregister(key.fileobj)
                            continue
                        sink, remaining = key.data
                        sink.write(data[:remaining])
                        if len(data) > remaining:
                            raise ValueError('hardening log budget exceeded')
                        key.data[1] -= len(data)
                return process.wait(timeout=max(0.1, deadline - time.monotonic()))
        finally:
            # The direct go process and its group remain owned until wait.
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
            process.wait()
            process.stdout.close()
            process.stderr.close()


def read_events(path):
    if path.stat().st_size > EVENT_LIMIT:
        raise ValueError('events exceed report budget')
    with path.open(encoding='utf-8') as source:
        for line in source:
            if line.strip():
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise ValueError('test event is not an object')
                yield event


def metadata(command):
    return subprocess.check_output(command, text=True, timeout=10).strip()


def main():
    def interrupted(_signum, _frame):
        raise KeyboardInterrupt('hardening run interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('run', 'report'))
    parser.add_argument('--profile', choices=('regressions', 'stress'), default='regressions')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--events', type=Path)
    parser.add_argument('--test-exit', type=int, default=0)
    args = parser.parse_args()
    output = args.output or Path('.work/hardening') / args.profile
    output.mkdir(parents=True, exist_ok=True)
    exit_code, errors = args.test_exit, []
    if args.mode == 'run':
        env = dict(os.environ)
        if args.profile == 'stress':
            for key, value in dict(CHAOS_ITERATIONS='2048', CHAOS_PROBES='256',
                                   CHAOS_CONCURRENCY='8', CHAOS_DURATION='15m').items():
                env.setdefault(key, value)
        command = ['go', 'test', '-race', '-count=1', '-json', '-timeout=22m', '-run', selection(),
                   './internal/platform', './internal/probe', './tests/contract']
        print('Running ' + shlex.join(command), flush=True)
        try:
            exit_code = run_command(command, output, env, timeout=22 * 60)
        except (OSError, ValueError, TimeoutError, KeyboardInterrupt) as exc:
            errors.append(str(exc) or 'interrupted')
            exit_code = 1
        events_path = output / 'events.jsonl'
    else:
        events_path = args.events or output / 'events.jsonl'
    try:
        report = summarize(read_events(events_path), metadata(['git', 'rev-parse', 'HEAD']),
                           metadata(['go', 'version']), exit_code)
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        errors.append(str(exc))
        report = summarize([], 'unavailable', 'unavailable', 1)
    report['failures'].extend(errors)
    report['logs']['events'] = str(events_path)
    report['logs']['stderr'] = str(output / 'stderr.log') if (output / 'stderr.log').exists() else None
    if errors:
        report['status'] = 'fail'
    (output / 'summary.json').write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        with open(os.environ['GITHUB_STEP_SUMMARY'], 'a', encoding='utf-8') as summary:
            summary.write(f"\nHardening **{report['status']}**: {len(report['inventory'])} required cases, "
                          f"{report['campaign']['completed_iterations']} completed seeded cases.\n\n"
                          "The test artifact contains the case inventory, resource counters, failures, and replay command.\n")
    print(f"Hardening: {report['status']}; {len(report['inventory'])} required cases; "
          f"{report['campaign']['completed_iterations']} completed seeded cases; {output / 'summary.json'}")
    for failure in report['failures']:
        print(failure, file=sys.stderr)
    return 0 if report['status'] == 'pass' else 1


if __name__ == '__main__':
    sys.exit(main())
