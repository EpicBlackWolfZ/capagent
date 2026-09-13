#!/usr/bin/env python3
"""Verify current/root/delegated identities and cancellation on an ephemeral CI host."""
import argparse
import json
import importlib.util
import os
from pathlib import Path
import pwd
import signal
import stat
import subprocess
import tempfile
import time


SPEC = importlib.util.spec_from_file_location("passive", Path(__file__).with_name("podman-discovery-smoke.py"))
PASSIVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PASSIVE)


def local_groups(account):
    groups = {account.pw_gid}
    for line in Path('/etc/group').read_text().splitlines():
        fields = line.split(':')
        if len(fields) == 4 and account.pw_name in fields[3].split(','):
            groups.add(int(fields[2]))
    return sorted(groups)


def verify_report(report, current, target, groups):
    trace = report['evaluation']
    if (trace['current']['uid'], trace['target']['uid'], trace['execution']['uid']) != (current, target, target):
        raise ValueError('execution identity was confused with launcher or target')
    if report['context']['uid'] != target or sorted(trace['execution']['supplementary_groups']) != groups:
        raise ValueError('target credential set mismatch')
    if trace['scope']['context_id'] != trace['selection'] or not trace['execution']['groups_known']:
        raise ValueError('target scope or completeness mismatch')
    if any(row.get('authority') != 'execution' for row in trace['observations']):
        raise ValueError('missing measurement authority')
    observed = {row['kind']: row['id'] for row in trace['namespaces']}
    expected = {kind: os.readlink('/proc/self/ns/' + kind) for kind in ('user', 'pid', 'net', 'mnt', 'ipc', 'uts', 'cgroup')}
    if observed != expected:
        raise ValueError('delegation changed namespaces')



def verify_subids(report, account):
    observations = [row['subids'] for row in report['evaluation']['observations'] if 'subids' in row]
    if len(observations) != 1:
        raise ValueError('missing subordinate ID observation')
    subids = observations[0]
    for pool, path in [('uid', '/etc/subuid'), ('gid', '/etc/subgid')]:
        expected = []
        for line in Path(path).read_text().splitlines():
            if not line or line.startswith('#'):
                continue
            owner, start, length = line.split(':')
            if owner == account.pw_name or owner.isdecimal() and int(owner) == account.pw_uid:
                expected.append({'start': int(start), 'length': int(length)})
        allocation = subids[pool]
        if not expected or allocation['ranges'] != expected or allocation['total'] != sum(r['length'] for r in expected):
            raise ValueError('rootless allocation differs from independent local-file observation')
    for helper in subids['helpers']:
        if helper['path'] not in [prefix + helper['name'] for prefix in ['/usr/bin/', '/usr/local/bin/', '/bin/']]:
            raise ValueError('mapping helper path escaped fixed discovery')
        metadata = Path(helper['path']).stat()
        if helper['uid'] != metadata.st_uid or helper['mode'] != stat.S_IMODE(metadata.st_mode) & 0o777:
            raise ValueError('mapping helper metadata mismatch')
        if helper['setuid'] != bool(metadata.st_mode & stat.S_ISUID) or helper['usable'] is True:
            raise ValueError('mapping privilege metadata was lost or overstated')


def joined_context_trace(text):
    return PASSIVE.joined_trace(text)


TRACE_SPEC = importlib.util.spec_from_file_location("context_trace", Path(__file__).with_name("context_trace.py"))
TRACE = importlib.util.module_from_spec(TRACE_SPEC)
TRACE_SPEC.loader.exec_module(TRACE)


def verify_trace(text, binary, target=None, query=None):
    TRACE.verify(joined_context_trace(text), binary, target, query)


def run_case(binary, selector, environment, trace=None):
    command = [str(binary), '--context=' + selector, '--json']
    if trace:
        command = ['/usr/bin/strace', *TRACE.TRACE_OPTIONS, '-o', str(trace), *command]
    result = subprocess.run(command, env=environment, capture_output=True, timeout=55, check=False)
    if result.returncode not in (0, 2):
        raise ValueError('context collection failed: ' + result.stderr.decode(errors='replace'))
    return json.loads(result.stdout)


