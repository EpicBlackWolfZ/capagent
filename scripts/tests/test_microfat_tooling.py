"""Offline black-box contracts for the production tooling entry points."""
import concurrent.futures
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import signal
import time
import subprocess
import tarfile
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[1]
VERSION = "v0.2.2"


class ToolingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        shutil.copytree(SOURCE, self.root / "scripts", ignore=shutil.ignore_patterns("__pycache__", "tests"))
        self.assets = self.root / "assets"
        self.assets.mkdir()
        self.manifest = {"schema_version": 1, "version": VERSION, "assets": []}
        for arch in ("amd64", "arm64"):
            for role in ("cli", "full", "minimal"):
                member = "microfat" if role == "cli" else "microfat-stub-" + role
                data = b'#!/bin/sh\nprintf "verified:%s\\n" "$1"\n'
                name = f"{role}-{arch}.tar.gz"
                asset = self.make_archive(name, member, data)
                self.manifest["assets"].append(dict(role=role, arch=arch, archive=name,
                    archive_sha256=hashlib.sha256(asset).hexdigest(), archive_size=len(asset),
                    member=member, sha256=hashlib.sha256(data).hexdigest(), size=len(data)))
        self.manifest_path = self.root / "scripts/trust" / f"microfat-{VERSION}.json"
        self.manifest_path.parent.mkdir(exist_ok=True)
        self.save_manifest()
        fake = self.root / "fake"
        fake.mkdir()
        (fake / "curl").write_text('''#!/usr/bin/env python3
import os,pathlib,sys
a=sys.argv
out=pathlib.Path(a[a.index('--output')+1])
src=pathlib.Path(os.environ['FIXTURE_ASSETS'])/a[-1].rsplit('/',1)[-1]
out.write_bytes(src.read_bytes())
''')
        (fake / "curl").chmod(0o755)
        (fake / "microfat").write_text('#!/bin/sh\necho UNTRUSTED >> "$FIXTURE_MARKER"\nexit 0\n')
        (fake / "microfat").chmod(0o755)
        self.env = dict(os.environ, PATH=str(fake) + os.pathsep + os.environ["PATH"],
                        FIXTURE_ASSETS=str(self.assets), FIXTURE_MARKER=str(self.root / "executed"),
                        MICROFAT_VERSION=VERSION, PYTHONDONTWRITEBYTECODE="1")

    def make_archive(self, name, member, data, kind=tarfile.REGTYPE, duplicate=False):
        target = self.assets / name
        with tarfile.open(target, "w:gz") as archive:
            info = tarfile.TarInfo(member)
            info.size = len(data) if kind == tarfile.REGTYPE else 0
            info.type = kind
            info.linkname = "../../outside" if kind != tarfile.REGTYPE else ""
            archive.addfile(info, io.BytesIO(data) if info.size else None)
            if duplicate:
                archive.addfile(info, io.BytesIO(data))
        return target.read_bytes()

    def save_manifest(self):
        self.manifest_path.write_text(json.dumps(self.manifest))

    def run_script(self, name="ensure-microfat.sh", *args, success=True):
        result = subprocess.run([str(self.root / "scripts" / name), *args], env=self.env,
                                capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        self.assertFalse((self.root / "executed").exists(), "untrusted program executed")
        return result

    def prepare(self):
        result = self.run_script("ensure-microfat.sh", "--print-dir")
        generation = Path(result.stdout.strip())
        self.assertTrue(generation.is_dir(), result.stdout)
        return generation

    def test_verified_set_and_offline_cache(self):
        generation = self.prepare()
        self.assertEqual(len(list(generation.iterdir())), 5)
        shutil.rmtree(self.assets)
        self.assertEqual(self.prepare(), generation)
        result = self.run_script("microfat.sh", "hello")
        self.assertEqual(result.stdout.strip(), "verified:hello")

    def test_missing_digest_and_unknown_version_fail_before_download(self):
        del self.manifest["assets"][0]["sha256"]
        self.save_manifest()
        self.run_script(success=False)
        self.env["MICROFAT_VERSION"] = "../../outside"
        self.run_script(success=False)
        self.env["MICROFAT_VERSION"] = "v9.9.9"
        self.run_script(success=False)

    def test_manifest_rejects_duplicates_and_bad_sizes(self):
        original = list(self.manifest["assets"])
        for mutation in ("duplicate", "missing", "oversize", "path"):
            with self.subTest(mutation=mutation):
                self.manifest["assets"] = [dict(item) for item in original]
                if mutation == "duplicate":
                    self.manifest["assets"].append(dict(original[0]))
                elif mutation == "missing":
                    self.manifest["assets"].pop()
                elif mutation == "oversize":
                    self.manifest["assets"][0]["size"] = 1 << 40
                else:
                    self.manifest["assets"][0]["archive"] = "../outside"
                self.save_manifest()
                self.run_script(success=False)

    def test_tampered_wrong_and_partial_download_never_install(self):
        for data in (b"modified", b"", b"\x1f\x8bpartial"):
            with self.subTest(data=data):
                for path in self.assets.iterdir():
                    path.write_bytes(data)
                self.run_script(success=False)
                self.assertFalse([p for p in (self.root / "bin").glob("**/microfat") if p.is_file()])

    def test_valid_digest_does_not_allow_invalid_archive_members(self):
        for kind, duplicate, member in ((tarfile.SYMTYPE, False, "microfat"),
                                       (tarfile.REGTYPE, True, "microfat"),
                                       (tarfile.REGTYPE, False, "../microfat")):
            with self.subTest(kind=kind, duplicate=duplicate, member=member):
                for row in self.manifest["assets"]:
                    data = self.make_archive(row["archive"], member, b"bad", kind, duplicate)
                    row["archive_sha256"] = hashlib.sha256(data).hexdigest()
                    row["archive_size"] = len(data)
                self.save_manifest()
                self.run_script(success=False)

    def test_cache_replacement_corruption_and_permissions_fail_closed(self):
        generation = self.prepare()
        cli = generation / "microfat"
        original = cli.read_bytes()
        for mutation in ("replace", "missing", "symlink", "writable"):
            with self.subTest(mutation=mutation):
                cli.unlink(missing_ok=True)
                if mutation == "replace":
                    cli.write_bytes(b'#!/bin/sh\necho UNTRUSTED >> "$FIXTURE_MARKER"\n')
                    cli.chmod(0o755)
                elif mutation == "symlink":
                    cli.symlink_to(self.root / "fake/microfat")
                elif mutation == "writable":
                    cli.write_bytes(original)
                    cli.chmod(0o777)
                self.run_script("microfat.sh", "hello", success=False)
        cli.unlink(missing_ok=True)
        cli.write_bytes(original)
        cli.chmod(0o500)
        self.run_script("microfat.sh", "hello")

    def test_concurrent_installers_observe_one_complete_generation(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
            paths = list(pool.map(lambda _: self.prepare(), range(3)))
        self.assertEqual(len(set(paths)), 1)

    def test_interrupted_download_cleans_staging_and_releases_lock(self):
        curl = self.root / "fake/curl"
        original = curl.read_text()
        curl.write_text("#!/usr/bin/env python3\nimport os,pathlib,time\n"
                        "pathlib.Path(os.environ['FIXTURE_ASSETS']+'/started').touch()\ntime.sleep(30)\n")
        child = subprocess.Popen([str(self.root / "scripts/ensure-microfat.sh")], env=self.env,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 5
            while not (self.assets / 'started').exists() and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertTrue((self.assets / 'started').exists())
            child.send_signal(signal.SIGTERM)
            child.communicate(timeout=5)
            self.assertNotEqual(child.returncode, 0)
            self.assertFalse(list((self.root / 'bin').glob('**/.install-*')))
        finally:
            if child.poll() is None:
                child.kill()
                child.communicate(timeout=5)
        curl.write_text(original)
        self.prepare()

    def test_manifest_change_uses_a_new_generation(self):
        first = self.prepare()
        self.manifest_path.write_text(json.dumps(self.manifest, indent=2))
        second = self.prepare()
        self.assertNotEqual(first, second)
        self.assertTrue(first.exists())

    def test_legacy_cache_and_mode_do_not_bypass_trust(self):
        (self.root / "bin").mkdir()
        shutil.copy2(self.root / "fake/microfat", self.root / "bin/microfat")
        self.prepare()
        self.run_script("bundle-fat.sh", "invalid", success=False)


if __name__ == "__main__":
    unittest.main()
