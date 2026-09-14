"""The final gate must reject missing, failed, stale and contradictory evidence."""
from copy import deepcopy
import json
import os
from pathlib import Path
import signal
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import gate


class GateFixtureMixin:
    def records(self):
        identity = dict(commit='a'*40, source_sha256='b'*64, dirty=False, go_version='go1.27.1', run_id='123', run_attempt='1')
        return [dict(schema_version=1, kind='gate-stage', stage=name, status='pass', identity=identity,
                     commands=[dict(name=command, status='pass', exit_code=0) for command in gate.required_commands(name)],
                     failures=[], details=self.details(name, identity)) for name in gate.STAGES]

    def details(self, name, identity):
        if name == 'test':
            return dict(hardening=dict(status='pass'),
                        coverage={key: dict(covered=100, total=100) for key in
                                  ('total', gate.hardening.MODULE+'/internal/model', gate.hardening.MODULE+'/internal/requirement')},
                        corpus_targets=list(gate.fuzzing.TARGETS),
                        benchmarks=[dict(name=family+'/'+str(i), iterations=1)
                                    for family, count in gate.BENCH_COUNTS.items() for i in range(count)])
        if name == 'fuzz':
            return dict(fuzz=dict(status='pass', profile='smoke', identity=identity,
                                 inventory=[dict(target=target, corpus=['seed']) for target in gate.fuzzing.TARGETS],
                                 commands=[dict(status='pass', exit_code=0) for _ in gate.fuzzing.TARGETS]))
        if name == 'vulncheck': return dict(vulnerabilities=dict(reachable=[], package_only=[], module_only=[]))
        if name == 'gitleaks': return dict(findings_count=0)
        if name == 'build':
            return dict(full=dict(commit=identity['commit'], launcher_mode='full', bundles=dict(amd64={}, arm64={})),
                        minimal=dict(commit=identity['commit'], launcher_mode='minimal', bundles=dict(amd64={}, arm64={})),
                        release=dict(commit=identity['commit'], archives={str(i): 'hash' for i in range(6)}),
                        handoff_sha256='d'*64)
        return {}


