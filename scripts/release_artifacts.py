#!/usr/bin/env python3
"""Verify exact release bytes and produce the read-only publisher handoff."""
import hashlib
import json
import os
import re
from pathlib import Path, PurePosixPath
import shutil
import struct
import subprocess
import sys
import tarfile

from microfat_tooling import ROOT, filename, host_arch, snapshot

VARIANTS = {"amd64": ("v1", "v2", "v3", "v4"), "arm64": ("v8.0", "v8.2", "v9.0")}
DIST = ROOT / "dist"
PACKAGED = DIST / "package"


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def verify_elf(path, arch):
    data = path.read_bytes()
    if len(data) < 64 or data[:7] != b"\x7fELF\x02\x01\x01":
        raise ValueError(f"not a supported ELF64 binary: {path}")
    kind, machine = struct.unpack_from("<HH", data, 16)
    if kind not in (2, 3) or machine != {"amd64": 62, "arm64": 183}[arch]:
        raise ValueError(f"wrong ELF architecture/type: {path}")
    phoff, shoff = struct.unpack_from("<QQ", data, 32)
    _, phsize, phnum, shsize, shnum, _ = struct.unpack_from("<HHHHHH", data, 52)
    if not phnum or phsize != 56 or phoff + phsize * phnum > len(data):
        raise ValueError(f"invalid ELF program table: {path}")
    for index in range(phnum):
        header = struct.unpack_from("<IIQQQQQQ", data, phoff + index * phsize)
        if header[0] in (2, 3) or header[2] + header[5] > len(data):
            raise ValueError(f"dynamic or truncated ELF program: {path}")
    if shnum:
        if shsize != 64 or shoff + shsize * shnum > len(data):
            raise ValueError(f"invalid ELF section table: {path}")
        for index in range(shnum):
            section = struct.unpack_from("<IIQQQQIIQQ", data, shoff + index * shsize)
            if section[1] == 6 and section[5]:
                raise ValueError(f"nonempty dynamic ELF section: {path}")


def verify_variants(variants, expected):
    actual = {}
    for variant in variants:
        name = variant["level"]
        if name in actual:
            raise ValueError("duplicate embedded variant")
        actual[name] = variant["sha256"]
    if actual != expected:
        raise ValueError("embedded variants do not match compiled payloads")


def verify_archive(path, expected):
    matches = []
    with tarfile.open(path, "r:gz") as archive:
        for member in archive:
            name = PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or not (member.isfile() or member.isdir()):
                raise ValueError("unsafe release archive member")
            if str(name) == "capagent":
                if not member.isfile() or member.size > 128 * 1024 * 1024:
                    raise ValueError("invalid release executable member")
                with archive.extractfile(member) as source:
                    matches.append(hashlib.file_digest(source, "sha256").hexdigest())
    if matches != [expected]:
        raise ValueError("archive does not contain the exact verified executable once")


def verify_sbom(path, kind):
    data = json.loads(path.read_bytes())
    if kind == "spdx":
        valid = data.get("spdxVersion", "").startswith("SPDX-") and data.get("SPDXID") == "SPDXRef-DOCUMENT"
    else:
        valid = data.get("bomFormat") == "CycloneDX" and bool(data.get("specVersion"))
    if not valid:
        raise ValueError(f"incorrect {kind} SBOM format: {path}")


def command(*args):
    return subprocess.check_output(args, cwd=ROOT, text=True, timeout=60).strip()


def verify_tool_versions():
    go = command('go', 'version')
    goreleaser = command('goreleaser', '--version')
    syft = json.loads(command('syft', 'version', '-o', 'json'))
    if go.split()[2] != 'go1.27.1' or not re.search(r'GitVersion:\s+v2\.18\.0(?:\s|$)', goreleaser) or syft.get('version') != '1.51.1':
        raise ValueError('release-check requires Go 1.27.1, GoReleaser 2.18.0, and Syft 1.51.1')


