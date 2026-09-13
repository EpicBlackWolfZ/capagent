import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('capture_prerequisites', ROOT / 'scripts/capture-podman-prerequisites.py')
CAPTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CAPTURE)


class PrerequisiteCaptureTests(unittest.TestCase):
    def report(self):
        report = json.loads((ROOT / 'testdata/fixtures/v1/assessment-rootless/expected.json').read_text())
        report['evaluation'].update(mode='live', collection='active', provenance='live')
        return report

    def test_exports_typed_measurements_and_redacts_home(self):
        report = self.report()
        report['runtimes']['podman']['raw_stdout'] = 'PRIVATE_CREDENTIAL'
        result = CAPTURE.project(report, 'synthetic capture-export test')
        text = json.dumps(result)
        self.assertNotIn('PRIVATE_CREDENTIAL', text)
        self.assertNotIn('/home/fixture-user', text)
        self.assertIn('/home/captured-user', text)
        self.assertEqual(result['info']['host']['ociRuntime']['path'], '/usr/bin/crun')
        for observation in result['observations']:
            for fact in observation['facts']:
                self.assertEqual(fact['scope'], observation['scope'])
                self.assertNotIn('raw_data', fact)

    def test_rejects_non_native_and_incomplete_inspection(self):
        for scenario in ('fixture', 'passive', 'unavailable', 'unknown'):
            report = self.report()
            if scenario == 'fixture':
                report['evaluation']['mode'] = 'fixture'
            elif scenario == 'passive':
                report['evaluation']['collection'] = 'passive'
            else:
                report['runtimes']['podman']['accessible'] = False if scenario == 'unavailable' else None
            with self.subTest(scenario=scenario), self.assertRaises(ValueError):
                CAPTURE.project(report, 'invalid test')