class GateTests(unittest.TestCase, GateFixtureMixin):

    def test_complete_gate_passes(self):
        records = self.records()
        self.assertEqual(gate.summarize(records, records[0]['identity'])['status'], 'pass')

    def test_six_valid_reports_with_all_success_outcomes_passes(self):
        records = self.records()
        outcomes = {name: 'success' for name in gate.STAGES}
        summary = gate.summarize(records, records[0]['identity'], outcomes)
        self.assertEqual(summary['status'], 'pass')
        self.assertEqual(summary['failures'], [])

    def test_six_valid_reports_without_outcomes_mapping_passes(self):
        records = self.records()
        summary = gate.summarize(records, records[0]['identity'], None)
        self.assertEqual(summary['status'], 'pass')
        self.assertEqual(summary['failures'], [])

    def test_each_stage_individual_outcome_failure_skipped_cancelled_fails(self):
        records = self.records()
        expected = records[0]['identity']
        for stage in gate.STAGES:
            for outcome in ('failure', 'skipped', 'cancelled'):
                outcomes = {name: 'success' for name in gate.STAGES}
                outcomes[stage] = outcome
                with self.subTest(stage=stage, outcome=outcome):
                    summary = gate.summarize(deepcopy(records), expected, outcomes)
                    self.assertEqual(summary['status'], 'fail')
                    self.assertIn('required CI dependency did not succeed', summary['failures'])

    def test_each_stage_individual_missing_none_empty_or_unexpected_outcome_fails(self):
        records = self.records()
        expected = records[0]['identity']
        for stage in gate.STAGES:
            for invalid in (None, '', 'in_progress', 'queued', 'unknown'):
                outcomes = {name: 'success' for name in gate.STAGES}
                outcomes[stage] = invalid
                with self.subTest(stage=stage, invalid=invalid):
                    summary = gate.summarize(deepcopy(records), expected, outcomes)
                    self.assertEqual(summary['status'], 'fail')
                    self.assertIn('required CI dependency did not succeed', summary['failures'])

            # Also verify missing key in outcomes
            outcomes = {name: 'success' for name in gate.STAGES}
            del outcomes[stage]
            with self.subTest(stage=stage, case='missing_key'):
                summary = gate.summarize(deepcopy(records), expected, outcomes)
                self.assertEqual(summary['status'], 'fail')
                self.assertIn('required CI dependency did not succeed', summary['failures'])

    def test_prereqs_only_with_failure_and_heavy_skipped_fails_with_diagnostics(self):
        all_records = self.records()
        expected = all_records[0]['identity']
        # Only prerequisite reports exist; lint has failed
        prereq_records = [deepcopy(r) for r in all_records if r['stage'] in ('lint', 'vulncheck', 'gitleaks')]
        lint_rec = [r for r in prereq_records if r['stage'] == 'lint'][0]
        lint_rec['status'] = 'fail'
        lint_rec['failures'] = ['linting error']
        outcomes = {
            'lint': 'failure',
            'vulncheck': 'success',
            'gitleaks': 'success',
            'test': 'skipped',
            'build': 'skipped',
            'fuzz': 'skipped',
        }
        summary = gate.summarize(prereq_records, expected, outcomes)
        self.assertEqual(summary['status'], 'fail')
        self.assertIn('required CI dependency did not succeed', summary['failures'])
        self.assertIn('missing, duplicate or unexpected stage report', summary['failures'])
        self.assertTrue(any('lint:' in f for f in summary['failures']))

    def test_smoke_step_failure_defeats_passing_stage_report(self):
        records = self.records()
        expected = records[0]['identity']
        # Test stage report says pass, but later smoke step failed in CI runner -> job outcome is failure
        outcomes = {name: 'success' for name in gate.STAGES}
        outcomes['test'] = 'failure'
        summary = gate.summarize(deepcopy(records), expected, outcomes)
        self.assertEqual(summary['status'], 'fail')
        self.assertIn('required CI dependency did not succeed', summary['failures'])

    def test_incomplete_or_inconsistent_gate_fails(self):
        for mutation in ('missing', 'duplicate', 'failed', 'commit', 'source', 'run', 'attempt', 'command', 'skipped', 'detail'):
            records = deepcopy(self.records())
            expected = deepcopy(records[0]['identity'])
            if mutation == 'missing': records.pop()
            if mutation == 'duplicate': records.append(deepcopy(records[0]))
            if mutation == 'failed': records[0]['status'] = 'fail'
            if mutation == 'commit': records[0]['identity']['commit'] = 'c'*40
            if mutation == 'source': records[0]['identity']['source_sha256'] = 'c'*64
            if mutation == 'run': records[0]['identity']['run_id'] = 'previous-run'
            if mutation == 'attempt': records[0]['identity']['run_attempt'] = '0'
            if mutation == 'command': records[0]['commands'].pop()
            if mutation == 'skipped': records[0]['commands'][0]['status'] = 'skip'
            if mutation == 'detail': records[1]['details'] = {}
            with self.subTest(mutation=mutation):
                self.assertEqual(gate.summarize(records, expected)['status'], 'fail')

    def test_failed_dependency_cannot_be_hidden_by_report(self):
        records = self.records()
        outcomes = {name: 'success' for name in gate.STAGES}
        outcomes['build'] = 'failure'
        self.assertEqual(gate.summarize(records, records[0]['identity'], outcomes)['status'], 'fail')

    def test_vulnerability_reachability_is_not_module_presence(self):
        rows = [dict(finding=dict(osv='GO-example', trace=[dict(module='module', version='v1')]))]
        self.assertEqual(gate.vulnerabilities(rows)['reachable'], [])
        rows.append(dict(finding=dict(osv='GO-example', trace=[dict(module='module', package='pkg', function='Danger')])) )
        self.assertEqual(gate.vulnerabilities(rows)['reachable'], ['GO-example'])


class GateEvidenceTests(unittest.TestCase, GateFixtureMixin):
    def test_bad_json_shape_is_a_reported_failure(self):
        self.assertEqual(gate.summarize([[]], self.records()[0]['identity'])['status'], 'fail')

    def test_exact_domain_threshold_is_rejected_even_when_total_passes(self):
        records = self.records()
        records[1]['details']['coverage'][gate.hardening.MODULE+'/internal/model']['covered'] = 95
        self.assertEqual(gate.summarize(records, records[0]['identity'])['status'], 'fail')

    def test_missing_benchmark_or_fuzz_execution_is_failure(self):
        for kind in ('benchmark', 'fuzz'):
            records = self.records()
            if kind == 'benchmark': records[1]['details']['benchmarks'].pop()
            else: records[-1]['details']['fuzz']['commands'].pop()
            self.assertEqual(gate.summarize(records, records[0]['identity'])['status'], 'fail')


