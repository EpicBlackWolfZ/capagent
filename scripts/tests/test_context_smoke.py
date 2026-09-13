import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location('context_smoke', Path(__file__).resolve().parents[1] / 'context-smoke.py')
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)


class ContextSmokeTests(unittest.TestCase):
    def test_trace_rejects_authority_and_transport_failures(self):
        base = ('10 execve("/capagent", ["/capagent", "--context=uid:1000", "--json"], []) = 0\n'
                '10 clone(flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 20\n'
                '20 execve("/proc/self/fd/3", ["/proc/self/fd/3", "--internal-target-worker"], []) = 0\n'
                '20 setgroups(1, [1000]) = 0\n20 setresgid(1000, 1000, 1000) = 0\n'
                '20 setresuid(1000, 1000, 1000) = 0\n20 exit_group(0) = ?\n20 +++ exited with 0 +++\n'
                '10 exit_group(0) = ?\n10 +++ exited with 0 +++\n')
        SMOKE.verify_trace(base, Path('/capagent'))
        for suffix in ('10 setresuid(1,1,1) = 0\n', '20 connect(3, {}) = 0\n',
                       '20 openat(4, "bad", O_WRONLY) = 5\n', '20 execve("/bin/sh", [], []) = 0\n'):
            with self.subTest(suffix=suffix), self.assertRaises(ValueError):
                SMOKE.verify_trace(base + suffix, Path('/capagent'))
        with self.assertRaises(ValueError):
            SMOKE.verify_trace('', Path('/capagent'))

    def test_interrupted_exit_requires_known_thread_and_matching_exit_group(self):
        trace = '10 clone(flags=CLONE_THREAD) = 11\n10 exit_group(2) = ?\n11 ???( <unfinished ...>\n11 +++ exited with 2 +++\n'
        self.assertIn('exit_group(2)', SMOKE.joined_context_trace(trace))
        for changed in (trace.replace('exited with 2', 'exited with 0'),
                        trace.replace('10 exit_group(2) = ?\n', ''),
                        trace.replace('10 clone(flags=CLONE_THREAD) = 11\n', ''),
                        trace.replace('???(', 'openat(')):
            with self.assertRaises((ValueError, RuntimeError)):
                SMOKE.joined_context_trace(changed)

    def test_thread_group_exit_can_interrupt_the_group_leader(self):
        trace = ('10 clone(flags=CLONE_THREAD) = 11\n11 clone(flags=CLONE_THREAD) = 12\n'
                 '12 exit_group(0) = ?\n10 ???( <unfinished ...>\n10 +++ exited with 0 +++\n')
        self.assertIn('exit_group(0)', SMOKE.joined_context_trace(trace))
        for changed in (trace.replace('exited with 0', 'exited with 2'),
                        trace.replace('10 clone(flags=CLONE_THREAD) = 11', '10 clone(flags=SIGCHLD) = 11'),
                        trace.replace('12 exit_group', '20 exit_group'),
                        trace.replace('???(', 'openat(')):
            with self.subTest(trace=changed), self.assertRaises((ValueError, RuntimeError)):
                SMOKE.joined_context_trace(changed)


    def test_demonstrated_passivity_bypasses(self):
        base = ('10 execve("/capagent", ["/capagent", "--context=uid:1000", "--json"], []) = 0\n'
                '10 clone(child_stack=NULL, flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 20\n'
                '20 execve("/proc/self/fd/3", ["/proc/self/fd/3", "--internal-target-worker"], []) = 0\n'
                '20 setgroups(1, [1000]) = 0\n20 setresgid(1000, 1000, 1000) = 0\n'
                '20 setresuid(1000, 1000, 1000) = 0\n20 exit_group(0) = ?\n20 +++ exited with 0 +++\n'
                '10 exit_group(0) = ?\n10 +++ exited with 0 +++\n')
        SMOKE.verify_trace(base, Path('/capagent'))
        for suffix in (
                '20 fchmodat(3, "file", 0600) = 0\n',
                '20 execve("/usr/bin/systemctl", ["/usr/bin/systemctl", "start", "example.service"], []) = 0\n',
                '20 clone(child_stack=NULL, flags=CLONE_NEWUSER|SIGCHLD) = 30\n',
                '20 sendto(3, "payload", 7, 0, {sa_family=AF_INET, sin_port=htons(9)}, 16) = 7\n',
                '20 fchmod(3, 0600) = 0\n',
                '20 ftruncate(3, 0) = 0\n',
                '20 setns(3, CLONE_NEWNET) = 0\n',
                '20 sendmsg(3, {}, 0) = 1\n',
                '20 pwrite64(3, "x", 1, 0) = 1\n',
                '20 clone(flags=CLONE_NEWNET|CLONE_THREAD) = 30\n',
                '20 execveat(3, "", ["shell"], [], AT_EMPTY_PATH) = 0\n',
                '10 setuid(1000) = 0\n',
                '20 setresuid(0, 0, 0) = 0\n'):
            with self.subTest(suffix=suffix), self.assertRaises(ValueError):
                SMOKE.verify_trace(base + suffix, Path('/capagent'))

    def test_resumed_syscall_identity_cannot_be_substituted(self):
        for trace in ('20 fchmodat(3, "file", <unfinished ...>\n20 <... read resumed>0600) = 0\n',
                      '20 read(3, <unfinished ...>\n20 read(4, <unfinished ...>\n20 <... read resumed>0) = 0\n'):
            with self.subTest(trace=trace), self.assertRaises((ValueError, RuntimeError)):
                SMOKE.joined_context_trace(trace)


if __name__ == '__main__':
    unittest.main()
