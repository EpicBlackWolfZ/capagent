"""The final gate must reject missing, failed, stale and contradictory evidence."""
from copy import deepcopy
import json
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import gate


class GateTests(unittest.TestCase):
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

    def test_complete_gate_passes(self):
        records = self.records()
        self.assertEqual(gate.summarize(records, records[0]['identity'])['status'], 'pass')

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


if __name__ == '__main__':
    unittest.main()

class GateEvidenceTests(GateTests):
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
