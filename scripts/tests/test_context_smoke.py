import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location('context_smoke', Path(__file__).resolve().parents[1] / 'context-smoke.py')
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)


class ContextSmokeTests(unittest.TestCase):
    def test_trace_rejects_authority_and_transport_failures(self):
        base = '10 execve("/capagent", [], []) = 0\n20 execve("/proc/self/fd/3", [], []) = 0\n20 setresuid(1000,1000,1000) = 0\n'
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


if __name__ == '__main__':
    unittest.main()
