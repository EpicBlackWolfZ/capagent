"""Qualification rejects misleading requirement exits, identity and output."""
import importlib.util
from pathlib import Path
import json
import os
import subprocess
import tempfile
from unittest import mock
import unittest

SPEC = importlib.util.spec_from_file_location('requirement_smoke', Path(__file__).resolve().parents[1] / 'requirement-smoke.py')
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class RequirementEvidenceTests(unittest.TestCase):
    def test_each_outcome_requires_matching_exit_and_scope(self):
        for code, state in enumerate(('SATISFIED', 'UNSATISFIED', 'INDETERMINATE')):
            report = {'context': {'uid': 1000}, 'evaluation': {
                'mode': 'live', 'collection': 'passive', 'requirement': {'state': state}}}
            MODULE.verify_report(report, 1000, code, 'passive')
            for uid, expected, collection in ((0, code, 'passive'), (1000, (code + 1) % 3, 'passive'),
                                               (1000, code, 'active')):
                with self.subTest(state=state, uid=uid, expected=expected), self.assertRaises(ValueError):
                    MODULE.verify_report(report, uid, expected, collection)

    def test_private_input_is_exported_only_after_execution(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / 'evidence'
            def run(command, **kwargs):
                document = Path(command[command.index('--requirement') + 1])
                self.assertEqual(document.stat().st_mode & 0o777, 0o600)
                self.assertEqual(output.stat().st_mode & 0o777, 0o700)
                name = document.name.split('.')[0].upper()
                code = MODULE.STATES.index(name)
                report = {'context': {'uid': os.geteuid()}, 'evaluation': {
                    'mode': 'live', 'collection': 'passive', 'requirement': {'state': name}}}
                return subprocess.CompletedProcess(command, code, json.dumps(report).encode(), name.encode())
            with mock.patch.object(MODULE.subprocess, 'run', side_effect=run):
                MODULE.run_suite(Path(__file__), output)
            self.assertEqual(output.stat().st_mode & 0o777, 0o755)
            for document in output.glob('*.requirement.json'):
                self.assertEqual(document.stat().st_mode & 0o777, 0o644)

    def test_failed_execution_still_exports_generated_evidence(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / 'evidence'
            with mock.patch.object(MODULE.subprocess, 'run', side_effect=RuntimeError('failed run')):
                with self.assertRaises(RuntimeError):
                    MODULE.run_suite(Path(__file__), output)
            self.assertEqual(output.stat().st_mode & 0o777, 0o755)
            self.assertEqual(next(output.glob('*.requirement.json')).stat().st_mode & 0o777, 0o644)

    def test_unsupported_exit_is_not_an_assessment(self):
        with self.assertRaises(ValueError):
            MODULE.verify_report({}, 1000, 70, 'passive')


if __name__ == '__main__':
    unittest.main()
