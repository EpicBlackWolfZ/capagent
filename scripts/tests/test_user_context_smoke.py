import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location('user_smoke', Path(__file__).resolve().parents[1] / 'user-context-smoke.py')
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)


class UserContextSmokeTests(unittest.TestCase):
    def test_active_trace_rejects_remote_and_mutating_queries(self):
        trace = ('10 execve("/capagent", [], []) = 0\n20 execve("/proc/self/fd/3", [], []) = 0\n'
                 '30 execve("/usr/bin/systemctl", ["/usr/bin/systemctl", "--user", "--no-pager", "--no-ask-password", '
                 '"show", "--property=Version", "--value"], []) = 0\n'
                 '30 connect(3, {sa_family=AF_UNIX, sun_path="/run/user/1000/systemd/private"}, 32) = 0\n')
        SMOKE.verify_active_trace(trace, Path('/capagent'), 1000)
        for changed in (trace.replace('AF_UNIX', 'AF_INET'), trace.replace('"show"', '"start"'),
                        trace.replace('/run/user/1000/', '/run/user/0/')):
            with self.assertRaises(ValueError):
                SMOKE.verify_active_trace(changed, Path('/capagent'), 1000)


if __name__ == '__main__':
    unittest.main()
