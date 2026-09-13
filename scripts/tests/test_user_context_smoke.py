import importlib.util
import json
import re
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location('user_smoke', Path(__file__).resolve().parents[1] / 'user-context-smoke.py')
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)


class UserContextSmokeTests(unittest.TestCase):
    def test_active_trace_allows_only_query_client_abstract_bind(self):
        trace = ('10 execve("/capagent", ["/capagent", "--context=uid:1000", "--json", "--active"], []) = 0\n10 clone(flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 20\n20 execve("/proc/self/fd/3", ["/proc/self/fd/3", "--internal-target-worker"], []) = 0\n20 setgroups(1, [1000]) = 0\n20 setresgid(1000, 1000, 1000) = 0\n20 setresuid(1000, 1000, 1000) = 0\n20 clone(flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 30\n'
                 '30 execve("/usr/bin/systemctl", ["/usr/bin/systemctl", "--user", "--no-pager", "--no-ask-password", '
                 '"show", "--property=Version", "--value"], []) = 0\n'
                 '30 socket(AF_UNIX, SOCK_STREAM|SOCK_CLOEXEC|SOCK_NONBLOCK, 0) = 3\n'
                 '30 bind(3, {sa_family=AF_UNIX, sun_path=@"4accc3dfd8af342a/bus/systemctl/"}, 34) = 0\n'
                 '30 connect(3, {sa_family=AF_UNIX, sun_path="/run/user/1000/systemd/private"}, 32) = 0\n30 exit_group(0) = ?\n30 +++ exited with 0 +++\n'
                 '20 exit_group(0) = ?\n20 +++ exited with 0 +++\n10 exit_group(0) = ?\n10 +++ exited with 0 +++\n')
        SMOKE.verify_active_trace(trace, Path('/capagent'), 1000)
        for changed in (trace.replace('30 bind', '20 bind'), trace.replace('bind(3', 'bind(4'),
                        trace.replace('sun_path=@"', 'sun_path="'), trace.replace('/bus/systemctl/', '/other/'),
                        trace.replace('AF_UNIX', 'AF_INET'), trace.replace('SOCK_STREAM', 'SOCK_DGRAM'),
                        trace.replace('30 connect', '20 connect'), trace + '30 listen(3, 1) = 0\n',
                        trace + '30 bind(3, {sa_family=AF_UNIX, sun_path=@"4accc3dfd8af342a/bus/systemctl/"}, 34) = 0\n'):
            with self.subTest(trace=changed), self.assertRaises(ValueError):
                SMOKE.verify_active_trace(changed, Path('/capagent'), 1000)

    def test_active_trace_rejects_remote_and_mutating_queries(self):
        trace = ('10 execve("/capagent", ["/capagent", "--context=uid:1000", "--json", "--active"], []) = 0\n10 clone(flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 20\n20 execve("/proc/self/fd/3", ["/proc/self/fd/3", "--internal-target-worker"], []) = 0\n20 setgroups(1, [1000]) = 0\n20 setresgid(1000, 1000, 1000) = 0\n20 setresuid(1000, 1000, 1000) = 0\n20 clone(flags=CLONE_VM|CLONE_VFORK|SIGCHLD) = 30\n'
                 '30 execve("/usr/bin/systemctl", ["/usr/bin/systemctl", "--user", "--no-pager", "--no-ask-password", '
                 '"show", "--property=Version", "--value"], []) = 0\n'
                 '30 connect(3, {sa_family=AF_UNIX, sun_path="/run/user/1000/systemd/private"}, 32) = 0\n30 exit_group(0) = ?\n30 +++ exited with 0 +++\n'
                 '20 exit_group(0) = ?\n20 +++ exited with 0 +++\n10 exit_group(0) = ?\n10 +++ exited with 0 +++\n')
        SMOKE.verify_active_trace(trace, Path('/capagent'), 1000)
        for changed in (trace.replace('AF_UNIX', 'AF_INET'), trace.replace('"show"', '"start"'),
                        trace.replace('/run/user/1000/', '/run/user/0/')):
            with self.assertRaises(ValueError):
                SMOKE.verify_active_trace(changed, Path('/capagent'), 1000)


class UserQueryNonceTests(unittest.TestCase):
    def test_native_short_nonce_and_bounded_widths(self):
        fixture = Path(__file__).parent / 'fixtures/context-trace'
        trace = (fixture / 'active-short-nonce.strace').read_text()
        target = json.loads((fixture / 'active-short-nonce-provenance.json').read_text())
        for nonce in ('0', 'a', '64b6565d8dfaa4a', 'ffffffffffffffff', '', 'f' * 17, 'nothex'):
            changed = trace.replace('64b6565d8dfaa4a/bus/systemctl/', nonce + '/bus/systemctl/')
            changed = re.sub(r'(bind\(.*sun_path=@"[^"\n]+"\}, )\d+',
                             lambda match: match[1] + str(18 + len(nonce)), changed)
            with self.subTest(nonce=nonce):
                if nonce and len(nonce) <= 16 and all(char in '0123456789abcdef' for char in nonce):
                    SMOKE.verify_active_trace(changed, Path('/capagent'), target['uid'], target)
                else:
                    with self.assertRaises(ValueError):
                        SMOKE.verify_active_trace(changed, Path('/capagent'), target['uid'], target)
        changed = re.sub(r'(bind\(.*sun_path=@"[^"\n]+"\}, )\d+', r'\g<1>34', trace)
        with self.assertRaises(ValueError):
            SMOKE.verify_active_trace(changed, Path('/capagent'), target['uid'], target)


if __name__ == '__main__':
    unittest.main()
