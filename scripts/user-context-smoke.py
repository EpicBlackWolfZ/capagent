#!/usr/bin/env python3
"""Verify opt-in user-manager queries on an explicitly ephemeral privileged CI host."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import subprocess
import time

SPEC = importlib.util.spec_from_file_location('contexts', Path(__file__).with_name('context-smoke.py'))
CONTEXTS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CONTEXTS)
ARGS = ['--user', '--no-pager', '--no-ask-password', 'show', '--property=Version', '--value']


def checked(command, **kwargs):
    return subprocess.run(command, check=True, capture_output=True, timeout=30, **kwargs)


def verify_active_trace(text, binary, uid, target=None):
    raw_text = CONTEXTS.joined_context_trace(text)
    text = CONTEXTS.TRACE.plain_fds(raw_text)
    socket = '/run/user/' + str(uid) + '/systemd/private'
    connections = re.findall(r'^\d+ connect\([^\n]*', text, re.M)
    if not connections:
        raise ValueError('active query has no observed local connection')
    if any('sa_family=AF_UNIX' not in line or 'sun_path="' + socket + '"' not in line for line in connections):
        raise ValueError('user query connected outside its declared local manager')
    queries = []
    for pid, executable, argv in re.findall(r'^(\d+) execve\("(/(?:usr/)?bin/systemctl)", (\[[^\n]*?\]),', text, re.M):
        args = json.loads(argv)
        if args == [executable, *ARGS]:
            queries.append(pid)
        elif args != [executable, '--version']:
            raise ValueError('unexpected systemctl operation')
    if len(queries) != 1:
        raise ValueError('missing or repeated manager query')
    query_pid = queries[0]
    if any(not line.startswith(query_pid + ' connect(') for line in connections):
        raise ValueError('connection outside the manager query process')
    bindings = re.findall(r'^\d+ bind\([^\n]*', text, re.M)
    if len(bindings) > 1:
        raise ValueError('repeated client socket binding')
    for line in bindings:
        # sd-bus may bind its stream client to a temporary abstract Unix name.
        # This creates no filesystem entry or listener; allow only the query's
        # own socket, subsequently connected to the declared manager.
        binding = re.fullmatch(query_pid + r' bind\((\d+), \{sa_family=AF_UNIX, '
                               r'sun_path=@"[0-9a-f]{16}/bus/systemctl/"\}, \d+\)\s+= 0', line)
        if binding is None:
            raise ValueError('unexpected user-query socket binding')
        fd = binding[1]
        created = re.search(r'^' + query_pid + r' socket\(AF_UNIX, SOCK_STREAM\|SOCK_CLOEXEC\|SOCK_NONBLOCK, 0\)\s+= '
                            + fd + r'$', text, re.M)
        if created is None or not any(connection.startswith(query_pid + ' connect(' + fd + ',') for connection in connections):
            raise ValueError('client binding is not the observed manager socket')
    fds = {re.match(r'^\d+ connect\((\d+),', line)[1] for line in connections}
    if len(fds) != 1:
        raise ValueError('query used more than one connected socket')
    CONTEXTS.verify_trace(raw_text, binary, target, (query_pid, next(iter(fds))))


def verify_user_report(report, active, uid):
    state = [row['user_context'] for row in report['evaluation']['observations'] if 'user_context' in row]
    if len(state) != 1:
        raise ValueError('missing user context')
    state = state[0]
    if state['runtime']['uid'] != uid or state['runtime']['mode'] != 0o700 or state['runtime']['valid'] is not True:
        raise ValueError('invalid supported runtime directory')
    if state['runtime']['source'] != 'default' or state['socket_valid'] is not True or state['linger_enabled'] is not True:
        raise ValueError('target environment, socket or linger mismatch')
    if state['query_attempted'] != active or state['accessible'] is not (True if active else None):
        raise ValueError('passive metadata confused with active accessibility')
    return state


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--user', required=True)
    parser.add_argument('--ephemeral-host', action='store_true')
    args = parser.parse_args()
    if not args.ephemeral_host or os.geteuid() != 0 or os.environ.get('GITHUB_ACTIONS') != 'true':
        raise SystemExit('explicit ephemeral privileged CI host required')
    account = pwd.getpwnam(args.user)
    if account.pw_uid == 0:
        raise SystemExit('rootless account required')
    binary = args.binary.resolve(strict=True)
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    unit = 'user@' + str(account.pw_uid) + '.service'
    runtime = '/run/user/' + str(account.pw_uid)
    linger = Path('/var/lib/systemd/linger') / account.pw_name
    was_lingering = linger.exists()
    was_running = subprocess.run(['/usr/bin/systemctl', 'is-active', '--quiet', unit], check=False, timeout=10).returncode == 0
    try:
        # Session preparation belongs to the test harness, outside every probe trace.
        if not was_lingering:
            checked(['/usr/bin/loginctl', 'enable-linger', account.pw_name])
        checked(['/usr/bin/systemctl', 'start', unit])
        deadline = time.monotonic() + 10
        while not Path(runtime + '/systemd/private').exists() and time.monotonic() < deadline:
            time.sleep(.05)
        environment = {'PATH': '/usr/bin:/bin', 'LC_ALL': 'C', 'HOME': '/wrong-launcher-home',
                       'XDG_RUNTIME_DIR': '/wrong-launcher-runtime', 'DBUS_SESSION_BUS_ADDRESS': 'tcp:host=127.0.0.1,port=9'}
        for active in (False, True):
            name = 'active' if active else 'passive'
            trace = output / (name + '.strace')
            command = ['/usr/bin/strace', *CONTEXTS.TRACE.TRACE_OPTIONS, '-o', str(trace), str(binary),
                       '--context=uid:' + str(account.pw_uid), '--json', *(['--active'] if active else [])]
            result = subprocess.run(command, env=environment, capture_output=True, timeout=55, check=False)
            if result.returncode not in (0, 2):
                raise ValueError('context query failed: ' + result.stderr.decode(errors='replace'))
            report = json.loads(result.stdout)
            CONTEXTS.verify_report(report, 0, account.pw_uid, CONTEXTS.local_groups(account))
            state = verify_user_report(report, active, account.pw_uid)
            if active:
                verify_active_trace(trace.read_text(), binary, account.pw_uid,
                                    {'uid': account.pw_uid, 'gid': account.pw_gid, 'groups': CONTEXTS.local_groups(account)})
                reference = checked(['/usr/sbin/runuser', '-u', account.pw_name, '--', '/usr/bin/env', '-i',
                                     'PATH=/usr/bin:/bin', 'LC_ALL=C', 'XDG_RUNTIME_DIR=' + runtime,
                                     'DBUS_SESSION_BUS_ADDRESS=unix:path=' + runtime + '/systemd/private',
                                     '/usr/bin/systemctl', *ARGS])
                if state['manager_version'] != reference.stdout.decode().strip():
                    raise ValueError('manager version differs from independent query')
            else:
                CONTEXTS.verify_trace(trace.read_text(), binary,
                                      {'uid': account.pw_uid, 'gid': account.pw_gid, 'groups': CONTEXTS.local_groups(account)})
            (output / (name + '.json')).write_bytes(result.stdout)
        (output / 'summary.json').write_text(json.dumps({'status': 'pass', 'uid': account.pw_uid,
                                                        'passive_connections': 0, 'active_query': True}) + '\n')
    finally:
        if not was_lingering:
            checked(['/usr/bin/loginctl', 'disable-linger', account.pw_name])
        if not was_running:
            checked(['/usr/bin/systemctl', 'stop', unit])


if __name__ == '__main__':
    main()
