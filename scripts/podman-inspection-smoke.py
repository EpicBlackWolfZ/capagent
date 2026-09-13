#!/usr/bin/env python3
"""Inspect owned Podman state on an expendable GitHub-hosted Linux runner."""
import argparse
from contextlib import contextmanager, nullcontext
import json
import os
from pathlib import Path
import pwd
import re
import signal
import subprocess
import tempfile
import time


TRACE_CALLS = "%file,%process,%network,fchmod,fchown,ftruncate,mount,umount2,setns,unshare"


def verify_connections(text, rejected):
    for line in text.splitlines():
        if not re.search(r"\bconnect\(", line):
            continue
        if rejected or "AF_INET" in line:
            raise RuntimeError("unexpected transport connection attempt")
        if "AF_UNIX" in line and not re.search(r'/run/(?:dbus/system_bus_socket|user/\d+/bus)"', line):
            raise RuntimeError("unexpected local socket connection")


def main_exit(text, binary):
    match = re.search(r'^\s*(\d+)\s+execve\("' + re.escape(str(binary)) + '"', text, re.M)
    if match:
        exited = re.search(r'^\s*' + match[1] + r'\s+\+\+\+ exited with (\d+) \+\+\+', text, re.M)
        if exited:
            return int(exited[1])
    return None


def verify_report(report, uid, expected_exit):
    evaluation = report.get("evaluation", {})
    if report.get("context", {}).get("uid") != uid or evaluation.get("mode") != "live" or evaluation.get("collection") != "active":
        raise RuntimeError("incorrect execution identity or collection policy")
    expected_state = "SATISFIED" if expected_exit == 0 else "INDETERMINATE"
    if evaluation.get("requirement", {}).get("state") != expected_state:
        raise RuntimeError("incorrect inspection verdict")
    if expected_exit == 0:
        for key in ("runtime.podman", "runtime.podman.info"):
            if report.get("capabilities", {}).get(key, {}).get("state") != "supported":
                raise RuntimeError("successful report lacks complete inspection predicates")


@contextmanager
def owned_config(path, text):
    # This harness is gated to an explicitly expendable CI host. Restore the
    # host configuration even on failed validation; never reset/prune storage.
    previous = path.read_bytes() if path.exists() else None
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    try:
        yield
    finally:
        if previous is None:
            path.unlink()
        else:
            path.write_bytes(previous)


def environment_for(directory, account):
    directory.mkdir(parents=True, exist_ok=False)
    directory.chmod(0o755)
    result = {"PATH": "/usr/bin:/bin", "LC_ALL": "C"}
    for key, name in (("HOME", "home"), ("XDG_CONFIG_HOME", "config"),
                      ("XDG_DATA_HOME", "data"), ("XDG_RUNTIME_DIR", "runtime")):
        path = directory / name
        path.mkdir(mode=0o700)
        os.chown(path, account.pw_uid, account.pw_gid)
        result[key] = str(path)
    return result


