#!/usr/bin/env python3
"""Verify a passive host report against independent native facts and its syscall trace."""
import argparse
import importlib.util
import json
import os
import re
from pathlib import Path
import subprocess
import sys

SPEC = importlib.util.spec_from_file_location("passive", Path(__file__).with_name("podman-discovery-smoke.py"))
PASSIVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PASSIVE)
HOST_PROBES = {"host." + name for name in (
    "os", "kernel", "systemd", "cgroups", "namespaces", "security", "filesystems", "network", "resolver")}


def verify_report(report, uid, kernel):
    trace = report["evaluation"]
    if report["schema_version"] != 1 or trace["scope"]["runtime"] or trace["scope"]["endpoint"]:
        raise ValueError("host scope mismatch")
    if "requirement" in trace or report["runtimes"] or report["capabilities"] or trace["evidence"]:
        raise ValueError("host collection invented a deployment verdict")
    if report["context"]["uid"] != uid or report["host"]["kernel"] != kernel:
        raise ValueError("host report disagrees with independent native identity or uname")
    if trace["collection"] != "passive" or trace["mode"] != "live":
        raise ValueError("wrong host collection policy")
    observed = {entry["probe_id"] for entry in trace["observations"] if "host" in entry}
    if observed != HOST_PROBES:
        raise ValueError("missing host observation")
    return 0 if report["host"]["completeness"] == "complete" and report["context"].get("completeness") == "complete" else 2


def verify_host_trace(trace, binary):
    PASSIVE.verify_trace(trace, binary)
    trace = PASSIVE.joined_trace(trace)
    if re.search(r"\b(?:socket|socketpair|sendto|sendmsg|sendmmsg)\(", trace):
        raise ValueError("host collector attempted active network operations")
    for line in trace.splitlines():
        # Go labels its own anonymous mappings. This changes no security policy.
        if "prctl(PR_SET_" in line and "prctl(PR_SET_VMA, PR_SET_VMA_ANON_NAME," not in line:
            raise ValueError("host collector attempted a security operation")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--strace", default="/usr/bin/strace", type=Path)
    parser.add_argument("--sudo", action="store_true")
    args = parser.parse_args()
    binary, tracer = args.binary.resolve(strict=True), args.strace.resolve(strict=True)
    destination = args.output.resolve()
    destination.mkdir(parents=True, exist_ok=True)
    environment = {"PATH": "/nonexistent", "LC_ALL": "C", "HOME": "/nonexistent",
                   "CONTAINER_HOST": "ssh://SECRET.invalid/ignored"}
    command = [str(tracer), "-f", "-qq", "-s", "256", "-o", str(destination / "host.trace"), "-e",
               "trace=%file,%process,%network,fchmod,fchown,ftruncate,mount,umount2,setns,unshare,prctl",
               str(binary), "--json"]
    if args.sudo:
        command = ["/usr/bin/sudo", "-n", "--", "/usr/bin/env", "-i",
                   *[f"{key}={value}" for key, value in environment.items()], *command]
    result = subprocess.run(command, env=environment, capture_output=True, timeout=30, check=False)
    if result.returncode not in (0, 2) or b"SECRET" in result.stdout + result.stderr:
        raise ValueError("host collection failed or leaked ambient input")
    report = json.loads(result.stdout)
    expected = verify_report(report, 0 if args.sudo else os.geteuid(), os.uname().release)
    if result.returncode != expected:
        raise ValueError("host exit code disagrees with collection completeness")
    consumer = Path(__file__).resolve().parents[1] / "examples/check-report.py"
    decision = subprocess.run([sys.executable, str(consumer)], input=result.stdout,
                              capture_output=True, timeout=10, check=False)
    if decision.returncode != 1:
        raise ValueError("deployment consumer did not reject the host-only report")
    trace = (destination / "host.trace").read_text()
    verify_host_trace(trace, binary)
    (destination / "host.json").write_bytes(result.stdout)
    summary = {"uid": report["context"]["uid"], "exit": expected, "passive_trace": True, "consumer_rejected": True,
               "observations": sorted(HOST_PROBES), "kernel": report["host"]["kernel"]}
    (destination / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary))


if __name__ == "__main__":
    main()
