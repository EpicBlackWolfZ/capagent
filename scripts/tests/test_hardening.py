"""Failure-oriented contracts for the actual hardening report and runner."""
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

MODULE = Path(__file__).resolve().parents[1] / 'hardening.py'
spec = importlib.util.spec_from_file_location('capagent_hardening', MODULE)
hardening = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = hardening
spec.loader.exec_module(hardening)


class HardeningReportTests(unittest.TestCase):
    def events(self):
        events = [dict(Action='pass', Package=package, Test=name)
                  for package, names in hardening.REQUIRED.items() for name in names]
        package = hardening.MODULE + '/tests/contract'
        records = [dict(kind='campaign_start', config=dict(Seed=1, Iterations=2, Probes=64, Concurrency=4), duration='1m0s'),
                   dict(kind='chaos_begin', iteration=0),
                   dict(kind='chaos_case', iteration=0, seed=1, profile='filesystem'),
                   dict(kind='chaos_begin', iteration=1),
                   dict(kind='chaos_case', iteration=1, seed=1, profile='command'),
                   dict(kind='campaign_complete', iterations=2, duration_ns=100)]
        records += [dict(kind='resource_case', scenario=scenario, workers=workers) for scenario, workers in hardening.RESOURCE_CASES]
        events += [dict(Action='output', Package=package, Test='TestChaosCampaign',
                        Output='    HARDENING ' + json.dumps(record) + '\n') for record in records]
        return events

    def report(self, events=None, code=0):
        return hardening.summarize(self.events() if events is None else events, 'a' * 40, 'go1.27.1', code)

    def test_complete_inventory_passes(self):
        report = self.report()
        self.assertEqual(report['status'], 'pass')
        self.assertEqual(report['campaign']['completed_iterations'], 2)
        self.assertIn('CHAOS_SEED=1', report['campaign']['replay'])

    def test_missing_required_case_is_failure(self):
        self.assertEqual(self.report(self.events()[1:])['status'], 'fail')

    def test_skip_fail_and_later_pass_never_hide_failure(self):
        for action in ('skip', 'fail'):
            events = self.events()
            events.insert(0, dict(events[0], Action=action))
            self.assertEqual(self.report(events)['status'], 'fail')

    def test_campaign_completion_and_iterations_are_required(self):
        for kind in ('campaign_start', 'campaign_complete', 'chaos_case'):
            events = [e for e in self.events() if kind not in e.get('Output', '')]
            self.assertEqual(self.report(events)['status'], 'fail')

    def test_helper_crash_or_process_timeout_is_failure(self):
        self.assertEqual(self.report(code=2)['status'], 'fail')

    def test_malformed_campaign_output_is_failure(self):
        events = self.events() + [dict(Action='output', Output='HARDENING {broken')]
        self.assertEqual(self.report(events)['status'], 'fail')

    def test_replayed_iteration_duplicates_are_rejected(self):
        events = self.events()
        events.append(next(e for e in events if '"iteration": 0' in e.get('Output', '')))
        self.assertEqual(self.report(events)['status'], 'fail')

    def test_resource_records_are_required(self):
        events = [e for e in self.events() if 'resource_case' not in e.get('Output', '')]
        self.assertEqual(self.report(events)['status'], 'fail')

    def test_missing_replay_controls_are_failure(self):
        events = self.events()
        for event in events:
            if 'campaign_start' in event.get('Output', ''):
                record = json.loads(event['Output'].split('HARDENING ', 1)[1])
                del record['config']['Probes']
                event['Output'] = 'HARDENING ' + json.dumps(record)
        self.assertEqual(self.report(events)['status'], 'fail')

    def test_runner_timeout_and_output_overflow_are_bounded(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            with self.assertRaises(TimeoutError):
                hardening.run_command([sys.executable, '-c', 'import time; time.sleep(60)'], output, {}, 0.1)
            with patch.object(hardening, 'EVENT_LIMIT', 8):
                with self.assertRaises(ValueError):
                    hardening.run_command([sys.executable, '-c', 'print("x" * 32)'], output, {}, 2)
            self.assertEqual((output / 'events.jsonl').stat().st_size, 8)


if __name__ == '__main__':
    unittest.main()
