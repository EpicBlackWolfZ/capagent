"""Execute the actual publisher workflow's verifier; no copied implementation."""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


@unittest.skipUnless(os.environ.get('CAPAGENT_HANDOFF_SCRIPT'), 'run through go test ./tests/contract for workflow extraction')
class HandoffTests(unittest.TestCase):
    def run_case(self, mutation):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'incoming').mkdir()
            commit = 'a' * 40
            files = {'release-inputs.json': json.dumps(dict(schema_version=1, commit=commit,
                     version='1.2.3', launcher_mode='minimal')).encode(), 'release-notes.md': b'release'}
            for arch in ('amd64', 'arm64'):
                name = f'capagent_1.2.3_linux_{arch}.tar.gz'
                for suffix in ('', '.spdx.json', '.cyclonedx.json'):
                    files[name + suffix] = b'verified build bytes'
            if mutation == 'provenance':
                files['release-inputs.json'] = files['release-inputs.json'].replace(b'minimal', b'full')
            checksums = ''.join(f'{hashlib.sha256(data).hexdigest()}  {name}\n' for name, data in sorted(files.items()))
            files['checksums.txt'] = checksums.encode()
            if mutation == 'corrupt_file':
                files['release-notes.md'] = b'changed after checksum'
            elif mutation == 'missing':
                del files['release-notes.md']
            elif mutation == 'extra':
                files['run-me.sh'] = b'#!/bin/sh\ntouch executed\n'
            elif mutation == 'checksum_set':
                files['checksums.txt'] = b''
            elif mutation == 'duplicate_checksum':
                files['checksums.txt'] += checksums.splitlines(keepends=True)[0].encode()
            source = root / 'incoming/release-inputs.tar.gz'
            with tarfile.open(source, 'w:gz') as archive:
                for name, data in files.items():
                    info = tarfile.TarInfo(name)
                    info.size = len(data)
                    archive.addfile(info, io.BytesIO(data))
                if mutation in ('duplicate', 'symlink', 'traversal'):
                    info = tarfile.TarInfo('../escape' if mutation == 'traversal' else 'release-notes.md')
                    if mutation == 'symlink':
                        info.type = tarfile.SYMTYPE
                        info.linkname = '../escape'
                    archive.addfile(info)
            expected = hashlib.sha256(source.read_bytes()).hexdigest()
            if mutation == 'wrong_hash':
                expected = 'f' * 64
            env = dict(os.environ, EXPECTED_SHA256=expected, BUILT_COMMIT=commit,
                       RELEASE_TAG='v1.2.3' if mutation != 'invalid_tag' else '../../outside')
            result = subprocess.run(['bash', '-euo', 'pipefail', '-c', env['CAPAGENT_HANDOFF_SCRIPT']],
                                    env=env, cwd=root, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode == 0, mutation == 'valid', result.stdout + result.stderr)
            self.assertEqual((root / 'release').exists(), mutation == 'valid')
            self.assertFalse((root / 'executed').exists())

    def test_valid_handoff_and_fail_closed_mutations(self):
        for mutation in ('valid', 'wrong_hash', 'corrupt_file', 'missing', 'extra', 'provenance',
                         'checksum_set', 'duplicate_checksum', 'duplicate', 'symlink', 'traversal', 'invalid_tag'):
            with self.subTest(mutation=mutation):
                self.run_case(mutation)
