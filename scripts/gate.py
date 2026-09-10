#!/usr/bin/env python3
"""Run and aggregate the permanent, non-publishing M1.1 verification gate."""
import argparse
from collections import Counter, defaultdict
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import sys
import subprocess
import uuid

import fuzzing
import hardening
from verification import command_text, execute, identity, write_report, install_signals

STAGES = ('lint', 'test', 'vulncheck', 'gitleaks', 'build', 'fuzz')
BENCHMARK = '^Benchmark(BoundedBuffer|ExecutionPlan|OrchestratorConcurrency|FilesystemRead|FilesystemParser)$'
BENCH_FAMILIES = ('BenchmarkBoundedBuffer', 'BenchmarkExecutionPlan', 'BenchmarkOrchestratorConcurrency',
                  'BenchmarkFilesystemRead', 'BenchmarkFilesystemParser')
BENCH_COUNTS = dict(zip(BENCH_FAMILIES, (112, 8, 30, 1, 1)))
COMMANDS = dict(lint=('modules', 'vet', 'lint', 'contracts'),
                test=('unit', 'race', 'coverage', 'hardening', 'benchmarks'),
                vulncheck=('scanner',), gitleaks=('scanner',), build=('release',), fuzz=('smoke',))


def required_commands(stage):
    return COMMANDS[stage]


def coverage(path):
    counts = defaultdict(lambda: dict(covered=0, total=0))
    for line in Path(path).read_text().splitlines():
        if line.startswith('mode:'):
            continue
        location, statements, hits = line.split()
        package = str(PurePosixPath(location.split(':')[0]).parent)
        for key in (package, 'total'):
            counts[key]['total'] += int(statements)
            counts[key]['covered'] += int(statements) if int(hits) > 0 else 0
    return {key: counts[key] for key in ('total', hardening.MODULE+'/internal/model', hardening.MODULE+'/internal/requirement')}


def vulnerabilities(events):
    found = dict(reachable=set(), package_only=set(), module_only=set())
    for event in events:
        finding = event.get('finding')
        if not finding:
            continue
        trace = finding.get('trace', [])
        level = ('reachable' if any(frame.get('function') for frame in trace) else
                 'package_only' if any(frame.get('package') for frame in trace) else 'module_only')
        found[level].add(finding['osv'])
    return {key: sorted(value) for key, value in found.items()}


def json_stream(path):
    text = Path(path).read_text()
    decoder, position = json.JSONDecoder(), 0
    while position < len(text):
        if text[position].isspace():
            position += 1
            continue
        value, position = decoder.raw_decode(text, position)
        if not isinstance(value, dict):
            raise ValueError('scanner JSON record must be an object')
        yield value


def benchmarks(events):
    # Go's test2json can split a benchmark line between output events.
    output = ''.join(event.get('Output', '') for event in events)
    pattern = r'^(Benchmark\S+?)-\d+\s+(\d+)\s+([\d.]+) ns/op(?:.*?\s)(\d+) B/op\s+(\d+) allocs/op\s*$'
    return [dict(name=name, iterations=int(n), ns_per_op=float(ns), bytes_per_op=int(size), allocs_per_op=int(allocs))
            for name, n, ns, size, allocs in re.findall(pattern, output, re.M)]


def validate_details(stage, detail, expected):
    if stage == 'test':
        if detail.get('hardening', {}).get('status') != 'pass':
            raise ValueError('missing passing hardening report')
        counts = detail['coverage']
        for key in ('total', hardening.MODULE+'/internal/model', hardening.MODULE+'/internal/requirement'):
            c, n = counts[key]['covered'], counts[key]['total']
            if type(c) is not int or type(n) is not int or not 0 <= c <= n or n == 0:
                raise ValueError('invalid coverage counts')
            if c*100 < n*95 or (key != 'total' and c*100 == n*95):
                raise ValueError('coverage threshold failed')
        if sorted(detail['corpus_targets']) != sorted(fuzzing.TARGETS):
            raise ValueError('missing corpus targets')
        rows = detail['benchmarks']
        if Counter(row['name'].split('/')[0] for row in rows) != BENCH_COUNTS:
            raise ValueError('missing or unexpected benchmark cases')
        if any(row['iterations'] != 1 for row in rows) or len({row['name'] for row in rows}) != len(rows):
            raise ValueError('invalid or duplicated benchmark result')
    elif stage == 'fuzz':
        report = detail['fuzz']
        if report['status'] != 'pass' or report['profile'] != 'smoke' or report['identity'] != expected:
            raise ValueError('missing passing short fuzz report for this source/run')
        if sorted(row['target'] for row in report['inventory']) != sorted(fuzzing.TARGETS):
            raise ValueError('incomplete fuzz inventory')
        if any(not row['corpus'] for row in report['inventory']) or len(report['commands']) != len(fuzzing.TARGETS):
            raise ValueError('missing fuzz corpus or executions')
        if any(row['status'] != 'pass' or row['exit_code'] != 0 for row in report['commands']):
            raise ValueError('fuzz command did not pass')
    elif stage == 'vulncheck':
        if detail['vulnerabilities']['reachable']:
            raise ValueError('reachable vulnerabilities remain')
    elif stage == 'gitleaks':
        if detail['findings_count'] != 0:
            raise ValueError('secret findings remain')
    elif stage == 'build':
        for mode in ('full', 'minimal'):
            record = detail[mode]
            if record['commit'] != expected['commit'] or record['launcher_mode'] != mode:
                raise ValueError('release evidence belongs to another commit or launcher mode')
            if set(record['bundles']) != {'amd64', 'arm64'}:
                raise ValueError('missing architecture artifact verification')
        if len(detail['release']['archives']) != 6 or detail['release']['commit'] != expected['commit']:
            raise ValueError('missing release archives/SBOM evidence')
        if not re.fullmatch('[a-f0-9]{64}', detail['handoff_sha256']):
            raise ValueError('missing release handoff digest')