def verify_bundles(mode):
    if mode not in ("full", "minimal"):
        raise ValueError("invalid launcher mode")
    results = {}
    with snapshot() as (tooling, manifest, rows):
        cli = str(tooling / "microfat")
        for arch, levels in VARIANTS.items():
            expected = {}
            for level in levels:
                payload = DIST / f"capagent-{arch}_linux_{arch}_{level}" / "capagent"
                verify_elf(payload, arch)
                expected[level] = digest(payload)
            row = next(r for r in rows if r["role"] == mode and r["arch"] == arch)
            stub = tooling / filename(row)
            verify_elf(stub, arch)
            bundle = DIST / "fat" / mode / arch / "capagent"
            verify_elf(bundle, arch)
            with bundle.open("rb") as source:
                if hashlib.sha256(source.read(row["size"])).hexdigest() != row["sha256"]:
                    raise ValueError("embedded launcher does not match trusted stub")
            index = json.loads(command(cli, "inspect", str(bundle), "--json"))
            if (index.get("app_name"), index.get("os"), index.get("arch")) != ("capagent", "linux", arch):
                raise ValueError("incorrect bundle identity")
            verify_variants(index["variants"], expected)
            verification = command(cli, "verify", str(bundle), "--json")
            (DIST / f"verify-{mode}-{arch}.json").write_text(verification + "\n")
            if arch == host_arch():
                subprocess.run([str(bundle), "--help"], check=True, timeout=20, stdout=subprocess.DEVNULL)
                print(command(str(bundle), "--version"))
            results[arch] = {"sha256": digest(bundle), "launcher_sha256": row["sha256"],
                             "launcher_archive_sha256": row["archive_sha256"], "variants": expected}
    metadata = json.loads((DIST / "metadata.json").read_bytes())
    record = {"schema_version": 1, "commit": command("git", "rev-parse", "HEAD"),
              "version": metadata["version"], "launcher_mode": mode,
              "microfat_version": manifest["version"],
              "microfat_source": {
                  "release": f"https://github.com/EpicBlackWolfZ/microfat/releases/tag/{manifest['version']}",
                  "checksum_signer": "https://github.com/EpicBlackWolfZ/microfat/.github/workflows/release.yml"
                                     f"@refs/tags/{manifest['version']}",
                  "issuer": "https://token.actions.githubusercontent.com",
                  "assets": rows,
              },
              "microfat_manifest_sha256": digest(ROOT / "scripts/trust" / f"microfat-{manifest['version']}.json"),
              "bundles": results}
    (DIST / f"verified-{mode}.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    return record


def finalize():
    record = json.loads((DIST / "verified-minimal.json").read_bytes())
    if record["commit"] != command("git", "rev-parse", "HEAD"):
        raise ValueError("stale verified build commit")
    metadata = json.loads((PACKAGED / "metadata.json").read_bytes())
    if metadata["version"] != record["version"]:
        raise ValueError("packaging and compilation versions differ")
    output = DIST / "release"
    if output.exists():
        raise ValueError("release handoff already exists; start a clean release-check")
    output.mkdir(mode=0o700)
    files = []
    for arch in VARIANTS:
        archive = PACKAGED / f"capagent_{record['version']}_linux_{arch}.tar.gz"
        bundle = DIST / "fat/minimal" / arch / "capagent"
        if digest(bundle) != record["bundles"][arch]["sha256"]:
            raise ValueError("bundle changed after verification")
        verify_archive(archive, record["bundles"][arch]["sha256"])
        files.append(archive)
        for kind in ("spdx", "cyclonedx"):
            sbom = Path(str(archive) + f".{kind}.json")
            verify_sbom(sbom, kind)
            files.append(sbom)
    record["archives"] = {p.name: digest(p) for p in files}
    record["tools"] = {"go": command("go", "version"), "goreleaser": command("goreleaser", "--version"),
                       "syft": command("syft", "version", "-o", "json")}
    record["validation"] = {"amd64": "native smoke, static ELF and integrity",
                            "arm64": "static ELF and integrity; no native execution"}
    for path in files:
        shutil.copyfile(path, output / path.name)
    (output / "release-inputs.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    notes = PACKAGED / "CHANGELOG.md"
    (output / "release-notes.md").write_text(notes.read_text() if notes.exists() else f"capagent {record['version']}\n")
    checksums = [f"{digest(p)}  {p.name}\n" for p in sorted(output.iterdir())]
    (output / "checksums.txt").write_text("".join(checksums))
    handoff = DIST / "release-inputs.tar.gz"
    with tarfile.open(handoff, "w:gz") as archive:
        for path in sorted(output.iterdir()):
            info = archive.gettarinfo(str(path), arcname=path.name)
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            info.mode = 0o644
            with path.open("rb") as source:
                archive.addfile(info, source)
    print(f"Verified release handoff: {handoff}\nSHA-256: {digest(handoff)}")
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as sink:
            sink.write(f"sha256={digest(handoff)}\ncommit={record['commit']}\n")


def main():
    try:
        if sys.argv[1:] == ["check-tools"]:
            verify_tool_versions()
        elif sys.argv[1:] == ["finalize"]:
            finalize()
        elif len(sys.argv) == 3 and sys.argv[1] == "verify":
            verify_bundles(sys.argv[2])
        else:
            raise ValueError("usage: release_artifacts.py verify full|minimal | finalize")
    except (OSError, ValueError, KeyError, struct.error, tarfile.TarError, subprocess.SubprocessError) as error:
        print(f"artifact verification failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
