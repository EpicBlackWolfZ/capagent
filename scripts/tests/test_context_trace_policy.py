import importlib.util
import json
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location('smoke', Path(__file__).resolve().parents[1] / 'context-smoke.py')
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)
ROOT = Path(__file__).with_name('fixtures') / 'context-trace'
TARGET = dict(uid=1000, gid=1000, groups=[1000])
BASE = ('10 execve("/capagent", ["/capagent", "--context=uid:1000", "--json"], []) = 0\n'
        '10 clone(flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 20\n'
        '20 execve("/proc/self/fd/3", ["/proc/self/fd/3", "--internal-target-worker"], []) = 0\n'
        '20 setgroups(1, [1000]) = 0\n20 setresgid(1000, 1000, 1000) = 0\n'
        '20 setresuid(1000, 1000, 1000) = 0\n')


def verify(trace, binary, target):
    # Supply explicit terminal records for these synthetic operation cases.
    text = SMOKE.joined_context_trace(trace)
    rows = SMOKE.TRACE.records(SMOKE.TRACE.plain_fds(text))
    parents, threads, _ = SMOKE.TRACE.lineage(rows, '10')
    for pid in {'10', *parents}:
        owner = SMOKE.TRACE.group(pid, parents, threads)
        if pid == owner and not any(p == pid and call == 'exit_group' for p, call, _, _ in rows):
            trace += pid + ' exit_group(0) = ?\n'
        trace += pid + ' +++ exited with 0 +++\n'
    SMOKE.verify_trace(trace, binary, target)


class ContextTracePolicyTests(unittest.TestCase):
    def test_authentic_delegated_trace(self):
        provenance = json.loads((ROOT / 'provenance.json').read_text())
        self.assertEqual(provenance['kind'], 'captured')
        SMOKE.verify_trace((ROOT / 'delegated.strace').read_text(), Path(provenance['binary']), provenance['target'])

    def test_negative_corpus(self):
        verify(BASE, Path('/capagent'), TARGET)
        for name, suffix in json.loads((ROOT / 'negative.json').read_text()).items():
            with self.subTest(case=name), self.assertRaises(ValueError):
                verify(BASE + suffix + '\n', Path('/capagent'), TARGET)

    def test_exact_arguments_lineage_and_identity(self):
        for trace in (
                BASE.replace('"--internal-target-worker"', '"--internal-target-worker", "--active"'),
                BASE.replace('"--json"', '"--json", "--active"'),
                BASE.replace('10 clone', '20 clone'),
                BASE.replace('setgroups(1, [1000])', 'setgroups(0, [])'),
                BASE.replace('setresgid(1000, 1000, 1000)', 'setresgid(0, 0, 0)'),
                BASE.replace('setresuid(1000, 1000, 1000)', 'setresuid(1000, 0, 1000)'),
                BASE.replace('20 setresuid(1000, 1000, 1000) = 0\n', ''),
                BASE + '20 setresuid(1000, 1000, 1000) = 0\n',
                BASE + '20 clone(flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 30\n'
                       '30 execve("/usr/bin/systemctl", ["/usr/bin/systemctl", "start", "unit"], []) = 0\n'):
            with self.subTest(trace=trace), self.assertRaises(ValueError):
                verify(trace, Path('/capagent'), TARGET)

    def test_normal_threads_metadata_and_transport(self):
        metadata = ('20 clone(flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 30\n'
                    '30 execve("/usr/bin/systemctl", ["/usr/bin/systemctl", "--version"], []) = 0\n'
                    '30 write(1<pipe:[123]>, "systemd 258\\n", 12) = 12\n')
        runtime = ('20 clone(flags=CLONE_VM|CLONE_FILES|CLONE_SIGHAND|CLONE_THREAD) = 21\n'
                   '21 eventfd2(0, EFD_CLOEXEC|EFD_NONBLOCK) = 6<{eventfd-count=0, eventfd-id=212, eventfd-semaphore=0}>\n'
                   '21 write(6<{eventfd-count=0, eventfd-id=212, eventfd-semaphore=0}>, "\\1\\0\\0\\0\\0\\0\\0\\0", 8) = 8\n'
                   '21 fcntl(6</dev/null<char 1:3>>, F_GETFL) = 0x8000 (flags O_RDONLY|O_LARGEFILE)\n')
        verify(BASE + metadata + runtime, Path('/capagent'), TARGET)
        legacy = runtime.replace('{eventfd-count=0, eventfd-id=212, eventfd-semaphore=0}', 'anon_inode:[eventfd]')
        legacy = legacy.replace('21 write(', '20 write(')
        verify(BASE + metadata + legacy, Path('/capagent'), TARGET)
        before = BASE.index('20 setgroups')
        thread = '20 clone(flags=CLONE_VM|CLONE_FILES|CLONE_SIGHAND|CLONE_THREAD) = 21\n'
        drops = BASE[before:].replace('20 ', '21 ')
        verify(BASE[:before] + thread + drops + BASE[before:], Path('/capagent'), TARGET)
        with self.assertRaises(ValueError):
            verify(BASE[:before] + thread + drops.replace('1000, 1000, 1000', '0, 0, 0') + BASE[before:],
                               Path('/capagent'), TARGET)

    def test_pidfd_probe_cannot_hide_execution(self):
        probe = '10 clone(flags=CLONE_VM|CLONE_VFORK|CLONE_PIDFD) = 19\n19 exit_group(0) = ?\n'
        first, rest = BASE.split('\n', 1)
        verify(first + '\n' + probe + rest, Path('/capagent'), TARGET)
        with self.assertRaises(ValueError):
            verify(first + '\n' + probe.replace('19 exit_group', '19 openat(3, "file", O_RDONLY) = 4\n19 exit_group')
                               + rest, Path('/capagent'), TARGET)

    def test_capture_inventory_includes_descriptor_variants(self):
        calls = SMOKE.TRACE.TRACE_CALLS.split(',')
        for call in ('%file', '%process', '%network', '%creds', '%memory', 'fchmod', 'fchown', 'ftruncate',
                     'fallocate', 'write', 'writev', 'pwrite64', 'pwritev', 'pwritev2', 'ioctl', 'fcntl',
                     'fsetxattr', 'fremovexattr', 'copy_file_range', 'io_uring_setup', 'setns', 'unshare',
                     'fchown32', 'ftruncate64', 'fcntl64', 'sendfile64'):
            self.assertIn(call, calls)
        self.assertIn('-yy', SMOKE.TRACE.TRACE_OPTIONS)


    def test_missing_exit_and_signal_mutations(self):
        with self.assertRaises(ValueError):
            SMOKE.verify_trace(BASE, Path('/capagent'), TARGET)
        for suffix in ('20 kill(1, SIGKILL) = 0\n', '20 tgkill(10, 10, SIGURG) = 0\n',
                       '10 clone(flags=CLONE_THREAD) = 11\n11 tgkill(10, 10, SIGRT_1) = 0\n'):
            with self.subTest(suffix=suffix), self.assertRaises(ValueError):
                verify(BASE + suffix, Path('/capagent'), TARGET)


if __name__ == '__main__':
    unittest.main()