def summarize(records, expected, outcomes=None):
    failures = []
    names = [row.get('stage', 'unknown') if isinstance(row, dict) else 'malformed' for row in records]
    if sorted(names) != sorted(STAGES):
        failures.append('missing, duplicate or unexpected stage report')
    if outcomes is not None and any(outcomes.get(name) != 'success' for name in STAGES):
        failures.append('required CI dependency did not succeed')
    for row in records:
        if not isinstance(row, dict):
            failures.append('malformed stage report')
            continue
        stage = row.get('stage', 'unknown')
        try:
            if row.get('schema_version') != 1 or row.get('kind') != 'gate-stage' or row.get('status') != 'pass':
                raise ValueError('stage is not a passing supported report')
            if row.get('identity') != expected:
                raise ValueError('source, toolchain or run identity mismatch')
            commands = row['commands']
            if [c['name'] for c in commands] != list(required_commands(stage)):
                raise ValueError('missing, duplicate or reordered command evidence')
            if any(c['status'] != 'pass' or c['exit_code'] != 0 for c in commands) or row['failures']:
                raise ValueError('failed or skipped command evidence')
            validate_details(stage, row['details'], expected)
        except (KeyError, TypeError, ValueError) as exc:
            failures.append(stage+': '+str(exc))
    return dict(schema_version=1, kind='m1.1-gate', status='fail' if failures else 'pass',
                identity=expected, stages=records, failures=failures)


def stage_commands(stage, output, release_mode):
    return {
        'lint': [('modules', ['make', 'check-mod']), ('vet', ['go', 'vet', './...']),
                 ('lint', ['make', 'lint']), ('contracts', ['make', 'build-contracts'])],
        'test': [('unit', ['go', 'test', '-json', '-count=1', '-timeout=5m', './...']),
                 ('race', ['go', 'test', '-race', '-json', '-count=1', '-timeout=5m',
                           '-coverprofile='+str(output/'coverage.out'), '-covermode=atomic', './...']),
                 ('coverage', ['python3', '-B', 'scripts/check-coverage.py', str(output/'coverage.out'), '95.0']),
                 ('hardening', ['python3', '-B', 'scripts/hardening.py', 'report', '--events',
                                str(output/'race/events.jsonl'), '--output', str(output/'hardening')]),
                 ('benchmarks', ['go', 'test', '-json', '-run=^$', '-bench='+BENCHMARK,
                                 '-benchtime=1x', '-benchmem', '-count=1', '-timeout=2m',
                                 './internal/platform', './internal/probe'])],
        'vulncheck': [('scanner', ['govulncheck', '-json', './...'])],
        'gitleaks': [('scanner', ['gitleaks', 'git', '--redact', '--no-banner', '--report-format=json',
                                 '--report-path='+str(output/'gitleaks.json')])],
        'build': [('release', ['make', 'release-check'] if release_mode == 'snapshot' else ['./scripts/release-check.sh', 'tag'])],
        'fuzz': [('smoke', ['python3', '-B', 'scripts/fuzzing.py', 'smoke', '--output', str(output/'fuzz')])],
    }[stage]


def collect_details(stage, output, ident):
    if stage == 'test':
        fuzzing.verify_events(hardening.read_events(output/'race/events.jsonl'), list(fuzzing.TARGETS))
        return dict(coverage=coverage(output/'coverage.out'), corpus_targets=list(fuzzing.TARGETS),
                    hardening=json.loads((output/'hardening/summary.json').read_text()),
                    benchmarks=benchmarks(hardening.read_events(output/'benchmarks/events.jsonl')))
    if stage == 'fuzz':
        return dict(fuzz=json.loads((output/'fuzz/summary.json').read_text()))
    if stage == 'vulncheck':
        return dict(vulnerabilities=vulnerabilities(json_stream(output/'scanner/events.jsonl')))
    if stage == 'gitleaks':
        findings = json.loads((output/'gitleaks.json').read_text())
        if findings is not None and not isinstance(findings, list):
            raise ValueError('invalid gitleaks JSON')
        return dict(findings_count=len(findings or []))
    if stage == 'build':
        detail = {}
        for mode in ('full', 'minimal'):
            path = Path('dist')/f'verified-{mode}.json'
            detail[mode] = json.loads(path.read_text())
            shutil.copyfile(path, output/path.name)
        detail['release'] = json.loads(Path('dist/release/release-inputs.json').read_text())
        detail['handoff_sha256'] = hashlib.sha256(Path('dist/release-inputs.tar.gz').read_bytes()).hexdigest()
        shutil.copyfile('dist/release/release-inputs.json', output/'release-inputs.json')
        return detail
    return {}


