#!/usr/bin/env python3
"""Trace passive capagent discovery on an ephemeral Linux test runner."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess


def verify_trace(text, binary):
    executions = re.findall(r'execve\("([^"\n]+)"', text)
    if executions != [str(binary)] or re.search(r"\b(?:execveat|fork|vfork)\(", text):
        raise RuntimeError("discovery executed an unexpected subprocess")
    for clone in re.findall(r"\bclone3?\([^\n]+", text):
        if "CLONE_THREAD" not in clone:
            raise RuntimeError("discovery created a non-thread child")
    mutations = (
        r"\b(?:mkdir(?:at)?|rmdir|unlink(?:at)?|rename(?:at2?)?|link(?:at)?|symlink(?:at)?|"
        r"chmod|fchmod(?:at2?)?|chown|fchown(?:at)?|lchown|truncate|ftruncate|"
        r"utime|utimes|utimensat|mknod(?:at)?|mount|umount2|setns|unshare)\("
    )
    if re.search(mutations, text) or re.search(r"\bO_(?:WRONLY|RDWR|CREAT|TRUNC|APPEND)\b", text):
        raise RuntimeError("discovery attempted a filesystem or namespace mutation")
    if re.search(r"\b(?:connect|bind|listen)\(", text):
        raise RuntimeError("discovery attempted network communication")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--strace", default="/usr/bin/strace", type=Path)
    parser.add_argument("--sudo", action="store_true")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    tracer = args.strace.resolve(strict=True)
    destination = args.output.resolve()
    destination.mkdir(parents=True, exist_ok=True)
    # Only fixed dummy values are injected. No host endpoint or credential
    # values enter the evidence or the traced process environment.
    environment = {
        "PATH": "/nonexistent", "LC_ALL": "C", "HOME": "/nonexistent",
        "CONTAINER_HOST": "ssh://SECRET.invalid/ignored",
        "CONTAINER_CONNECTION": "SECRET", "XDG_RUNTIME_DIR": "/nonexistent",
    }
    expected_uid = 0 if args.sudo else os.geteuid()
    results = []
    for name, extra, expected_exit in (
        ("installed", [], 2),
        ("absent", ["--podman-path", str(destination / "no-podman")], 1),
        ("invalid", ["--podman-path", "relative"], 64),
    ):
        trace = destination / f"{name}.trace"
        command = [str(tracer), "-f", "-qq", "-s", "256", "-o", str(trace), "-e",
                   "trace=%file,%process,%network,fchmod,fchown,ftruncate,mount,umount2,setns,unshare",
                   str(binary), "--runtime", "podman", "--json", *extra]
        if args.sudo:
            command = ["/usr/bin/sudo", "-n", "--", "/usr/bin/env", "-i",
                       *[f"{key}={value}" for key, value in environment.items()], *command]
        result = subprocess.run(command, env=environment, capture_output=True, timeout=30, check=False)
        if result.returncode != expected_exit:
            raise RuntimeError(f"{name}: unexpected exit {result.returncode}")
        verify_trace(trace.read_text(), binary)
        if b"SECRET" in result.stdout + result.stderr:
            raise RuntimeError("ambient endpoint value appeared in output")
        if name != "invalid":
            report = json.loads(result.stdout)
            if report["evaluation"]["mode"] != "live" or report["context"]["uid"] != expected_uid:
                raise RuntimeError("wrong execution identity or report mode")
            runtime = report["runtimes"]["podman"]
            if runtime["installed"] is not (name == "installed") or runtime["accessible"] is not None:
                raise RuntimeError("incorrect installed/accessibility result")
            if "version" in runtime or runtime.get("cli_runnable") is not None:
                raise RuntimeError("passive discovery claimed CLI execution")
        (destination / f"{name}.json").write_bytes(result.stdout)
        results.append({"scenario": name, "exit": result.returncode, "uid": expected_uid, "passive_trace": True})
    (destination / "summary.json").write_text(json.dumps(results, indent=2) + "\n")
    print(json.dumps(results))


if __name__ == "__main__":
    main()