def verify_cancellation(binary, account, output):
    with tempfile.TemporaryDirectory(prefix='capagent-context-') as directory:
        root = Path(directory)
        os.chown(root, account.pw_uid, account.pw_gid)
        root.chmod(0o700)
        marker = root / 'pid'
        helper = root / 'podman'
        helper.write_text('#!/usr/bin/python3\nimport os,time\n' +
                          'with open(' + repr(str(marker)) + ', "w") as f: f.write(str(os.getpid()))\n' +
                          'time.sleep(60)\n')
        helper.chmod(0o755)
        command = [str(binary), '--context=uid:' + str(account.pw_uid), '--runtime=podman', '--active', '--podman-path=' + str(helper)]
        with subprocess.Popen(command, env={'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'},
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE) as process:
            try:
                deadline = time.monotonic() + 10
                while not marker.exists() and time.monotonic() < deadline:
                    if process.poll() is not None:
                        raise ValueError('delegated runtime did not start')
                    time.sleep(.05)
                # The marker is tiny but may be observed between creation and writing.
                while marker.exists() and not marker.read_text() and time.monotonic() < deadline:
                    time.sleep(.01)
                if not marker.exists() or not marker.read_text():
                    raise ValueError('missing runtime readiness')
                child = int(marker.read_text())
                process.send_signal(signal.SIGTERM)
                stdout, stderr = process.communicate(timeout=5)
                if Path('/proc/' + str(child)).exists():
                    raise ValueError('cancelled worker left its direct runtime process')
                (output / 'cancellation.json').write_text(json.dumps({'exit': process.returncode, 'runtime_reaped': True}))
            finally:
                if process.poll() is None:
                    process.kill()
                    process.communicate(timeout=5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--user', required=True)
    parser.add_argument('--packaged', action='store_true')
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise SystemExit('context smoke requires an ephemeral privileged CI host')
    binary = args.binary.resolve(strict=True)
    account = pwd.getpwnam(args.user)
    if account.pw_uid == 0:
        raise SystemExit('rootless target required')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    environment = {'PATH': '/usr/bin:/bin', 'LC_ALL': 'C', 'HOME': '/deliberately-wrong-launcher-home',
                   'XDG_RUNTIME_DIR': '/deliberately-wrong-launcher-runtime', 'DBUS_SESSION_BUS_ADDRESS': 'tcp:host=127.0.0.1,port=9'}
    with tempfile.TemporaryDirectory(prefix='capagent-package-cache-') as cache:
        for execution in ('memfd', 'cache') if args.packaged else ('native',):
            if args.packaged:
                environment.update(MICROFAT_EXEC_MODE=execution, MICROFAT_CACHE_DIR=cache)
            for kind, selector in (('username', 'user:' + account.pw_name), ('numeric', 'uid:' + str(account.pw_uid))):
                trace = None if args.packaged else output / (kind + '.strace')
                report = run_case(binary, selector, environment, trace)
                verify_report(report, 0, account.pw_uid, local_groups(account))
                if not args.packaged:
                    verify_subids(report, account)
                (output / (execution + '-' + kind + '.json')).write_text(json.dumps(report, indent=2) + '\n')
                if trace:
                    verify_trace(trace.read_text(), binary, {'uid': account.pw_uid, 'gid': account.pw_gid, 'groups': local_groups(account)})
    if not args.packaged:
        # Runtime directory setup belongs to the test harness, never the probe.
        runtime = Path('/run/user/' + str(account.pw_uid))
        if not runtime.exists():
            runtime.mkdir(mode=0o700)
            os.chown(runtime, account.pw_uid, account.pw_gid)
        verify_cancellation(binary, account, output)
    (output / 'summary.json').write_text(json.dumps({'status': 'pass', 'target_uid': account.pw_uid,
                                                   'cases': ['username', 'numeric'], 'packaged': args.packaged}) + '\n')


if __name__ == '__main__':
    main()