def tool_versions(stage):
    tools = {
        'lint': {'golangci-lint': ['golangci-lint', 'version'], 'actionlint': ['actionlint', '-version'],
                 'shellcheck': ['shellcheck', '--version']},
        'vulncheck': {'govulncheck': ['govulncheck', '-version']},
        'gitleaks': {'gitleaks': ['gitleaks', 'version']},
        'build': {'goreleaser': ['goreleaser', '--version'], 'syft': ['syft', 'version']},
    }.get(stage, {})
    return {name: command_text(cmd) for name, cmd in tools.items()}


def run_stage(stage, output, release_mode='snapshot'):
    output = Path(output)
    ident = identity()
    report = dict(schema_version=1, kind='gate-stage', stage=stage, status='fail', identity=ident,
                  commands=[], failures=[], tools={}, details={})
    # Replace a stale success before the first operation, including tool setup failure.
    write_report(output/'summary.json', report)
    try:
        report['tools'] = tool_versions(stage)
        for name, command in stage_commands(stage, output, release_mode):
            if stage == 'fuzz':
                # Keep one supervisor: a killed Python wrapper must not orphan a
                # separately sessioned go fuzz process owned by a nested runner.
                nested = fuzzing.run('smoke', output/'fuzz')
                result = dict(status=nested['status'], exit_code=int(nested['status'] != 'pass'),
                              error='; '.join(nested['failures']), operation='in-process fuzzing.run', replay_command=command)
            else:
                result = execute(command, output/name, timeout=1200 if stage == 'build' else 600)
            report['commands'].append(dict(name=name, **result))
            if result['status'] != 'pass':
                raise ValueError(name+': '+(result['error'] or f"exit {result['exit_code']}"))
        report['details'] = collect_details(stage, output, ident)
        validate_details(stage, report['details'], ident)
        if identity() != ident:
            raise ValueError('source changed during verification')
        report['status'] = 'pass'
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as exc:
        report['failures'].append(str(exc))
    finally:
        write_report(output/'summary.json', report)
    print(f"Gate {stage}: {report['status']}; {output/'summary.json'}", flush=True)
    return report


def finish(root, expected, outcomes=None):
    records, errors = [], []
    for stage in STAGES:
        try:
            records.append(json.loads((root/stage/'summary.json').read_text()))
        except (OSError, ValueError) as exc:
            errors.append(stage+': '+str(exc))
    report = summarize(records, expected, outcomes)
    report['failures'].extend(errors)
    if errors:
        report['status'] = 'fail'
    write_report(root/'summary.json', report)
    text = f"M1.1 hardening gate: **{report['status']}**\n\nCommit: `{expected['commit']}`\n\n"
    text += '\n'.join(f"- {row['stage']}: {row['status']}" for row in records)+'\n'
    text += '\n'.join('- '+failure for failure in report['failures'])+'\n'
    if expected['dirty']:
        text += '\nWorking tree differs from the commit; this is local development evidence.\n'
    (root/'summary.md').write_text(text)
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as sink:
            sink.write(text)
    print(text, flush=True)
    return report


def main():
    install_signals()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('run', 'stage', 'report'))
    parser.add_argument('--stage', choices=STAGES)
    parser.add_argument('--output', type=Path, default=Path('.work/gate'))
    parser.add_argument('--release-mode', choices=('snapshot', 'tag'), default='snapshot')
    args = parser.parse_args()
    if args.mode == 'stage':
        if not args.stage:
            parser.error('--stage is required')
        result = run_stage(args.stage, args.output/args.stage, args.release_mode)
    elif args.mode == 'run':
        os.environ.setdefault('CAPAGENT_GATE_RUN', str(uuid.uuid4()))
        expected = identity()
        for stage in STAGES:
            if run_stage(stage, args.output/stage, args.release_mode)['status'] != 'pass':
                break
        result = finish(args.output, expected)
    else:
        outcomes = json.loads(os.environ['CAPAGENT_JOB_RESULTS']) if os.environ.get('CAPAGENT_JOB_RESULTS') else None
        result = finish(args.output, identity(), outcomes)
    return int(result['status'] != 'pass')


if __name__ == '__main__':
    sys.exit(main())
