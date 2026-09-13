"""Failure-oriented checks for fuzz inventory, corpus evidence and supervision."""
import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
import fuzzing


class FuzzingTests(unittest.TestCase):
    def test_inventory_is_exact_and_has_permanent_seeds(self):
        inventory = fuzzing.inventory(SCRIPTS.parent)
        self.assertEqual(len(inventory), 14)
        self.assertTrue(all(row['corpus'] for row in inventory))
        self.assertTrue(all(row['input_bytes'] > 0 for row in inventory))

    def test_removed_target_and_corpus_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                fuzzing.inventory(Path(directory))

    def test_corpus_requires_target_pass_and_package_pass(self):
        target = next(iter(fuzzing.TARGETS))
        package = fuzzing.MODULE + '/' + fuzzing.TARGETS[target][0]
        events = [dict(Action='pass', Package=package, Test=target), dict(Action='pass', Package=package)]
        fuzzing.verify_events(events, [target])
        for mutation in ('missing', 'skip', 'fail', 'duplicate', 'no-package'):
            changed = [dict(e) for e in events]
            if mutation == 'missing': changed = changed[1:]
            if mutation in ('skip', 'fail'): changed[0]['Action'] = mutation
            if mutation == 'duplicate': changed.append(dict(changed[0]))
            if mutation == 'no-package': changed = changed[:1]
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                fuzzing.verify_events(changed, [target])

    def test_supervisor_retains_failures_and_bounds_output(self):
        with tempfile.TemporaryDirectory() as directory:
            result = fuzzing.execute([sys.executable, '-c', 'raise SystemExit(2)'], Path(directory)/'fail', timeout=2)
            self.assertEqual(result['status'], 'fail')
            result = fuzzing.execute([sys.executable, '-c', 'import time;time.sleep(60)'], Path(directory)/'hang', timeout=.05)
            self.assertEqual(result['status'], 'fail')
            self.assertIn('watchdog', result['error'])


if __name__ == '__main__':
    unittest.main()

class CorpusFailureTests(unittest.TestCase):
    def test_missing_corpus_is_rejected_after_target_discovery(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, (package, _) in fuzzing.TARGETS.items():
                folder = root/package
                folder.mkdir(parents=True, exist_ok=True)
                with (folder/'fixture_test.go').open('a') as source:
                    source.write('func '+name+'(f *testing.F) {}\n')
                corpus = folder/'testdata/fuzz'/name
                corpus.mkdir(parents=True)
                (corpus/'seed').write_text('go test fuzz v1\n[]byte("a")\n')
            fuzzing.inventory(root)
            (root/'internal/platform/testdata/fuzz/FuzzMountinfo/seed').unlink()
            with self.assertRaisesRegex(ValueError, 'missing permanent corpus'):
                fuzzing.inventory(root)

    def test_output_overflow_is_failure_with_bounded_retained_bytes(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory, patch.object(fuzzing.hardening, 'EVENT_LIMIT', 8):
            result = fuzzing.execute([sys.executable, '-c', 'print("x"*100)'], Path(directory), timeout=2)
            self.assertEqual(result['status'], 'fail')
            self.assertEqual(Path(result['stdout']).stat().st_size, 8)

class FuzzTemporaryOwnershipTests(unittest.TestCase):
    def test_worker_failure_does_not_leave_temporary_fixture_tree(self):
        from unittest.mock import patch
        root = SCRIPTS.parent
        observed = []
        def failed_worker(_command, output, timeout, env):
            self.assertGreater(timeout, 0)
            temporary = Path(env['TMPDIR'])
            self.assertEqual(env['GOTMPDIR'], str(temporary))
            self.assertTrue(temporary.is_relative_to(output.parent.resolve()))
            (temporary/'orphaned-worker-fixture').write_text('synthetic')
            observed.append(temporary)
            return dict(status='fail', exit_code=2, error='synthetic worker crash')
        with tempfile.TemporaryDirectory() as directory, patch.object(fuzzing, 'execute', failed_worker):
            report = fuzzing.run('smoke', Path(directory)/'fuzz', 'FuzzMountinfo')
            self.assertEqual(report['status'], 'fail')
            self.assertTrue(observed)
            self.assertFalse(any(path.exists() for path in observed))
