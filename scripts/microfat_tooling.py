#!/usr/bin/env python3
"""Repository-pinned microfat inputs; build-time only, Python standard library."""
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import subprocess
import sys
import tarfile
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
MAX_ASSET = 32 * 1024 * 1024
MAX_EXPANDED = 128 * 1024 * 1024
MAX_MEMBERS = 8192
LOCK_TIMEOUT = 30
ROLES = ("cli", "full", "minimal")
ARCHES = ("amd64", "arm64")


def fail(message):
    raise ValueError(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            fail(f"duplicate manifest key: {key}")
        result[key] = value
    return result


def host_arch():
    machine = os.uname().machine
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(machine)
    if not arch or sys.platform != "linux":
        fail(f"unsupported tooling host: {sys.platform}/{machine}")
    return arch


def load_manifest():
    version = os.environ.get("MICROFAT_VERSION", "v0.2.2")
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", version):
        fail("invalid MICROFAT_VERSION")
    path = ROOT / "scripts/trust" / f"microfat-{version}.json"
    raw = path.read_bytes()
    if len(raw) > 65536:
        fail("oversized trust manifest")
    manifest = json.loads(raw, object_pairs_hook=unique_object)
    if set(manifest) != {"schema_version", "version", "assets"}:
        fail("unexpected trust manifest fields")
    if type(manifest["schema_version"]) is not int or manifest["schema_version"] != 1 or manifest["version"] != version:
        fail("unsupported trust manifest version")
    assets = manifest["assets"]
    if not isinstance(assets, list) or len(assets) != len(ROLES) * len(ARCHES):
        fail("trust manifest must contain both CLIs and all four stubs")
    seen = set()
    names = set()
    fields = {"role", "arch", "archive", "archive_sha256", "archive_size", "member", "sha256", "size"}
    for row in assets:
        if not isinstance(row, dict) or set(row) != fields:
            fail("missing or unexpected asset trust fields")
        if row["role"] not in ROLES or row["arch"] not in ARCHES:
            fail("invalid asset role/architecture")
        key = row["role"], row["arch"]
        if key in seen or row["archive"] in names:
            fail("duplicate trusted asset")
        seen.add(key)
        names.add(row["archive"])
        for field in ("archive", "member"):
            if not isinstance(row[field], str) or not re.fullmatch(r"[a-zA-Z0-9_.-]+", row[field]) or row[field] in (".", ".."):
                fail(f"invalid asset {field}")
        for field in ("sha256", "archive_sha256"):
            if not isinstance(row[field], str) or not re.fullmatch(r"[a-f0-9]{64}", row[field]):
                fail(f"missing or invalid {field}")
        for field in ("size", "archive_size"):
            if type(row[field]) is not int or not 0 < row[field] <= MAX_ASSET:
                fail(f"invalid asset {field}")
    return manifest, hashlib.sha256(raw).hexdigest()


def filename(row):
    return "microfat" if row["role"] == "cli" else f"microfat-stub-{row['role']}-{row['arch']}"


def selected(manifest, arch):
    return [r for r in manifest["assets"] if r["role"] != "cli" or r["arch"] == arch]


def secure_dir(path):
    path.mkdir(mode=0o700, exist_ok=True)
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o022:
        fail(f"unsafe tooling directory: {path}")


def read_verified(path, digest, size, executable=False):
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size != size:
            fail(f"invalid file type/size: {path}")
        if info.st_uid != os.getuid() or info.st_mode & 0o022:
            fail(f"unsafe file ownership/permissions: {path}")
        if executable and not info.st_mode & stat.S_IXUSR:
            fail(f"missing executable permission: {path}")
        data = source.read(size + 1)
        if len(data) != size or hashlib.sha256(data).hexdigest() != digest:
            fail(f"SHA-256 mismatch: {path}")
        return data


def verify_set(directory, rows):
    secure_dir(directory)
    if {p.name for p in directory.iterdir()} != {filename(r) for r in rows}:
        fail(f"incomplete or unexpected cache set: {directory}; remove this generation and retry")
    for row in rows:
        read_verified(directory / filename(row), row["sha256"], row["size"], executable=True)


@contextlib.contextmanager
def install_lock(parent):
    fd = os.open(parent / ".lock", os.O_CREAT | os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
            fail("unsafe tooling lock")
        deadline = time.monotonic() + LOCK_TIMEOUT
        while True:
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() >= deadline:
                    fail("timed out waiting for tooling installation lock")
                time.sleep(0.05)
        yield
    finally:
        os.close(fd)


def extract_member(archive_path, row):
    matches = []
    total = 0
    with tarfile.open(archive_path, "r:gz") as archive:
        for index, member in enumerate(archive):
            path = PurePosixPath(member.name)
            total += member.size
            if index >= MAX_MEMBERS or total > MAX_EXPANDED:
                fail("archive expansion budget exceeded")
            if path.is_absolute() or ".." in path.parts or not (member.isfile() or member.isdir()):
                fail(f"unsafe archive member: {member.name}")
            if str(path) == row["member"]:
                if not member.isfile() or member.size != row["size"]:
                    fail("invalid executable archive member")
                with archive.extractfile(member) as source:
                    matches.append(source.read(row["size"] + 1))
    if len(matches) != 1 or len(matches[0]) != row["size"] or hashlib.sha256(matches[0]).hexdigest() != row["sha256"]:
        fail("missing, duplicate, or corrupt executable archive member")
    return matches[0]


def ensure():
    arch = host_arch()
    manifest, manifest_digest = load_manifest()
    rows = selected(manifest, arch)
    parent = ROOT
    for part in ("bin", "toolchains", "microfat", manifest["version"], arch):
        parent = parent / part
        secure_dir(parent)
    generation = parent / manifest_digest
    with install_lock(parent):
        if generation.exists() or generation.is_symlink():
            verify_set(generation, rows)
            return generation, manifest, rows
        with tempfile.TemporaryDirectory(prefix=".install-", dir=parent) as work:
            work = Path(work)
            stage = work / "complete"
            stage.mkdir(mode=0o700)
            for row in rows:
                archive = work / row["archive"]
                url = f"https://github.com/EpicBlackWolfZ/microfat/releases/download/{manifest['version']}/{row['archive']}"
                subprocess.run(["curl", "--fail", "--silent", "--show-error", "--location", "--proto", "=https",
                                "--proto-redir", "=https", "--connect-timeout", "10", "--max-time", "60",
                                "--retry", "2", "--max-filesize", str(row["archive_size"]),
                                "--output", str(archive), url], check=True, timeout=190)
                read_verified(archive, row["archive_sha256"], row["archive_size"])
                destination = stage / filename(row)
                destination.write_bytes(extract_member(archive, row))
                destination.chmod(0o500)
            verify_set(stage, rows)
            stage.rename(generation)
    return generation, manifest, rows


@contextlib.contextmanager
def snapshot():
    generation, manifest, rows = ensure()
    with tempfile.TemporaryDirectory(prefix="capagent-microfat-") as directory:
        directory = Path(directory)
        for row in rows:
            target = directory / filename(row)
            target.write_bytes(read_verified(generation / filename(row), row["sha256"], row["size"], executable=True))
            target.chmod(0o500)
        verify_set(directory, rows)
        yield directory, manifest, rows


def run(args):
    with snapshot() as (directory, _, _):
        return subprocess.run([str(directory / "microfat"), *args], check=False).returncode


def install_exit_handlers():
    def interrupted(signum, _frame):
        raise KeyboardInterrupt(f"interrupted by signal {signum}")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)


def main():
    install_exit_handlers()
    os.umask(0o077)
    try:
        if sys.argv[1:] in ([], ["--print-dir"]):
            directory, _, _ = ensure()
            if sys.argv[1:]:
                print(directory)
            else:
                print(f"Verified microfat toolchain: {directory}", file=sys.stderr)
            return 0
        if sys.argv[1:2] == ["run"] and len(sys.argv) > 2:
            return run(sys.argv[2:])
        fail("usage: ensure-microfat.sh [--print-dir] or microfat.sh <command> [args...]")
    except (OSError, ValueError, TypeError, tarfile.TarError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        print(f"microfat trust verification failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
