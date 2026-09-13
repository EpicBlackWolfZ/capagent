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

    def test_engine_capture_preserves_version_binding_and_redaction(self):
        report = self.report()
        report['evaluation']['target']['username'] = 'PRIVATE_USERNAME'
        result = CAPTURE.project(report, 'engine capture test', configuration=True)
        observations = result['observations']
        source = next(item['configuration'] for item in observations if 'configuration' in item)
        version = next(item for item in observations if item['id'] == source['version_source_id'])
        self.assertEqual(version['version']['Version']['canonical'], '5.8.4')
        self.assertIn('runtime.podman.config.engine.parsed', result['states'])
        self.assertIn('runtime.podman.cgroup_manager.systemd', result['states'])
        self.assertEqual(result['context']['identity']['target']['username'], 'captured-user')

    def test_storage_capture_retains_paths_and_reconstructs_only_selected_helper(self):
        report = json.loads((ROOT / 'testdata/fixtures/v1/storage-runtime-override/expected.json').read_text())
        report['evaluation'].update(mode='live', collection='active', provenance='live')
        report['runtimes']['podman']['raw_graph_options'] = {'secret': 'PRIVATE_CREDENTIAL'}
        result = CAPTURE.project(report, 'storage projection test', configuration=True)
        self.assertIn('runtime.podman.config.storage.parsed', result['states'])
        self.assertIn('runtime.podman.storage.roots.accessible', result['states'])
        self.assertTrue(any('storage_paths' in item for item in result['observations']))
        self.assertTrue(any('filesystems' in item.get('host', {}) for item in result['observations']))
        self.assertEqual(result['info']['store']['graphOptions']['overlay.mount_program'],
                         {'Executable': '/usr/bin/fuse-overlayfs'})
        self.assertNotIn('PRIVATE_CREDENTIAL', json.dumps(result))

    def test_network_capture_preserves_effective_choice_and_prerequisites(self):
        report = json.loads((ROOT / 'testdata/fixtures/v1/network-runtime-conflict/expected.json').read_text())
        report['evaluation'].update(mode='live', collection='active', provenance='live')
        result = CAPTURE.project(report, 'network projection test', configuration=True)
        self.assertEqual(result['info']['host']['rootlessNetworkCmd'], 'slirp4netns')
        self.assertEqual(result['states']['runtime.podman.network.backend.netavark'], 'unsupported')
        self.assertIn('runtime.podman.config.dns.parsed', result['states'])
        self.assertTrue(any(item.get('configuration', {}).get('family') == 'network' for item in result['observations']))
