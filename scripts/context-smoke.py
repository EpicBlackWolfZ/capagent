#!/usr/bin/env python3
"""Verify current/root/delegated identities and cancellation on an ephemeral CI host."""
import argparse
import json
import importlib.util
import os
from pathlib import Path
import pwd
import re
import signal
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


def verify_trace(text, binary):
    text = PASSIVE.joined_trace(text)
    # No container/namespace creation, host mutation, user-manager connection, or arbitrary executable.
    if re.search(r'\b(?:connect|bind|listen|unshare|setns|mount|mkdir(?:at)?|unlink(?:at)?|rename(?:at2?)?|chmod|chown)\(', text):
        raise ValueError('passive delegation changed host state or connected to a service')
    if re.search(r'\bO_(?:WRONLY|RDWR|CREAT|TRUNC|APPEND)\b', text):
        raise ValueError('passive delegation opened writable files')
    executions = re.findall(r'execve\("([^"\n]+)"', text)
    if not executions or executions[0] != str(binary) or executions.count('/proc/self/fd/3') != 1:
        raise ValueError('worker did not re-execute its pinned payload')
    if any(path not in (str(binary), '/proc/self/fd/3', '/usr/bin/systemctl', '/bin/systemctl') for path in executions):
        raise ValueError('unexpected worker executable')
    launcher = re.search(r'^(\d+)\s+execve\(', text, re.M)
    if launcher is None:
        raise ValueError('missing launching process')
    threads = {launcher[1]}
    edges = re.findall(r'^(\d+)\s+clone3?\(.*CLONE_THREAD.*\)\s+= (\d+)$', text, re.M)
    while True:
        expanded = threads | {child for parent, child in edges if parent in threads}
        if expanded == threads:
            break
        threads = expanded
    for line in text.splitlines():
        match = re.match(r'(\d+)\s+(?:setgroups|setresgid|setresuid)\(', line)
        if match and match[1] in threads:
            raise ValueError('launcher credentials changed')


def run_case(binary, selector, environment, trace=None):
    command = [str(binary), '--context=' + selector, '--json']
    if trace:
        command = ['/usr/bin/strace', '-f', '-q', '-I', '2', '-s', '320', '-o', str(trace),
                   '-e', 'trace=%process,%file,%network,prctl,setgroups,setresgid,setresuid', *command]
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
    for kind, selector in (('username', 'user:' + account.pw_name), ('numeric', 'uid:' + str(account.pw_uid))):
        trace = None if args.packaged else output / (kind + '.strace')
        report = run_case(binary, selector, environment, trace)
        verify_report(report, 0, account.pw_uid, local_groups(account))
        (output / (kind + '.json')).write_text(json.dumps(report, indent=2) + '\n')
        if trace:
            verify_trace(trace.read_text(), binary)
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