class GateReportLifecycleTests(unittest.TestCase, GateFixtureMixin):
    def test_finish_passing_report_emission(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            records = self.records()
            expected = records[0]['identity']
            outcomes = {name: 'success' for name in gate.STAGES}

            for rec in records:
                stage_dir = root / rec['stage']
                gate.write_report(stage_dir / 'summary.json', rec)

            summary_md_target = root / 'step_summary.md'
            with patch.dict(os.environ, {'GITHUB_STEP_SUMMARY': str(summary_md_target)}):
                report = gate.finish(root, expected, outcomes)

            self.assertEqual(report['status'], 'pass')
            self.assertEqual(report['kind'], 'm1.1-gate')
            self.assertEqual(report['schema_version'], 1)
            self.assertEqual(report['identity'], expected)
            self.assertEqual(report['failures'], [])

            written_json = json.loads((root / 'summary.json').read_text())
            self.assertEqual(written_json['status'], 'pass')

            md_text = (root / 'summary.md').read_text()
            self.assertTrue(md_text.startswith(f"CI Gate: **pass**\n\nCommit: `{expected['commit']}`\n\n"),
                            f"unexpected heading in markdown: {md_text[:60]}")

            # Verify step summary was appended to
            self.assertEqual(summary_md_target.read_text(), md_text)

    def test_finish_staged_failure_missing_heavy_stage_files(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            all_records = self.records()
            expected = all_records[0]['identity']
            outcomes = {
                'lint': 'failure',
                'vulncheck': 'success',
                'gitleaks': 'success',
                'test': 'skipped',
                'build': 'skipped',
                'fuzz': 'skipped',
            }

            # Only write prerequisite files, with lint failing
            for rec in all_records:
                if rec['stage'] == 'lint':
                    failed_rec = deepcopy(rec)
                    failed_rec['status'] = 'fail'
                    failed_rec['failures'] = ['lint error']
                    gate.write_report(root / rec['stage'] / 'summary.json', failed_rec)
                elif rec['stage'] in ('vulncheck', 'gitleaks'):
                    gate.write_report(root / rec['stage'] / 'summary.json', rec)

            step_summary = root / 'step_summary.md'
            with patch.dict(os.environ, {'GITHUB_STEP_SUMMARY': str(step_summary)}):
                report = gate.finish(root, expected, outcomes)
            self.assertEqual(report['status'], 'fail')
            self.assertEqual(report['kind'], 'm1.1-gate')
            self.assertEqual(report['schema_version'], 1)

            # Heavy stage files must be reported as errors
            failures_str = ' '.join(report['failures'])
            self.assertIn('test:', failures_str)
            self.assertIn('build:', failures_str)
            self.assertIn('fuzz:', failures_str)
            self.assertIn('required CI dependency did not succeed', failures_str)

            md_text = (root / 'summary.md').read_text()
            self.assertTrue(md_text.startswith(f"CI Gate: **fail**\n\nCommit: `{expected['commit']}`\n\n"),
                            f"unexpected heading in markdown: {md_text[:60]}")
            self.assertEqual(step_summary.read_text(), md_text)

    def test_finish_lifecycle_preserves_ambient_step_summary_sentinel(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            ambient_summary = Path(tmpdir) / 'ambient_step_summary.md'
            sentinel = "<!-- sentinel: preserve ambient summary -->\n"
            ambient_summary.write_text(sentinel)

            with patch.dict(os.environ, {'GITHUB_STEP_SUMMARY': str(ambient_summary)}):
                self.test_finish_passing_report_emission()
                self.test_finish_staged_failure_missing_heavy_stage_files()

            self.assertEqual(ambient_summary.read_text(), sentinel,
                             'gate.finish() leaked summary output into ambient GITHUB_STEP_SUMMARY')

    def test_main_report_mode_exit_codes(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            records = self.records()
            expected = records[0]['identity']

            for rec in records:
                gate.write_report(root / rec['stage'] / 'summary.json', rec)

            valid_outcomes = {name: 'success' for name in gate.STAGES}
            step_summary = root / 'step_summary.md'
            original_sigterm = signal.getsignal(signal.SIGTERM)

            # Test 1: Passing report mode exits 0
            with patch.object(sys, 'argv', ['gate.py', 'report', '--output', str(root)]), \
                 patch('gate.identity', return_value=expected), \
                 patch.object(gate, 'install_signals'), \
                 patch.dict(os.environ, {'CAPAGENT_JOB_RESULTS': json.dumps(valid_outcomes),
                                         'GITHUB_STEP_SUMMARY': str(step_summary)}):
                code = gate.main()
                self.assertEqual(code, 0)

            # Test 2: Failing outcome exits 1
            failing_outcomes = dict(valid_outcomes, lint='failure')
            with patch.object(sys, 'argv', ['gate.py', 'report', '--output', str(root)]), \
                 patch('gate.identity', return_value=expected), \
                 patch.object(gate, 'install_signals'), \
                 patch.dict(os.environ, {'CAPAGENT_JOB_RESULTS': json.dumps(failing_outcomes),
                                         'GITHUB_STEP_SUMMARY': str(step_summary)}):
                code = gate.main()
                self.assertEqual(code, 1)

            self.assertEqual(signal.getsignal(signal.SIGTERM), original_sigterm,
                             'gate.main() leaked process-wide SIGTERM handler')


if __name__ == '__main__':
    unittest.main()
