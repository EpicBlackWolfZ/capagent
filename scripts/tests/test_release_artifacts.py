"""Exact-artifact verification, using finite synthetic ELF/archive fixtures."""
import hashlib
import importlib.util
import io
import json
import os
import subprocess
from pathlib import Path
import struct
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

SOURCE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SOURCE))


def load_verifier():
    spec = importlib.util.spec_from_file_location("release_artifacts", SOURCE / "release_artifacts.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def elf(machine=62, program_type=1):
    header = bytearray(120)
    header[:7] = b'\x7fELF\x02\x01\x01'
    struct.pack_into('<HHIQQQIHHHHHH', header, 16, 2, machine, 1, 0, 64, 0, 0, 64, 56, 1, 64, 0, 0)
    struct.pack_into('<IIQQQQQQ', header, 64, program_type, 5, 0, 0, 0, 120, 120, 4096)
    return bytes(header)


class ReleaseArtifactsTests(unittest.TestCase):
    def setUp(self):
        self.module = load_verifier()
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_pinned_tool_versions_accept_release_and_source_version_prefixes(self):
        for version in ('2.18.0', 'v2.18.0'):
            with self.subTest(version=version), mock.patch.object(self.module, 'command', side_effect=[
                    'go version go1.27.1 linux/amd64', f'GitVersion:    {version}\n', '{"version":"1.51.1"}']):
                self.module.verify_tool_versions()
        for version in ('2.18.1', 'v2.18.01', '2.18.0-dev', 'unknown'):
            with self.subTest(version=version), mock.patch.object(self.module, 'command', side_effect=[
                    'go version go1.27.1 linux/amd64', f'GitVersion:    {version}\n', '{"version":"1.51.1"}']):
                with self.assertRaises(ValueError):
                    self.module.verify_tool_versions()

    def test_missing_release_tools_fail_before_building(self):
        environment = dict(os.environ, PATH='/usr/bin:/bin')
        result = subprocess.run([str(SOURCE / 'release-check.sh')], env=environment,
                                capture_output=True, text=True, timeout=5)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Required release tool missing:', result.stderr)

    def test_static_elf_exact_architecture_and_headers(self):
        target = self.root / 'binary'
        for arch, machine in [('amd64', 62), ('arm64', 183)]:
            target.write_bytes(elf(machine))
            self.module.verify_elf(target, arch)
        for data in (b'not elf', elf(62, 3), elf(62, 2), elf(183), elf()[:80]):
            with self.subTest(data=data[:20]):
                target.write_bytes(data)
                with self.assertRaises(ValueError):
                    self.module.verify_elf(target, 'amd64')

    def test_archive_contains_exact_verified_binary_once(self):
        binary = elf()
        target = self.root / 'capagent.tar.gz'
        for content, duplicate in [(binary, False), (b'tampered', False), (binary, True)]:
            with tarfile.open(target, 'w:gz') as archive:
                for _ in range(2 if duplicate else 1):
                    member = tarfile.TarInfo('capagent')
                    member.size = len(content)
                    archive.addfile(member, io.BytesIO(content))
            if content == binary and not duplicate:
                self.module.verify_archive(target, hashlib.sha256(binary).hexdigest())
            else:
                with self.assertRaises(ValueError):
                    self.module.verify_archive(target, hashlib.sha256(binary).hexdigest())

    def test_sbom_format_is_content_not_filename(self):
        target = self.root / 'sbom.json'
        for kind, content in [('spdx', {'spdxVersion': 'SPDX-2.3', 'SPDXID': 'SPDXRef-DOCUMENT'}),
                              ('cyclonedx', {'bomFormat': 'CycloneDX', 'specVersion': '1.6'})]:
            target.write_text(json.dumps(content))
            self.module.verify_sbom(target, kind)
            with self.assertRaises(ValueError):
                self.module.verify_sbom(target, 'spdx' if kind == 'cyclonedx' else 'cyclonedx')

    def test_payload_index_requires_exact_variants_and_digests(self):
        expected = {'v1': 'a' * 64, 'v2': 'b' * 64}
        variants = [{'level': name, 'sha256': digest} for name, digest in expected.items()]
        self.module.verify_variants(variants, expected)
        for bad in (variants[:1], variants + variants[:1], [dict(variants[0], sha256='c' * 64), variants[1]]):
            with self.assertRaises(ValueError):
                self.module.verify_variants(bad, expected)


if __name__ == '__main__':
    unittest.main()
