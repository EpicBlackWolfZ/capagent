"""Qualification rejects misleading requirement exits, identity and output."""
import importlib.util
from pathlib import Path
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

    def test_unsupported_exit_is_not_an_assessment(self):
        with self.assertRaises(ValueError):
            MODULE.verify_report({}, 1000, 70, 'passive')


if __name__ == '__main__':
    unittest.main()
