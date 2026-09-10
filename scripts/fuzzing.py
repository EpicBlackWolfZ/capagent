#!/usr/bin/env python3
"""Inventory, replay and supervise the bounded native Go fuzz facility."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import sys
import time

import hardening
from verification import execute, identity, write_report, install_signals

MODULE = hardening.MODULE
TARGETS = {
    **{name: ('internal/platform', limit) for name, limit in [
        ('FuzzMountinfo', 65536), ('FuzzCgroups', 65536), ('FuzzFilesystems', 65536),
        ('FuzzSysfsState', 65536), ('FuzzCgroupNames', 4096), ('FuzzSubpath', 4096),
        ('FuzzScopedSymlinkGraph', 4096), ('FuzzCommandSpec', 8192), ('FuzzEnvPolicy', 8192),
        ('FuzzBoundedBuffer', 65536)]},
    'FuzzRegistryDAG': ('internal/probe', 4096),
    'FuzzOrchestrator': ('tests/contract', 4096),
    'FuzzReportJSON': ('tests/contract', 65536),
}


def inventory(root):
    found = {}
    for package in {row[0] for row in TARGETS.values()}:
        for source in (root/package).glob('*_test.go'):
            for name in re.findall(r'^func (Fuzz\w+)\(f \*testing\.F\)', source.read_text(), re.M):
                if name in found:
                    raise ValueError('duplicate fuzz target: ' + name)
                found[name] = package
    if found != {name: row[0] for name, row in TARGETS.items()}:
        raise ValueError('fuzz target inventory is missing, moved or unregistered')
    result = []
    for name, (package, limit) in TARGETS.items():
        seeds = []
        for path in sorted((root/package/'testdata/fuzz'/name).glob('*')):
            if path.is_symlink() or not path.is_file() or path.stat().st_size > 1 << 20:
                raise ValueError('invalid corpus file: ' + str(path))
            seeds.append(dict(path=str(path.relative_to(root)), sha256=hashlib.sha256(path.read_bytes()).hexdigest(),
                              replay=f"go test -race ./{package} -run '^{name}/{path.name}$'"))
        if not seeds:
            raise ValueError('missing permanent corpus: ' + name)
        result.append(dict(target=name, package=package, input_bytes=limit, allocation_bytes=32 << 20,
                           callback_timeout_seconds=10, deterministic=True, corpus=seeds))
    return result


def verify_events(events, targets):
    passes, packages = [], set()
    expected = {(MODULE+'/'+TARGETS[name][0], name) for name in targets}
    for event in events:
        action, package, name = event.get('Action'), event.get('Package'), event.get('Test')
        if action == 'fail':
            raise ValueError('fuzz test or package failed')
        if (package, name) in expected:
            if action == 'skip':
                raise ValueError('required fuzz target skipped')
            if action == 'pass':
                passes.append((package, name))
        if action == 'pass' and not name:
            packages.add(package)
    if len(passes) != len(expected) or set(passes) != expected or not {p for p, _ in expected} <= packages:
        raise ValueError('missing or duplicate fuzz completion evidence')


def run(profile, output, target=None):
    output = Path(output)
    report = dict(schema_version=1, kind='fuzz', status='fail', identity=identity(), profile=profile,
                  inventory=[], commands=[], failures=[], seconds_per_target=0 if profile == 'corpus' else 5 if profile == 'smoke' else 60)
    try:
        report['inventory'] = inventory(Path.cwd())
        targets = [target] if target else list(TARGETS)
        if target and target not in TARGETS:
            raise ValueError('unknown fuzz target')
        env = dict(os.environ, CAPAGENT_FUZZ_FIXED='synthetic', GOMAXPROCS='2')
        if profile == 'corpus':
            packages = sorted({TARGETS[name][0] for name in targets})
            commands = [('corpus', targets, ['go', 'test', '-race', '-count=1', '-json', '-timeout=2m',
                         '-run=^('+'|'.join(targets)+')$', *['./'+p for p in packages]])]
        else:
            duration = '5s' if profile == 'smoke' else '60s'
            commands = [(name, [name], ['go', 'test', '-json', '-run=^$', '-count=1', '-parallel=1',
                         '-fuzz=^'+name+'$', '-fuzztime='+duration, '-fuzzminimizetime=15s', '-timeout=3m',
                         './'+TARGETS[name][0]]) for name in targets]
        deadline = time.monotonic() + (1800 if profile == 'stress' else 600)
        for name, selected, command in commands:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ValueError('fuzz campaign watchdog expired')
            result = execute(command, output/name, timeout=min(180, remaining), env=env)
            report['commands'].append(result)
            if result['status'] != 'pass':
                raise ValueError(name+': '+(result['error'] or 'test process failed'))
            events = list(hardening.read_events(Path(result['stdout'])))
            verify_events(events, selected)
            if profile != 'corpus':
                output_text = ''.join(row.get('Output', '') for row in events)
                counts = re.findall(r'execs: (\d+)', output_text)
                if not counts or int(counts[-1]) <= 0 or 'fuzzing with 1 workers' not in output_text:
                    raise ValueError('missing native fuzz exploration evidence: '+name)
                result['executions'] = int(counts[-1])
        report['status'] = 'pass'
    except (OSError, ValueError, TypeError) as exc:
        report['failures'].append(str(exc))
    finally:
        for package in {row[0] for row in TARGETS.values()}:
            source = Path(package)/'testdata/fuzz'
            if source.exists():
                shutil.copytree(source, output/'corpus'/package, dirs_exist_ok=True)
        write_report(output/'summary.json', report)
    print(f"Fuzz {profile}: {report['status']}; {output/'summary.json'}", flush=True)
    return report


def main():
    install_signals()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('profile', choices=('corpus', 'smoke', 'stress'))
    parser.add_argument('--output', type=Path)
    parser.add_argument('--target', choices=list(TARGETS))
    args = parser.parse_args()
    report = run(args.profile, args.output or Path('.work/fuzz')/args.profile, args.target)
    return int(report['status'] != 'pass')


if __name__ == '__main__':
    sys.exit(main())