def trace_case(binary, executable, tracer, destination, account, environment, scenario):
    trace = destination / f"{scenario}.trace"
    stdout, stderr = destination / f"{scenario}.json", destination / f"{scenario}.stderr"
    rejected, cancelled = scenario.startswith("remote"), scenario == "cancelled"
    expected_exit = 2 if rejected or cancelled else 0
    argv = [str(binary), "--runtime", "podman", "--active", "--podman-path", str(executable), "--json"]
    # A root tracer with -u preserves the real account's credentials, groups and
    # setuid newuidmap semantics. Namespace UID 0 is not host-root evidence.
    command = [str(tracer), "-f", "-q", "-s", "256", "-u", account.pw_name, "-o", str(trace), "-e", "trace=" + TRACE_CALLS]
    if cancelled:
        # Deterministically exceed the 5s version budget in the real runner.
        # Delay only exec entry; no fake output or replacement executable.
        command += ["-e", "inject=execve:delay_enter=6s:when=1"]
    started = time.monotonic()
    with stdout.open("wb") as out, stderr.open("wb") as err:
        process = subprocess.Popen(command + argv, env=environment, stdout=out, stderr=err, start_new_session=True)
        try:
            deadline = started + 45
            while process.poll() is None and time.monotonic() < deadline:
                captured = trace.read_text() if trace.exists() else ""
                if main_exit(captured, binary) is not None:
                    # Podman's pause process can keep -f alive after capagent
                    # exits. Detach this owned tracer; record retained processes.
                    process.send_signal(signal.SIGINT)
                    break
                time.sleep(0.05)
            process.wait(timeout=5)
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)
    captured = trace.read_text()
    actual_exit = main_exit(captured, binary)
    if actual_exit != expected_exit:
        raise RuntimeError(f"{scenario}: capagent exit {actual_exit}, expected {expected_exit}; see {stdout}")
    report = json.loads(stdout.read_bytes())
    verify_report(report, account.pw_uid, expected_exit)
    verify_connections(captured, rejected)
    codes = [item["code"] for item in report["evaluation"]["diagnostics"]]
    if cancelled and "version_timeout" not in codes:
        raise RuntimeError("cancellation did not exercise the version deadline")
    if not cancelled and '"--trace=false", "info", "--format", "json"' not in captured:
        raise RuntimeError("missing reviewed local info invocation")
    if rejected and report["capabilities"]["runtime.podman.info"]["state"] != "unavailable":
        raise RuntimeError("remote invocation did not fail closed")
    events = [line for line in captured.splitlines() if re.search(
        r'\b(?:connect|execve|mkdir|mkdirat|chmod|fchmod|clone|clone3|unshare|setns|kill|wait4|waitid)\(|\bO_(?:WRONLY|RDWR|CREAT|TRUNC)\b', line)]
    pids = sorted({int(pid) for pid in re.findall(r'^\s*(\d+)\s', captured, re.M)})
    remaining = []
    for pid in pids:
        status = Path(f"/proc/{pid}/status")
        if status.exists():
            # Only processes from this trace, not a host-wide process dump.
            remaining.append({"pid": pid, "status": status.read_text()})
    return {"scenario": scenario, "exit": actual_exit, "uid": account.pw_uid,
            "seconds": round(time.monotonic() - started, 3), "argv": argv, "environment_policy": environment,
            "events": events, "remaining_traced_processes": remaining, "diagnostics": codes}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--remote-binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--user", required=True)
    parser.add_argument("--ephemeral-host", action="store_true")
    args = parser.parse_args()
    if not args.ephemeral_host or os.geteuid() != 0 or os.environ.get("GITHUB_ACTIONS") != "true":
        raise RuntimeError("requires root and --ephemeral-host on an expendable GitHub Actions runner")
    binary, remote = args.binary.resolve(strict=True), args.remote_binary.resolve(strict=True)
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    output.chmod(0o755)
    # Runtime databases and mode-0700 directories are not report artifacts.
    # Keep them in an owned temporary tree until the ephemeral runner exits.
    state = Path(tempfile.mkdtemp(prefix="capagent-inspection-"))
    state.chmod(0o755)
    accounts = (pwd.getpwnam(args.user), pwd.getpwnam("root"))
    if accounts[0].pw_uid == 0:
        raise RuntimeError("the rootless test needs a real non-root host account")
    summary = {"status": "running", "accounts": [account.pw_name for account in accounts], "cases": [], "owned_state_root": str(state),
               "cleanup": "configuration restored; owned Podman state/processes retained until disposable runner teardown"}
    try:
        for account in accounts:
            directory = output / account.pw_name
            directory.mkdir(mode=0o755)
            environment = environment_for(state / account.pw_name, account)
            root_storage = '[storage]\ndriver="overlay"\ngraphroot=' + json.dumps(str(state / account.pw_name / "graph"))
            root_storage += '\nrunroot=' + json.dumps(str(state / account.pw_name / "runroot")) + '\n'
            storage = owned_config(Path("/etc/containers/storage.conf"), root_storage) if account.pw_uid == 0 else nullcontext()
            with storage:
                for scenario in ("fresh", "initialized", "cancelled"):
                    summary["cases"].append(trace_case(binary, Path("/usr/bin/podman"), Path("/usr/bin/strace"),
                                                      directory, account, environment, scenario))
                for executable, scenario in ((remote, "remote-only"), (Path("/usr/bin/podman"), "remote-config")):
                    mode = "false" if scenario == "remote-only" else "true"
                    with owned_config(Path("/etc/containers/containers.conf"), '[engine]\nremote=' + mode + '\n'):
                        summary["cases"].append(trace_case(binary, executable, Path("/usr/bin/strace"),
                                                          directory, account, environment, scenario))
        summary["status"] = "pass"
    finally:
        (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps({"status": summary["status"], "cases": len(summary["cases"])}))


if __name__ == "__main__":
    main()
