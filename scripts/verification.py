"""Shared bounded execution and source identity for development verification."""
import hashlib
import json
import os
from pathlib import Path
import shlex
import signal
import subprocess
import time

import hardening


def command_text(command):
    return subprocess.check_output(command, text=True, timeout=15).strip()


def identity():
    paths = subprocess.check_output(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'], timeout=15).split(b'\0')
    digest = hashlib.sha256()
    for raw in sorted(set(paths) - {b'', b'.ignore'}):
        path = Path(os.fsdecode(raw))
        digest.update(raw + b'\0')
        digest.update(path.read_bytes() if path.is_file() else b'<missing>')
    changes = command_text(['git', 'status', '--porcelain'])
    return dict(commit=command_text(['git', 'rev-parse', 'HEAD']), source_sha256=digest.hexdigest(),
                dirty=any(line.strip() != '?? .ignore' for line in changes.splitlines()),
                go_version=command_text(['go', 'version']),
                run_id=os.environ.get('GITHUB_RUN_ID', os.environ.get('CAPAGENT_GATE_RUN', 'local')),
                run_attempt=os.environ.get('GITHUB_RUN_ATTEMPT', '1'))


def execute(command, output, timeout, env=None):
    output = Path(output)
    start = time.monotonic()
    error, code = '', 1
    print('Running ' + shlex.join(command), flush=True)
    try:
        code = hardening.run_command(command, output, dict(os.environ) if env is None else env, timeout)
    except (OSError, ValueError, TimeoutError, KeyboardInterrupt) as exc:
        error = str(exc) or 'interrupted'
    return dict(status='pass' if code == 0 and not error else 'fail', command=command,
                exit_code=code, error=error, elapsed_seconds=round(time.monotonic()-start, 3),
                stdout=str(output/'events.jsonl'), stderr=str(output/'stderr.log'))


def write_report(path, report):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(report, indent=2, sort_keys=True)+'\n', encoding='utf-8')
    temporary.replace(path)


def install_signals():
    def interrupted(_signal, _frame):
        raise KeyboardInterrupt('verification interrupted')
    signal.signal(signal.SIGTERM, interrupted)
