#!/usr/bin/env python3
"""Verify live requirement exits without executing Podman or deploying workloads."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import uuid

SPEC = importlib.util.spec_from_file_location('passive', Path(__file__).with_name('podman-discovery-smoke.py'))
PASSIVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PASSIVE)
STATES = ('SATISFIED', 'UNSATISFIED', 'INDETERMINATE')


def verify_report(report, uid, expected_exit, collection):
    if expected_exit not in range(len(STATES)):
        raise ValueError('execution failure is not a requirement outcome')
    evaluation = report.get('evaluation', {})
    if report.get('context', {}).get('uid') != uid or evaluation.get('mode') != 'live':
        raise ValueError('incorrect target identity or evaluation mode')
    if evaluation.get('collection') != collection or evaluation.get('requirement', {}).get('state') != STATES[expected_exit]:
        raise ValueError('requirement outcome or collection policy mismatch')


def run_suite(binary, output, target_uid=None, tracer=None):
    binary = binary.resolve(strict=True)
    output.mkdir(parents=True, exist_ok=False, mode=0o700)
    output = output.resolve()
    try:
        return run_cases(binary, output, target_uid, tracer)
    finally:
        # These are fixed generated predicates, never credential/configuration
        # files. Keep them caller-only during execution, then export evidence
        # for the unprivileged CI artifact uploader, including on failed runs.
        for state in STATES:
            document = output / (state.lower() + '.requirement.json')
            if document.exists():
                document.chmod(0o644)
        output.chmod(0o755)


def run_cases(binary, output, target_uid, tracer):
    uid = os.geteuid() if target_uid is None else target_uid
    missing = '/capagent-requirement-missing-' + uuid.uuid4().hex + '/podman'
    cases = []
    nodes = ({'not': {'capability': 'runtime.podman'}}, {'capability': 'runtime.podman'},
             {'capability': 'runtime.podman.info'})
    for expected, node in enumerate(nodes):
        name = STATES[expected].lower()
        document = output / (name + '.requirement.json')
        document.write_text(json.dumps(node) + '\n')
        document.chmod(0o600)
        # Root-owned caller documents deliberately cannot be opened by a
        # delegated user. The worker must evaluate the parent's validated bytes.
        command = [str(binary), '--runtime', 'podman', '--podman-path', missing,
                   '--requirement', str(document), '--explain', '--json']
        if target_uid is not None:
            command += ['--context', 'uid:' + str(target_uid)]
        trace = output / (name + '.trace')
        if tracer is not None:
            if target_uid is not None:
                raise ValueError('delegated trace validation belongs to the credential harness')
            command = [str(tracer), '-f', '-q', '-s', '256', '-o', str(trace), '-e',
                       'trace=%file,%process,%network,fchmod,fchown,ftruncate,mount,umount2,setns,unshare', *command]
        result = subprocess.run(command, env={'PATH': '/usr/bin:/bin', 'LC_ALL': 'C', 'HOME': '/nonexistent'},
                                capture_output=True, check=False, timeout=45)
        (output / (name + '.json')).write_bytes(result.stdout)
        (output / (name + '.explanation.txt')).write_bytes(result.stderr)
        if result.returncode != expected:
            raise ValueError(name + ': unexpected requirement exit ' + str(result.returncode))
        report = json.loads(result.stdout)
        verify_report(report, uid, expected, 'passive')
        if target_uid is not None:
            evaluation = report['evaluation']
            if (evaluation['current']['uid'], evaluation['target']['uid'], evaluation['execution']['uid']) != (os.geteuid(), uid, uid):
                raise ValueError('delegated requirement lost caller or execution identity')
        if STATES[expected].encode() not in result.stderr or len(result.stderr) > 65536:
            raise ValueError('missing or unbounded requirement explanation')
        if tracer is not None:
            PASSIVE.verify_trace(trace.read_text(), binary)
        cases.append({'state': STATES[expected], 'exit': result.returncode, 'target_uid': uid,
                      'caller_uid': os.geteuid(), 'passive_trace': tracer is not None})
    (output / 'summary.json').write_text(json.dumps(cases, indent=2) + '\n')
    return cases


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--target-uid', type=int)
    parser.add_argument('--strace', type=Path)
    args = parser.parse_args()
    print(json.dumps(run_suite(args.binary, args.output, args.target_uid, args.strace)))


if __name__ == '__main__':
    main()
