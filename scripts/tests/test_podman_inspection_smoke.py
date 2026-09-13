"""Active smoke evidence must reject attempted transports and false success."""
import importlib.util
from pathlib import Path
import unittest

SOURCE = Path(__file__).resolve().parents[1] / "podman-inspection-smoke.py"
SPEC = importlib.util.spec_from_file_location("inspection_smoke", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class InspectionEvidenceTests(unittest.TestCase):
    def test_local_bus_is_distinct_from_remote_transport(self):
        MODULE.verify_connections('connect(3, {sa_family=AF_UNIX, sun_path="/run/dbus/system_bus_socket"}, 30) = -1 ENOENT', False)
        for text in ('connect(3, {sa_family=AF_INET}, 16) = -1 ECONNREFUSED',
                     'connect(3, {sa_family=AF_UNIX, sun_path="/run/podman/podman.sock"}, 20) = -1 ENOENT'):
            with self.subTest(text=text), self.assertRaises(RuntimeError):
                MODULE.verify_connections(text, False)

    def test_remote_rejection_requires_no_connection_attempt(self):
        MODULE.verify_connections('exit_group(125) = ?', True)
        with self.assertRaises(RuntimeError):
            MODULE.verify_connections('connect(3, {sa_family=AF_UNIX}, 20) = -1 ENOENT', True)

    def test_main_exit_must_belong_to_capagent(self):
        trace = '17 execve("/capagent", [], 0x1) = 0\n18 +++ exited with 0 +++\n'
        self.assertIsNone(MODULE.main_exit(trace, Path('/capagent')))
        self.assertEqual(MODULE.main_exit(trace + '17 +++ exited with 2 +++\n', Path('/capagent')), 2)

    def test_info_success_requires_both_predicates_and_identity(self):
        report = {"context": {"uid": 1000}, "evaluation": {"collection": "active", "mode": "live",
                  "requirement": {"state": "SATISFIED"}}, "capabilities": {
                      "runtime.podman": {"state": "supported"}, "runtime.podman.info": {"state": "supported"}}}
        MODULE.verify_report(report, 1000, 0)
        with self.assertRaises(RuntimeError):
            MODULE.verify_report(report, 0, 0)
        report['capabilities']['runtime.podman.info']['state'] = 'unknown'
        with self.assertRaises(RuntimeError):
            MODULE.verify_report(report, 1000, 0)


if __name__ == '__main__':
    unittest.main()
