"""Host collection validation must not mistake metadata for deployment readiness."""
import importlib.util
from pathlib import Path
import unittest

SOURCE = Path(__file__).resolve().parents[1] / "host-facts-smoke.py"
SPEC = importlib.util.spec_from_file_location("host_facts_smoke", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class HostReportTests(unittest.TestCase):
    def report(self):
        return {"schema_version": 1, "context": {"uid": 1000, "completeness": "complete"}, "host": {"kernel": "6.12", "completeness": "complete"},
                "runtimes": {}, "capabilities": {}, "evaluation": {"scope": {"runtime": "", "endpoint": ""},
                "evidence": [], "mode": "live", "collection": "passive", "observations": [
                    {"probe_id": name, "host": {}} for name in MODULE.HOST_PROBES]}}

    def test_complete_partial_and_no_verdict(self):
        report = self.report()
        self.assertEqual(MODULE.verify_report(report, 1000, "6.12"), 0)
        report["host"]["completeness"] = "partial"
        self.assertEqual(MODULE.verify_report(report, 1000, "6.12"), 2)
        report["evaluation"]["requirement"] = {"state": "SATISFIED"}
        with self.assertRaises(ValueError):
            MODULE.verify_report(report, 1000, "6.12")

    def test_mismatched_identity_or_missing_measurements(self):
        for mutation in (lambda r: r["context"].update(uid=0),
                         lambda r: r["evaluation"].update(observations=[]),
                         lambda r: r["evaluation"].update(collection="active"),
                         lambda r: r.update(schema_version=2)):
            report = self.report()
            mutation(report)
            with self.assertRaises(ValueError):
                MODULE.verify_report(report, 1000, "6.12")

    def test_network_and_security_attempts_rejected(self):
        initial = '1 execve("/capagent", [], 0x123) = 0\n'
        for operation in ('socket(AF_INET, SOCK_DGRAM, 0) = 3',
                          'prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) = 0',
                          'sendto(3, "query", 5, 0, NULL, 0) = -1 EPERM'):
            with self.subTest(operation=operation), self.assertRaises(ValueError):
                MODULE.verify_host_trace(initial + '1 ' + operation, Path('/capagent'))
        MODULE.verify_host_trace(initial + '1 prctl(PR_SET_VMA, PR_SET_VMA_ANON_NAME, 0x123, 4096, " Go: heap") = 0',
                                 Path('/capagent'))


if __name__ == '__main__':
    unittest.main()
