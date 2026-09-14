#!/bin/sh
# collect-podman-host.sh -- standalone Linux collector (Python 3.8+ stdlib).
# Use: sh collect-podman-host.sh --help
# This is a POSIX-shell launcher, not a pure-shell implementation.
command -v python3 >/dev/null 2>&1 || {
    printf '%s\n' 'Error: python3 (>=3.8) is required; no pip packages are needed.' >&2
    exit 127
}
exec python3 -I -B - "$0" "$@" <<'__CAPAGENT_PYTHON__'
"""Portable Podman diagnostics. Embedded in collect-podman-host.sh.

Linux + Python >= 3.8, standard library only. No sudo, shell evaluation,
GNU timeout, jq, tar executable, pip packages, or DNS lookup is required.
Trusted executables/configuration are a prerequisite, not a sandbox guarantee.
"""
import argparse
import base64
import errno
import gzip
import hashlib
import hmac
import io
import ipaddress
import json
import math
import os
import re
import secrets
import selectors
import signal
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
from pathlib import Path

VERSION = "2.1.0"
SAFE_PATH = "/usr/bin:/bin"
STOP_SIGNAL = 0
TOML = None
try:
    import tomllib as TOML  # Python >= 3.11; older Pythons safely omit TOML bodies.
except ImportError:
    pass


def on_signal(signum, _frame):
    global STOP_SIGNAL
    STOP_SIGNAL = signum  # Do not raise between fork/exec and process registration.


def json_loads(text):
    def bounded_int(value):
        if len(value) > 128:
            raise ValueError("oversized JSON integer")
        return int(value)
    def bounded_float(value):
        if len(value) > 128:
            raise ValueError("oversized JSON float")
        result = float(value)
        if not math.isfinite(result):
            raise ValueError("non-finite JSON float")
        return result
    def bad_constant(_value):
        raise ValueError("non-standard JSON number")
    def unique_pairs(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate JSON key")
            result[key] = value
        return result
    return json.loads(text, parse_int=bounded_int, parse_float=bounded_float,
                      parse_constant=bad_constant, object_pairs_hook=unique_pairs)


def encode_json(obj):
    return (json.dumps(obj, indent=2, sort_keys=True, ensure_ascii=True,
                       allow_nan=False) + "\n").encode("utf-8")


def stop_group(proc, sig):
    try:
        os.killpg(proc.pid, sig)
    except (ProcessLookupError, PermissionError):
        pass


def execute(argv, env, timeout, limit, deadline=None, ignore_stop=False):
    """Bounded per-stream capture, TERM/KILL escalation, bounded pipe draining.

    No shell. Children get a new session and no inherited stdin. A process
    stuck in uninterruptible kernel I/O, or one escaping its process group,
    cannot be guaranteed dead by a portable collector.
    """
    started = time.monotonic()
    result = {"exit_code": None, "status": "not_started", "timed_out": False,
              "output_limit_exceeded": False, "pipe_drain_incomplete": False,
              "stdout_bytes": 0, "stderr_bytes": 0, "reaped": True}
    out = {"stdout": bytearray(), "stderr": bytearray()}
    if (STOP_SIGNAL and not ignore_stop) or (deadline and started >= deadline):
        result["status"] = "interrupted" if STOP_SIGNAL else "total_budget_exhausted"
        return result, b"", b""
    end = started + timeout
    if deadline is not None:
        end = min(end, deadline)
    try:
        proc = subprocess.Popen(argv, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                cwd="/", env=env, close_fds=True, start_new_session=True)
    except OSError as exc:
        result.update(status="spawn_failed", errno=exc.errno)
        return result, b"", b""
    sel = selectors.DefaultSelector()
    reason = None
    stopping_at = None
    exited_at = None
    killed = False
    try:
        for name in ("stdout", "stderr"):
            stream = getattr(proc, name)
            os.set_blocking(stream.fileno(), False)
            sel.register(stream, selectors.EVENT_READ, name)
        while True:
            now = time.monotonic()
            code = proc.poll()
            if code is not None and exited_at is None:
                exited_at = now
            if reason is None:
                if STOP_SIGNAL and not ignore_stop:
                    reason = "interrupted"
                elif now >= end:
                    reason = "timed_out"
                    result["timed_out"] = True
                elif exited_at is not None and sel.get_map() and now - exited_at >= 1:
                    reason = "pipe_drain_incomplete"
                    result["pipe_drain_incomplete"] = True
            if reason is not None and stopping_at is None:
                stopping_at = now
                stop_group(proc, signal.SIGTERM)
            if stopping_at is not None and now - stopping_at >= 0.5 and not killed:
                stop_group(proc, signal.SIGKILL)
                killed = True
            if code is not None and not sel.get_map():
                # Also terminate ordinary descendants after an aborted command.
                if stopping_at is not None and not killed:
                    stop_group(proc, signal.SIGKILL)
                break
            if stopping_at is not None and now - stopping_at >= 1.5:
                if sel.get_map():
                    result["pipe_drain_incomplete"] = True
                break
            for key, _events in sel.select(0.05):
                try:
                    block = os.read(key.fileobj.fileno(), 65536)
                except BlockingIOError:
                    continue
                if not block:
                    sel.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                name = key.data
                result[name + "_bytes"] += len(block)
                room = max(0, limit - len(out[name]))
                out[name].extend(block[:room])
                if len(block) > room:
                    result["output_limit_exceeded"] = True
                    if reason is None:
                        reason = "output_limit_exceeded"
        try:
            result["exit_code"] = proc.wait(timeout=0.2)
        except subprocess.TimeoutExpired:
            result["reaped"] = False
        result["status"] = reason or ("completed" if result["exit_code"] == 0 else "nonzero_exit")
    finally:
        for stream in (proc.stdout, proc.stderr):
            if not stream.closed:
                stream.close()
        sel.close()
        if proc.poll() is None:
            stop_group(proc, signal.SIGKILL)
            try:
                proc.wait(timeout=0.2)
            except subprocess.TimeoutExpired:
                result["reaped"] = False
        result["elapsed_seconds"] = round(time.monotonic() - started, 3)
    return result, bytes(out["stdout"]), bytes(out["stderr"])


# Filesystem inspection runs in bounded workers, too: permission failures,
# FIFO paths and stalled network mounts must not abort/stall the whole bundle.
# Workers produce a small, controlled JSON envelope; raw data is base64-framed.
IO_WORKER = r'''
import base64, errno, json, os, stat, sys
op, path, cap, entries = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
def meta(st):
    kind = ("regular" if stat.S_ISREG(st.st_mode) else
            "directory" if stat.S_ISDIR(st.st_mode) else
            "socket" if stat.S_ISSOCK(st.st_mode) else
            "symlink" if stat.S_ISLNK(st.st_mode) else "special")
    return {"kind": kind, "mode": oct(stat.S_IMODE(st.st_mode)),
            "uid": st.st_uid, "gid": st.st_gid, "size": st.st_size,
            "device": st.st_dev, "inode": st.st_ino}
r = {"status": "ok"}
try:
    ls = os.lstat(path)
    r["metadata"] = meta(ls)
    if stat.S_ISLNK(ls.st_mode):
        r["symlink_target"] = os.readlink(path)
        r["resolved_path"] = os.path.realpath(path)
    if op == "stat":
        st = os.stat(path)
        r["target_metadata"] = meta(st)
        r["executable"] = stat.S_ISREG(st.st_mode) and os.access(path, os.X_OK)
        if stat.S_ISREG(st.st_mode):
            try:
                r["file_capabilities_hex"] = os.getxattr(path, "security.capability").hex()
                r["file_capabilities_status"] = "present"
            except OSError as e:
                r["file_capabilities_errno"] = e.errno  # Keep the original observation.
                r["file_capabilities_status"] = (
                    "not_present_or_not_accessible" if e.errno == errno.ENODATA else
                    "unsupported" if e.errno in (errno.ENOTSUP, errno.EOPNOTSUPP) else
                    "unavailable")
    elif op == "list":
        names = []
        with os.scandir(path) as it:
            for entry in it:
                if len(names) >= entries:
                    r["truncated"] = True
                    break
                names.append(entry.name)
        r["entries"] = sorted(names)
    else:
        # Configuration and explicitly selected files do not follow final
        # symlinks. Metadata is retained. Known kernel/OS aliases use read-follow.
        if stat.S_ISLNK(ls.st_mode) and op != "read-follow":
            r["status"] = "symlink_content_omitted"
        else:
            st = os.stat(path) if op == "read-follow" else ls
            if not stat.S_ISREG(st.st_mode):
                r["status"] = "not_regular"
            else:
                flags = os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC | os.O_NOCTTY
                if op != "read-follow":
                    flags |= os.O_NOFOLLOW
                fd = os.open(path, flags)
                try:
                    actual = os.fstat(fd)
                    if not stat.S_ISREG(actual.st_mode):
                        r["status"] = "not_regular"
                    elif (actual.st_dev, actual.st_ino) != (st.st_dev, st.st_ino):
                        r["status"] = "changed_during_read"
                    else:
                        chunks, size = [], 0
                        while size <= cap:
                            b = os.read(fd, min(65536, cap + 1 - size))
                            if not b:
                                break
                            chunks.append(b)
                            size += len(b)
                        data = b"".join(chunks)
                        after = os.fstat(fd)
                        r["changed_during_read"] = (actual.st_size, actual.st_mtime_ns) != (after.st_size, after.st_mtime_ns)
                        r["truncated"] = len(data) > cap
                        r["data_b64"] = base64.b64encode(data[:cap]).decode("ascii")
                finally:
                    os.close(fd)
except OSError as e:
    r["status"] = {errno.ENOENT: "missing", errno.EACCES: "unreadable", errno.EPERM: "unreadable"}.get(e.errno, "io_error")
    if e.errno == errno.ENOENT and r.get("metadata", {}).get("kind") == "symlink":
        r["status"] = "dangling_symlink"
    r["errno"] = e.errno
print(json.dumps(r))
'''


class Privacy:
    SECRET = re.compile(r"(?i)(password|passwd|secret|token|credential|authorization|"
                        r"identitytoken|privatekey|accesskey|apikey|^auths?$)")
    SECRET_LINE = re.compile(r"(?i)(?<![a-z0-9_])(?:[\"']?(?:[a-z0-9_]{0,128}(?:password|passwd|secret|token|credential|authorization|api[_-]?key|access[_-]?key)|auth)[\"']?\s*[:=]|\bBearer\s+\S+)")
    PUBLIC_FILENAMES = {"containers.conf", "storage.conf", "registries.conf", "policy.json",
                        "containers.conf.d", "registries.conf.d", "auth.json", "fixture.json",
                        "summary.json", "metadata.json", "mounts.conf", "nsswitch.conf",
                        "podman-connections.json"}
    OPAQUE = {"env", "environment", "environmentfile", "environmentfiles", "envfile",
              "envfiles", "args", "argv", "execstart", "execstartpre", "execstartpost",
              "execstop", "execstoppost", "command", "commands", "annotations", "labels"}
    DNS = re.compile(r"(?<![\w.-])(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+"
                     r"[a-zA-Z]{2,63}(?![\w.-])")
    IP = re.compile(r"(?<![\w:])(?:\d{1,3}\.){3}\d{1,3}(?![\w.])|"
                    r"(?<![\w:])(?:[0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F:.]{0,39}(?:%[\w.-]+)?")

    def __init__(self, enabled, user, home, host, aliases):
        self.enabled = enabled
        self.salt = secrets.token_bytes(32)  # Never included in the bundle.
        self.user = user
        self.home = home
        self.hosts = sorted(set([h for h in aliases + [host, host.split(".")[0]] if h]),
                            key=len, reverse=True)

    def pseudonym(self, value, category):
        digest = hmac.new(self.salt, value.encode("utf-8", "replace"), hashlib.sha256).hexdigest()[:16]
        return "captured-" + category + "-" + digest

    def text(self, value):
        if not self.enabled:
            return value
        value = re.sub(r"-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----",
                       "[PRIVATE KEY OMITTED]", value, flags=re.S)
        # For optional free-form logs, omit the complete secret-bearing line.
        value = "\n".join("[OVERSIZED TEXT LINE OMITTED]" if len(line) > 16384 else
                          "[REDACTED LINE]" if self.SECRET_LINE.search(line) else line
                          for line in value.split("\n"))
        # URL userinfo can contain both a username and a password.
        value = re.sub(r"([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]+@", r"\1[REDACTED]@", value)
        if self.home and self.home != "/":
            value = value.replace(self.home, "/home/captured-user")
        value = re.sub(r"/home/[^/\s\"';)]+", "/home/captured-user", value)
        for host in self.hosts:  # FQDN before short hostname; literal, escaped matching.
            if host not in ("localhost", "localhost.localdomain"):
                value = re.sub(r"(?<![\w-])" + re.escape(host) + r"(?![\w-])", "captured-host", value)
        if self.user and self.user != "root":
            value = re.sub(r"(?<![\w-])" + re.escape(self.user) + r"(?![\w-])", "captured-user", value)
        def ip_replace(match):
            raw = match.group(0)
            try:
                addr = ipaddress.ip_address(raw.split("%", 1)[0])
            except ValueError:
                return raw
            kind = "ipv%d" % addr.version
            return self.pseudonym(raw, kind)
        value = self.IP.sub(ip_replace, value)
        # Preserve known public filenames and capagent capability identifiers.
        # Other dotted names (including private filenames) are pseudonymized.
        def dns_replace(match):
            raw = match.group(0)
            if raw in self.PUBLIC_FILENAMES or raw.startswith(("runtime.podman.", "host.", "systemd.", "quadlet.", "capability.")) or raw == "runtime.podman":
                return raw
            return self.pseudonym(raw, "name")
        value = self.DNS.sub(dns_replace, value)
        return value

    def tree(self, obj, depth=0):
        if depth > 48:
            return "[DEPTH LIMIT]"
        if isinstance(obj, dict):
            result = {}
            for key, value in obj.items():
                key = str(key)
                normalized = re.sub(r"[^a-z0-9]", "", key.lower())
                # Keep schema keys intact, but anonymize dynamic path/DNS keys.
                schema_key = key.startswith(("runtime.", "host.", "user.", "storage.", "network.", "systemd.", "quadlet.", "context.", "capability."))
                dynamic_key = "/" in key or ":" in key or ("." in key and not schema_key)
                safe_key = self.text(key) if self.enabled and dynamic_key else key
                if safe_key in result:
                    # Never merge distinct entries after anonymization.
                    safe_key = self.pseudonym(key, "key")
                if self.enabled and (self.SECRET.search(normalized) or normalized in self.OPAQUE):
                    result[safe_key] = "[REDACTED]"
                else:
                    result[safe_key] = self.tree(value, depth + 1)
            return result
        if isinstance(obj, (list, tuple)):
            return [self.tree(x, depth + 1) for x in obj]
        if isinstance(obj, str):
            return self.text(obj)
        if obj is None or isinstance(obj, (bool, int, float)):
            return obj
        return self.text(str(obj))


class Collector:
    def __init__(self, args):
        self.a = args
        self.started = time.monotonic()
        self.deadline = self.started + args.total_timeout
        self.env = {"PATH": SAFE_PATH, "LC_ALL": "C", "LANG": "C"}
        self.checks = []
        self.files = {}
        self.bytes_saved = 0
        self.warnings = []
        self.raw_budget_used = 0
        self.uid, self.gid = os.geteuid(), os.getegid()
        self.user, self.home = None, None
        self.privacy = None
        self.podman = None
        self.selected_paths = {}
        self.executable_results = {}
        self.canary_name = None
        self.canary_label = secrets.token_hex(16)
        self.canary_cleanup = "not_needed"
        self.summary = {"collection": "active" if args.active else "passive",
                        "collector_effective_uid": self.uid,
                        "canary": "not_requested",
                        "capagent": "disabled" if args.no_capagent else "not_checked",
                        "capagent_passive_report": "not_requested",
                        "capagent_active_report": "not_requested"}
        self.interrupted = False
        self.io_records = []
        self.config_helpers = []
        self.config_helper_dirs = []

    def available(self):
        return not STOP_SIGNAL and time.monotonic() < self.deadline

    def fs(self, op, path, ignore_stop=False):
        if not path:
            return {"status": "unset"}, b""
        if len(self.io_records) >= self.a.max_files * 4 and not ignore_stop:
            if "filesystem_operation_limit" not in self.warnings:
                self.warnings.append("filesystem_operation_limit")
            return {"status": "operation_limit"}, b""
        argv = [sys.executable, "-I", "-B", "-c", IO_WORKER, op, str(path),
                str(self.a.max_bytes), str(self.a.max_files)]
        result, out, _err = execute(argv, {"PATH": SAFE_PATH, "LC_ALL": "C"},
                                    self.a.io_timeout, self.a.max_bytes * 2 + 65536,
                                    None if ignore_stop else self.deadline, ignore_stop)
        record = {"operation": op, "path": str(path), "transport": result}
        payload = {}
        data = b""
        if result["status"] == "completed":
            try:
                payload = json_loads(out.decode("utf-8"))
                data = base64.b64decode(payload.pop("data_b64", ""), validate=True)
            except (ValueError, UnicodeError, TypeError):
                payload = {"status": "invalid_worker_response"}
        else:
            payload = {"status": result["status"]}
        if op.startswith("read"):
            self.raw_budget_used += len(data)
            if self.raw_budget_used > self.a.max_total_bytes:
                data = b""
                payload["status"] = "total_input_limit"
        record["result"] = payload
        self.io_records.append(record)
        return payload, data

    def save(self, name, obj, essential=False):
        # Every public artifact passes through the same privacy boundary.
        # No raw temporary command output or source files are archived.
        if isinstance(obj, bytes):
            data = self.privacy.text(obj.decode("utf-8", "replace")).encode("utf-8")
        else:
            data = encode_json(self.privacy.tree(obj))
        if not essential and (len(self.files) >= self.a.max_files or
                              self.bytes_saved + len(data) > self.a.max_total_bytes):
            if "bundle_content_limit" not in self.warnings:
                self.warnings.append("bundle_content_limit")
            return False
        self.bytes_saved -= len(self.files.get(name, b""))
        self.files[name] = data
        self.bytes_saved += len(data)
        return True

    def initialize(self):
        # Avoid pwd.getpwuid/socket.getfqdn: NSS/DNS can query external services.
        meta, data = self.fs("read-follow", "/etc/passwd")
        if meta.get("status") == "ok" and not meta.get("truncated"):
            for line in data.decode("utf-8", "replace").splitlines():
                fields = line.split(":")
                if len(fields) == 7 and fields[2] == str(self.uid):
                    self.user, self.home = fields[0], fields[5]
                    break
        self.home = os.environ.get("HOME") or self.home
        host = os.uname().nodename
        aliases = []
        meta, data = self.fs("read-follow", "/etc/hosts")
        if meta.get("status") == "ok" and not meta.get("truncated"):
            for line in data.decode("utf-8", "replace").splitlines():
                fields = line.split("#", 1)[0].split()
                if host in fields[1:] or host.split(".")[0] in fields[1:]:
                    aliases.extend(fields[1:129])
                    aliases = aliases[:128]
        self.privacy = Privacy(not self.a.no_redact, self.user, self.home, host, aliases)
        self.active_environment_errors = []
        if os.getuid() != self.uid or os.getgid() != self.gid:
            self.active_environment_errors.append("real_effective_identity_mismatch")
        if self.home:
            self.env["HOME"] = self.home
        else:
            self.active_environment_errors.append("home_unknown")
        for name in ("XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"):
            if os.environ.get(name):
                self.env[name] = os.environ[name]
        for name in ("HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"):
            value = self.env.get(name)
            if value:
                # A trailing slash is common and unambiguous. Normalize it for
                # capagent's canonical-path policy; never resolve symlinks here.
                value = value.rstrip("/") or "/"
                self.env[name] = value
            if value and (not os.path.isabs(value) or os.path.normpath(value) != value or "\x00" in value):
                self.active_environment_errors.append(name.lower() + "_invalid")
        self.home = self.env.get("HOME")
        if self.home and os.path.isabs(self.home):
            meta, _ = self.fs("stat", self.home)
            if meta.get("target_metadata", {}).get("kind") != "directory":
                self.active_environment_errors.append("home_unavailable")
        runtime = self.env.get("XDG_RUNTIME_DIR")
        # Root does not need a user runtime directory for rootful inspection.
        # A supplied directory must nevertheless belong to the effective user:
        # preserving a sudo caller's XDG_RUNTIME_DIR must not mix user contexts.
        if runtime or self.uid != 0:
            runtime_problem, _ = self.runtime_directory_problem(runtime)
            if runtime_problem:
                self.active_environment_errors.append(runtime_problem)
        self.config_home = self.env.get("XDG_CONFIG_HOME") or (os.path.join(self.home, ".config") if self.home else None)
        if self.config_home and not os.path.isabs(self.config_home):
            self.config_home = None
        self.runtime = runtime if runtime and os.path.isabs(runtime) else "/run/user/%d" % self.uid
        known_ambient = ("CONTAINER_HOST", "CONTAINER_CONNECTION", "CONTAINER_SSHKEY", "PODMAN_CONNECTIONS_CONF",
                         "CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE", "CONTAINERS_STORAGE_CONF",
                         "CONTAINERS_REGISTRIES_CONF", "CONTAINERS_REGISTRIES_CONF_DIR",
                         "STORAGE_DRIVER", "STORAGE_OPTS", "DBUS_SESSION_BUS_ADDRESS", "DBUS_SYSTEM_BUS_ADDRESS",
                         "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
                         "http_proxy", "https_proxy", "all_proxy", "no_proxy", "REGISTRY_AUTH_FILE")
        self.save("identity/context.json", {
            "uid": self.uid, "gid": self.gid, "real_uid": os.getuid(), "real_gid": os.getgid(),
            "groups": os.getgroups(), "username": self.user, "home": self.home,
            "sudo_context_present": "SUDO_USER" in os.environ,
            "environment_policy": "fixed PATH/locale; HOME and XDG only; no ambient endpoints or overrides",
            "selected_environment": self.env,
            "ambient_variables_ignored": [x for x in known_ambient if x in os.environ],
            "active_environment_errors": self.active_environment_errors,
            "systemd_scope_requested": self.a.systemd_scope,
            "systemd_scope_selected": self.systemd_scope(),
            "systemd_environment_policy": "Fixed PATH/locale only for system/logind queries; validated current-user XDG_RUNTIME_DIR additionally for user-manager queries. No ambient D-Bus addresses.",
            "note": "Measures the current UID, never automatically the sudo caller or another account. System-manager checks are independent of HOME/Podman environment validation."})

    def warn(self, code):
        if code not in self.warnings:
            self.warnings.append(code)

    def systemd_scope(self):
        # This selects systemd applicability only; never switches credentials or
        # claims to infer the actual Podman engine mode from a numeric UID.
        if self.a.systemd_scope != "auto":
            return self.a.systemd_scope
        return "system" if self.uid == 0 else "user"

    def runtime_directory_problem(self, value):
        if not value:
            return "xdg_runtime_dir_unset", {"status": "unset"}
        if (not os.path.isabs(value) or os.path.normpath(value) != value or
                "\x00" in value):
            return "xdg_runtime_dir_invalid", {"status": "invalid_path"}
        meta, _ = self.fs("stat", value)
        target = meta.get("target_metadata", {})
        if (meta.get("status") != "ok" or target.get("kind") != "directory" or
                target.get("uid") != self.uid or target.get("mode") != "0o700"):
            return "xdg_runtime_dir_not_private_owned_directory", meta
        return None, meta

    def find_executable(self, role, explicit=None, directories=None):
        paths = ([os.path.abspath(explicit)] if explicit else
                 [os.path.join(d, role) for d in (directories or ("/usr/bin", "/usr/local/bin", "/bin"))])
        observations = []
        chosen = None
        for path in paths:
            meta, _ = self.fs("stat", path)
            observations.append({"path": path, **meta})
            if chosen is None and meta.get("status") != "missing":
                # First existing/inaccessible candidate is authoritative. Never
                # silently replace a rejected explicit or earlier candidate.
                chosen = (path, meta)
        decision = {
            "candidates": observations,
            "selected": chosen[0] if chosen else None,  # Candidate, not approval.
            "approved_for_execution": None,
            "execution_allowed": False,
            "status": "not_found",
            "reason": "explicit_path_missing" if explicit else "all_candidates_missing",
            "owner_uid": None,
            "policy": {"collector_effective_uid": self.uid,
                       "allowed_owner_uids": sorted({0, self.uid}),
                       "reject_group_or_world_writable": True,
                       "note": "Metadata eligibility only, not successful execution or a sandbox. File capability xattr absence does not block selection."}}
        if chosen:
            path, meta = chosen
            target = meta.get("target_metadata", {})
            owner = target.get("uid")
            mode = target.get("mode")
            decision["owner_uid"] = owner
            if meta.get("status") != "ok":
                decision.update(status="discovery_incomplete", reason=meta.get("status", "metadata_unavailable"))
            elif (not isinstance(mode, str) or not re.fullmatch(r"0o[0-7]{1,4}", mode) or
                  type(owner) is not int or owner < 0):
                decision.update(status="discovery_incomplete", reason="metadata_missing_or_invalid")
            elif target.get("kind") != "regular":
                decision.update(status="blocked_by_execution_policy", reason="not_regular_file")
            elif not meta.get("executable"):
                decision.update(status="blocked_by_execution_policy", reason="not_executable")
            elif owner not in (0, self.uid):
                decision.update(status="blocked_by_execution_policy", reason="owner_uid_not_allowed")
            elif int(mode, 8) & 0o022:
                decision.update(status="blocked_by_execution_policy", reason="group_or_world_writable")
            else:
                decision.update(status="selected_executable", reason="policy_passed",
                                execution_allowed=True, approved_for_execution=path)
        self.executable_results[role] = decision
        self.selected_paths[role] = decision["selected"]
        # Save AFTER making the decision so the inventory and summary agree.
        self.save("executables/" + role + ".json", decision)
        if role in ("podman", "capagent") and not decision["execution_allowed"]:
            if chosen or explicit:
                self.warn(role + "_" + decision["status"])
                print("Warning: %s: %s (%s); see executables/%s.json." %
                      (role, decision["status"], decision["reason"], role), file=sys.stderr)
        return decision["approved_for_execution"]

    def run(self, name, argv, timeout=None, as_json=False, ignore_stop=False, env=None):
        result, out, err = execute(argv, env or self.env, timeout or self.a.timeout,
                                   self.a.max_bytes, None if ignore_stop else self.deadline, ignore_stop)
        result["check"] = name
        result["invocation"] = argv
        result["json_valid"] = None
        result["json_validation"] = "syntax_only" if as_json else "not_requested"
        result["stderr_retained"] = bool(self.a.include_logs or self.a.no_redact)
        parsed = None
        # A nonzero exit does not imply invalid JSON (capagent uses 1 and 2).
        if as_json and result["status"] in ("completed", "nonzero_exit"):
            try:
                parsed = json_loads(out.decode("utf-8"))
                result["json_valid"] = True
            except (ValueError, UnicodeError, RecursionError):
                result["json_valid"] = False
        self.checks.append(result)
        base = "commands/" + name
        self.save(base + ".status.json", result)
        if result["json_valid"] is True:
            self.save(base + ".stdout.json", parsed)
        elif self.a.include_logs or self.a.no_redact:
            self.save(base + ".stdout.txt", out)
        if self.a.include_logs or self.a.no_redact:
            self.save(base + ".stderr.txt", err)
        return result, out, parsed

    def read_document(self, name, path, kind="text", follow=False):
        meta, data = self.fs("read-follow" if follow else "read", path)
        status = {"source": path, **meta}
        parsed = None
        complete = meta.get("status") == "ok" and not meta.get("truncated") and not meta.get("changed_during_read")
        if self.a.no_redact and data:
            # Still use .txt, never pretend malformed or truncated data is JSON.
            self.save(name + ".raw.txt", data)
        if complete:
            try:
                text = data.decode("utf-8")
                if kind == "json":
                    parsed = json_loads(text)
                elif kind == "toml":
                    if TOML is None:
                        status["content_status"] = "omitted_no_stdlib_toml_parser"
                    else:
                        parsed = TOML.loads(text)
                elif kind == "unit":
                    # Unit syntax/continuations are not reimplemented. Default:
                    # metadata only. Explicit logs/raw mode can retain text.
                    status["content_status"] = "metadata_only_unit_may_contain_secrets"
                    if self.a.include_logs and not self.a.no_redact:
                        self.save(name + ".redacted.txt", data)
                else:
                    self.save(name + ".txt", data)
                if parsed is not None:
                    status["content_status"] = "parsed"
                    self.save(name + ".json", parsed)
            except (ValueError, UnicodeError, RecursionError):
                status["content_status"] = "omitted_invalid_document"
        elif data:
            status["content_status"] = "omitted_incomplete_document"
        self.save(name + ".source.json", status)
        return parsed, meta, data

    def host(self):
        u = os.uname()
        self.save("host/uname.json", {"sysname": u.sysname, "nodename": u.nodename,
                                     "release": u.release, "version": u.version, "machine": u.machine})
        for label, path in (("os-release", "/etc/os-release"), ("proc-cgroups", "/proc/cgroups"),
                            ("self-cgroup", "/proc/self/cgroup"), ("self-status", "/proc/self/status"),
                            ("controllers", "/sys/fs/cgroup/cgroup.controllers"),
                            ("subtree-control", "/sys/fs/cgroup/cgroup.subtree_control"),
                            ("selinux", "/sys/fs/selinux/enforce"),
                            ("apparmor", "/sys/module/apparmor/parameters/enabled"),
                            ("seccomp-actions", "/proc/sys/kernel/seccomp/actions_avail"),
                            ("max-user-namespaces", "/proc/sys/user/max_user_namespaces"),
                            ("unprivileged-userns", "/proc/sys/kernel/unprivileged_userns_clone"),
                            ("apparmor-userns", "/proc/sys/kernel/apparmor_restrict_unprivileged_userns")):
            if not self.available():
                break
            self.read_document("host/" + label, path, follow=True)
        # Kernel command lines and mount options can embed credentials. Do not
        # collect arbitrary values by default, even in otherwise redacted mode.
        if self.a.include_logs or self.a.no_redact:
            self.read_document("host/cmdline", "/proc/cmdline", follow=True)
        meta, data = self.fs("read-follow", "/proc/self/mountinfo")
        cgroups = []
        for line in data.decode("utf-8", "replace").splitlines():
            parts = line.split()
            if "-" in parts:
                index = parts.index("-")
                if len(parts) > index + 1 and parts[index + 1] in ("cgroup", "cgroup2"):
                    cgroups.append({"root": parts[3], "mountpoint": parts[4], "filesystem": parts[index + 1]})
        types = {x["filesystem"] for x in cgroups}
        complete = meta.get("status") == "ok" and not meta.get("truncated")
        hierarchy = ("hybrid" if types == {"cgroup", "cgroup2"} else "v2" if types == {"cgroup2"}
                     else "v1" if types == {"cgroup"} else "not_observed") if complete else "unknown"
        self.summary["cgroup_hierarchy"] = hierarchy
        self.save("host/cgroup-mounts.json", {"collection": meta, "mounts": cgroups, "hierarchy": hierarchy,
                                            "note": "Observed in collector namespace; controller presence does not prove user delegation."})
        meta, memory = self.fs("read-follow", "/proc/meminfo")
        total = re.search(rb"^MemTotal:\s+(\d+)\s+kB", memory, re.M)
        self.save("host/hardware.json", {"logical_cpus": os.cpu_count(),
                                         "memory_kib": int(total.group(1)) if total else None,
                                         "memory_collection": meta})
        self.summary["virtualization"] = "not_queried_passively"
        if self.a.active and self.available():
            exe = self.find_executable("systemd-detect-virt")
            if exe:
                status, data, _ = self.run("virtualization", [exe])
                value = data.decode("ascii", "replace").strip()
                if status["status"] in ("completed", "nonzero_exit") and re.fullmatch(r"[a-z0-9_-]{1,40}", value):
                    self.summary["virtualization"] = "none_detected" if value == "none" else value

    def identity(self):
        for kind in ("subuid", "subgid"):
            meta, data = self.fs("read-follow", "/etc/" + kind)
            ranges, malformed, other = [], 0, 0
            for line in data.decode("utf-8", "replace").splitlines():
                if not line.strip() or line.lstrip().startswith("#"):
                    continue
                parts = line.split(":")
                if parts[0] not in (self.user, str(self.uid)):
                    other += 1
                    continue
                if (len(parts) != 3 or not re.fullmatch(r"[0-9]{1,10}", parts[1]) or
                        not re.fullmatch(r"[0-9]{1,10}", parts[2])):
                    malformed += 1
                    continue
                start, count = int(parts[1]), int(parts[2])
                if count < 1 or start > 4294967295 or start + count > 4294967296:
                    malformed += 1
                else:
                    ranges.append({"start": start, "count": count})
            complete = meta.get("status") == "ok" and not meta.get("truncated")
            state = "unknown" if not complete else "invalid_entries" if malformed else "found" if ranges else "absent"
            self.summary[kind] = state
            self.save("identity/" + kind + ".json", {"collection": meta, "current_user_ranges": ranges,
                       "subject_uid": self.uid, "subject_username": self.user,
                       "rootful_runtime_requires_ranges": False,
                       "status": state, "malformed_current_user_entries": malformed,
                       "other_account_entries_omitted": other,
                       "scope": "local file only; external libsubid/NSS providers are not evaluated",
                       "note": "Ranges do not prove mapping helpers work. Overlap/usefulness is not assessed."})
        sockets = {}
        for label, suffix in (("user_manager", "systemd/private"), ("user_bus", "bus")):
            path = os.path.join(self.runtime, suffix)
            meta, _ = self.fs("stat", path)
            sockets[label] = {"path": path, **meta}
        linger = {"status": "username_unknown"}
        if self.user:
            linger, _ = self.fs("stat", "/var/lib/systemd/linger/" + self.user)
        scope = self.systemd_scope()
        self.summary["systemd_scope"] = scope
        self.summary["systemd_scope_requested"] = self.a.systemd_scope
        self.summary["subid_subject_uid"] = self.uid
        self.summary["subid_requirement"] = "not_required_rootful" if self.uid == 0 else "workload_dependent"
        self.save("identity/systemd.json", {
            "scope_requested": self.a.systemd_scope, "scope_selected": scope,
            "scope_selection_reason": ("explicit_option" if self.a.systemd_scope != "auto" else
                                       "effective_uid_zero" if self.uid == 0 else "effective_uid_nonzero"),
            "subject_uid": self.uid, "user_checks_applicable": scope == "user",
            "runtime_directory_source": "supplied" if self.env.get("XDG_RUNTIME_DIR") else "conventional_path_metadata_only",
            "sockets": sockets, "linger_file": linger,
            "note": "Socket/file presence is not service health. Conventional runtime paths are inventoried, never exported to create or impersonate a login session."})
        self.systemd_queries()

    def query_skip_reason(self):
        if not self.a.active:
            return "not_requested_passive"
        if STOP_SIGNAL:
            return "skipped_interrupted"
        if time.monotonic() >= self.deadline:
            return "skipped_collection_budget"
        if os.getuid() != self.uid or os.getgid() != self.gid:
            return "blocked_identity_mismatch"
        return None

    def manager_query(self, scope, executable, env, skip=None):
        """Keep a manager state distinct from the command/transport outcome."""
        result = {"scope": scope, "state": None, "status": skip or "not_checked",
                  "reason": skip, "command_status": None, "exit_code": None}
        if not skip and not executable:
            decision = self.executable_results.get("systemctl", {})
            result.update(status="unavailable", reason="systemctl_" + decision.get("status", "not_found"))
        elif not skip:
            status, out, _ = self.run(scope + "-manager", [executable, "--" + scope,
                "--no-pager", "--no-ask-password", "is-system-running"], env=env)
            value = out.decode("ascii", "replace").strip()
            allowed = {"initializing", "starting", "running", "degraded", "maintenance", "stopping", "offline", "unknown"}
            collected = status["status"] in ("completed", "nonzero_exit") and value in allowed
            result.update(state=value if collected else None,
                          status="collected" if collected else status["status"],
                          reason=None if collected else "manager_state_unavailable",
                          command_status=status["status"], exit_code=status["exit_code"])
            if status["status"] == "completed" and not collected:
                result.update(status="invalid_response", reason="unrecognized_manager_state")
        self.summary[scope + "_manager_query"] = result["state"] if result["state"] is not None else result["status"]
        self.save("identity/" + scope + "-manager-result.json", result)
        return result

    def failed_units_query(self, executable, env, skip=None):
        # Use the manager's numeric property, not a localized human table or
        # the number of output lines from a failed command. Unknown stays null.
        result = {"scope": "system", "count": None, "status": skip or "not_checked",
                  "reason": skip, "exit_code": None, "command_status": None}
        if not skip and not executable:
            decision = self.executable_results.get("systemctl", {})
            result.update(status="unavailable", reason="systemctl_" + decision.get("status", "not_found"))
        elif not skip:
            prefix = [executable, "--system", "--no-pager", "--no-ask-password"]
            status, out, _ = self.run("system-failed-count", prefix + ["show", "--property=NFailedUnits"], env=env)
            match = re.fullmatch(rb"NFailedUnits=([0-9]{1,10})", out.strip())
            count = int(match.group(1)) if match else None
            valid = status["status"] == "completed" and count is not None and count <= 4294967295
            result.update(count=count if valid else None,
                          status="collected" if valid else "invalid_response" if status["status"] == "completed" else status["status"],
                          reason=None if valid else "failed_unit_count_unavailable",
                          command_status=status["status"], exit_code=status["exit_code"])
            # Names/descriptions can disclose workload details: free-form list
            # is opt-in, and it never serves as the source of a synthetic zero.
            if (self.a.include_logs or self.a.no_redact) and self.available():
                listing, _, _ = self.run("system-failed-units", prefix + ["--plain", "--full",
                    "--no-legend", "list-units", "--state=failed"], env=env)
                result["listing_command_status"] = listing["status"]
        self.summary["system_failed_units"] = result["count"]
        self.summary["system_failed_units_query"] = result["status"]
        self.save("identity/system-failed-units-result.json", result)

    def systemd_queries(self):
        scope = self.systemd_scope()
        common_skip = self.query_skip_reason()
        # No HOME/XDG/Podman settings are needed for system/logind queries.
        # Do not forward either ambient D-Bus address, even under sudo -E.
        system_env = {"PATH": SAFE_PATH, "LC_ALL": "C", "LANG": "C"}
        systemctl = self.find_executable("systemctl") if common_skip is None else None
        self.manager_query("system", systemctl, system_env, common_skip or self.query_skip_reason())
        self.failed_units_query(systemctl, system_env, common_skip or self.query_skip_reason())

        inapplicable = ("not_applicable_rootful" if self.uid == 0 else "not_applicable_system_scope") if scope == "system" else None
        user_skip = inapplicable or self.query_skip_reason()
        user_env = dict(system_env)
        runtime_evidence = None
        runtime_problem = None
        if not user_skip:
            runtime = self.env.get("XDG_RUNTIME_DIR")
            runtime_problem, runtime_evidence = self.runtime_directory_problem(runtime)
            if runtime_problem:
                user_skip = ("skipped_missing_session_environment" if runtime_problem == "xdg_runtime_dir_unset" else
                             "skipped_invalid_session_environment")
            else:
                user_env["XDG_RUNTIME_DIR"] = runtime
        user_result = self.manager_query("user", systemctl, user_env, user_skip or self.query_skip_reason())
        if runtime_problem:
            user_result["reason"] = runtime_problem
            user_result["runtime_directory_evidence"] = runtime_evidence
            self.save("identity/user-manager-result.json", user_result)

        # logind is reached on the SYSTEM bus, so missing user-session variables
        # need not prevent show-user. A failed query never establishes false.
        linger_skip = inapplicable or self.query_skip_reason()
        linger_result = {"subject_uid": self.uid, "linger": None,
                         "status": linger_skip or "not_checked", "reason": linger_skip,
                         "command_status": None, "exit_code": None}
        if not linger_skip:
            loginctl = self.find_executable("loginctl")
            if not loginctl:
                decision = self.executable_results.get("loginctl", {})
                linger_result.update(status="unavailable", reason="loginctl_" + decision.get("status", "not_found"))
            else:
                status, out, _ = self.run("linger-query", [loginctl, "--no-pager", "--no-ask-password",
                    "show-user", str(self.uid), "--property=Linger"], env=system_env)
                value = out.decode("ascii", "replace").strip()
                valid = status["status"] == "completed" and value in ("Linger=yes", "Linger=no")
                linger_result.update(linger=(value == "Linger=yes") if valid else None,
                    status="collected" if valid else "invalid_response" if status["status"] == "completed" else status["status"],
                    reason=None if valid else "logind_query_did_not_establish_linger",
                    command_status=status["status"], exit_code=status["exit_code"])
        self.summary["linger_query"] = linger_result["status"]
        self.summary["user_linger"] = linger_result["linger"]
        self.save("identity/linger-query-result.json", linger_result)

    def configurations(self):
        sources = []
        roots = [("vendor", "/usr/share/containers"), ("system", "/etc/containers")]
        if self.config_home:
            roots.append(("user", os.path.join(self.config_home, "containers")))
        for tier, root in roots:
            for filename in ("containers.conf", "storage.conf", "registries.conf", "policy.json"):
                sources.append((tier, os.path.join(root, filename)))
            for directory in ("containers.conf.d", "registries.conf.d"):
                meta, _ = self.fs("list", os.path.join(root, directory))
                for filename in meta.get("entries", []):
                    if filename.endswith(".conf"):
                        sources.append((tier + "-dropin", os.path.join(root, directory, filename)))
        # Inventory ambient overrides, but do not apply them to runtime commands.
        # Their contents may be the cause of a shell-vs-controlled-policy mismatch.
        for name in ("CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE", "CONTAINERS_STORAGE_CONF", "CONTAINERS_REGISTRIES_CONF"):
            value = os.environ.get(name)
            if value:
                sources.append(("ambient-ignored-" + name, os.path.abspath(value)))
        value = os.environ.get("CONTAINERS_REGISTRIES_CONF_DIR")
        if value:
            root = os.path.abspath(value)
            meta, _ = self.fs("list", root)
            for filename in meta.get("entries", []):
                if filename.endswith(".conf"):
                    sources.append(("ambient-ignored-registry-dropin", os.path.join(root, filename)))
        index = []
        for i, (tier, path) in enumerate(sources[:self.a.max_files]):
            if not self.available():
                break
            label = "configs/source-%03d" % i
            parsed, meta, _data = self.read_document(label, path, "json" if path.endswith(".json") else "toml")
            index.append({"artifact": label, "tier": tier, "path": path, "status": meta.get("status")})
            if isinstance(parsed, dict):
                engine = parsed.get("engine", {})
                if isinstance(engine, dict):
                    for key in ("helper_binaries_dir", "conmon_path"):
                        values = engine.get(key, [])
                        if isinstance(values, list):
                            for value in values:
                                if isinstance(value, str) and os.path.isabs(value):
                                    if key == "helper_binaries_dir":
                                        self.config_helper_dirs.append(value)
                                    else:
                                        self.config_helpers.append(value)
                    runtimes = engine.get("runtimes", {})
                    if isinstance(runtimes, dict):
                        for values in runtimes.values():
                            if isinstance(values, list):
                                self.config_helpers.extend(x for x in values if isinstance(x, str) and os.path.isabs(x))
                storage = parsed.get("storage", {})
                if isinstance(storage, dict):
                    options = storage.get("options", {})
                    if isinstance(options, dict):
                        overlay = options.get("overlay", {})
                        if isinstance(overlay, dict) and isinstance(overlay.get("mount_program"), str):
                            self.config_helpers.append(overlay["mount_program"])
        self.save("configs/index.json", {"sources": index, "toml_parser_available": TOML is not None,
            "meaning": "Candidate source inventory, NOT an effective merged configuration. Vendor/system/user precedence, storage replacement, version-dependent drop-ins and modules differ.",
            "ambient_override_files": "Inventoried, not applied to controlled runtime commands.",
            "symlinks": "Final symlink content omitted; source metadata retained. Parent directories/configuration must be trusted.",
            "auth_files": "Standard auth/key stores and environment dumps are not requested. Explicit override/unit paths are trusted inputs; raw mode can retain sensitive contents."})
        for i, path in enumerate(self.a.unit_file):
            if self.available():
                self.read_document("selected-units/unit-%03d" % i, os.path.abspath(path), "unit")

    def helpers_and_quadlet(self):
        roles = ("crun", "runc", "conmon", "netavark", "aardvark-dns", "pasta", "slirp4netns",
                 "fuse-overlayfs", "newuidmap", "newgidmap")
        dirs = ["/usr/bin", "/usr/local/bin", "/bin", "/usr/libexec/podman", "/usr/lib/podman",
                "/usr/local/libexec/podman", "/usr/local/lib/podman"]
        configured = []
        for path in sorted(set(self.config_helpers))[:32]:
            if not self.available():
                break
            if os.path.isabs(path):
                meta, _ = self.fs("stat", path)
                configured.append({"path": path, **meta})
        self.save("helpers/configured-candidates.json", {"candidates": configured, "note": "References from candidate sources, not proof of effective selection."})
        dirs = list(dict.fromkeys(dirs + self.config_helper_dirs[:16]))
        for role in roles:
            if not self.available():
                break
            exe = self.find_executable(role, directories=dirs)
            if exe and self.a.helper_versions and role not in ("newuidmap", "newgidmap"):
                self.run("helper-" + role, [exe, "--version"])
        generators = {}
        for scope, filename in (("user", "podman-user-generator"), ("system", "podman-system-generator")):
            observations, selected, classification = [], None, "missing"
            for root in ("/run/systemd", "/etc/systemd", "/usr/local/lib/systemd", "/usr/lib/systemd", "/lib/systemd"):
                path = os.path.join(root, scope + "-generators", filename)
                meta, _ = self.fs("stat", path)
                observations.append({"path": path, **meta})
                if selected is None and meta.get("status") != "missing":
                    selected = path
                    target = meta.get("target_metadata", {})
                    if meta.get("resolved_path") == "/dev/null" or meta.get("symlink_target") == "/dev/null" or (target.get("kind") == "regular" and target.get("size") == 0):
                        classification = "masked"
                    elif meta.get("status") != "ok":
                        classification = "unknown"
                    elif meta.get("executable"):
                        classification = "executable_present"
                    else:
                        classification = "not_executable"
            generators[scope] = {"candidates": observations, "selected": selected, "status": classification}
        self.save("quadlet/generators.json", generators)
        self.summary["quadlet_user_generator"] = generators["user"]["status"]
        self.summary["quadlet_system_generator"] = generators["system"]["status"]
        roots = {"rootful": ["/run/containers/systemd", "/etc/containers/systemd", "/usr/share/containers/systemd"],
                 "rootless": [os.path.join(self.runtime, "containers/systemd")]}
        if self.config_home:
            roots["rootless"].append(os.path.join(self.config_home, "containers/systemd"))
        for prefix in ("/etc", "/usr/share"):
            roots["rootless"].extend([prefix + "/containers/systemd/users/" + str(self.uid), prefix + "/containers/systemd/users"])
        records = {}
        for scope, paths in roots.items():
            records[scope] = []
            for path in paths:
                meta, _ = self.fs("stat", path)
                records[scope].append({"path": path, **meta})
        self.save("quadlet/roots.json", {"candidate_roots": records,
            "note": "No generator execution or daemon-reload. Presence does not prove generated units or deployment readiness."})

    def runtime_inspection(self):
        self.podman = self.find_executable("podman", self.a.podman_path)
        selected_podman_path = self.selected_paths.get("podman")
        podman_decision = self.executable_results.get("podman", {})
        self.summary["podman"] = "selected_executable" if self.podman else podman_decision.get("status", "not_found")
        self.summary["podman_reason"] = podman_decision.get("reason")
        if self.a.active and self.active_environment_errors:
            self.summary["podman_inspection"] = "blocked_invalid_environment"
        elif self.a.active and self.podman and self.available():
            # --trace=false is intentionally retained as a local-build guard.
            # Unsupported builds fail closed, never retry without the guard.
            status, _out, data = self.run("podman-info", self.podman_argv("info", "--format", "json"),
                                          timeout=self.a.podman_timeout, as_json=True)
            valid_shape = isinstance(data, dict) and isinstance(data.get("host"), dict) and isinstance(data.get("store"), dict)
            self.summary["podman_inspection"] = "collected" if status["status"] == "completed" and valid_shape else "failed_or_incomplete"
            if valid_shape:
                rootless = data["host"].get("security", {}).get("rootless") if isinstance(data["host"].get("security"), dict) else None
                self.summary["runtime_reported_rootless"] = rootless if isinstance(rootless, bool) else None
        else:
            self.summary["podman_inspection"] = "not_requested" if not self.a.active else "unavailable"
        if self.a.no_capagent:
            self.summary["capagent"] = "disabled"
        elif not self.available():
            self.summary["capagent"] = "skipped_interrupted" if STOP_SIGNAL else "skipped_collection_budget"
        else:
            exe = self.find_executable("capagent", self.a.capagent)
            decision = self.executable_results.get("capagent", {})
            self.summary["capagent"] = decision.get("status", "not_found")
            self.summary["capagent_reason"] = decision.get("reason")
            self.summary["capagent_owner_uid"] = decision.get("owner_uid")
            if exe:
                argv = [exe, "--runtime", "podman", "--json", "--pretty"]
                if selected_podman_path:
                    argv += ["--podman-path", selected_podman_path]
                status, _out, data = self.run("capagent-passive", argv, timeout=self.a.podman_timeout, as_json=True)
                valid = isinstance(data, dict) and status["json_valid"] is True and status["exit_code"] in (0, 1, 2)
                self.summary["capagent"] = "json_document_exit_%d" % status["exit_code"] if valid else "failed_or_invalid_report"
                self.summary["capagent_passive_report"] = self.summary["capagent"]
                if self.a.active:
                    if self.active_environment_errors:
                        self.summary["capagent_active_report"] = "blocked_invalid_environment"
                    elif not self.podman:
                        self.summary["capagent_active_report"] = "skipped_without_approved_podman"
                    elif not self.available():
                        self.summary["capagent_active_report"] = "skipped_interrupted" if STOP_SIGNAL else "skipped_collection_budget"
                    else:
                        active, _, active_data = self.run("capagent-active", argv + ["--active"], timeout=max(45, self.a.podman_timeout), as_json=True)
                        valid_active = isinstance(active_data, dict) and active["json_valid"] is True and active["exit_code"] in (0, 1, 2)
                        self.summary["capagent_active_report"] = "json_document_exit_%d" % active["exit_code"] if valid_active else "failed_or_invalid_report"
        if self.a.canary and self.available():
            if not self.podman or self.active_environment_errors or self.summary.get("podman_inspection") != "collected":
                self.summary["canary"] = "skipped_without_successful_local_inspection"
            else:
                self.canary()

    def podman_argv(self, *args):
        return [self.podman, "--remote=false", "--trace=false"] + list(args)

    def canary(self):
        # No short-name fallback. Resolve the requested image to a full local ID
        # before run; the actual run is always --pull=never.
        image = self.a.image
        status, _out, _data = self.run("canary-image-exists", self.podman_argv("image", "exists", image))
        if status["status"] != "completed":
            if status["status"] == "nonzero_exit" and status["exit_code"] == 1 and self.a.allow_pull:
                status, _out, _data = self.run("canary-pull", self.podman_argv("pull", image), timeout=self.a.pull_timeout)
                if status["status"] != "completed":
                    self.summary["canary"] = "image_pull_failed"
                    return
            else:
                self.summary["canary"] = "image_not_local" if status["exit_code"] == 1 else "image_lookup_failed"
                return
        status, _out, data = self.run("canary-image-inspect", self.podman_argv("image", "inspect", "--format", "json", image), as_json=True)
        item = data[0] if isinstance(data, list) and len(data) == 1 and isinstance(data[0], dict) else {}
        image_id = item.get("Id", item.get("ID", ""))
        if status["status"] != "completed" or not isinstance(image_id, str) or not re.fullmatch(r"(?:sha256:)?[0-9a-f]{64}", image_id):
            self.summary["canary"] = "image_identity_unverified"
            return
        self.canary_name = "capagent-canary-" + self.canary_label
        marker = "capagent-canary-ok-" + self.canary_label
        self.canary_cleanup = "pending"
        argv = self.podman_argv("run", "--rm", "--pull=never", "--name", self.canary_name,
                               "--label", "io.capagent.collector=" + self.canary_label,
                               "--network=none", "--read-only", "--cap-drop=ALL",
                               "--security-opt=no-new-privileges", "--log-driver=none",
                               "--entrypoint=/bin/echo", image_id, marker)
        status, out, _data = self.run("canary-run", argv, timeout=self.a.podman_timeout)
        matched = out.decode("utf-8", "replace").strip() == marker
        self.summary["canary"] = "succeeded" if status["status"] == "completed" and matched else "failed_or_incomplete"
        self.save("active/canary.json", {"status": self.summary["canary"], "image_requested": image,
            "resolved_image_id": image_id, "marker_matched": matched,
            "scope": "Basic isolated /bin/echo execution; NOT a network, DNS, writable-storage, or Quadlet test.",
            "side_effects": "Podman startup/container state; pulled images intentionally retained; trusted configuration may install hooks/mounts."})

    def cleanup_canary(self):
        if self.canary_cleanup != "pending":
            return
        # Cleanup has its own short budget even after collection cancellation.
        status, _out, _data = self.run("canary-cleanup-exists", self.podman_argv("container", "exists", self.canary_name),
                                       timeout=5, ignore_stop=True)
        if status["status"] == "nonzero_exit" and status["exit_code"] == 1:
            self.canary_cleanup = "already_absent"
            return
        status, _out, data = self.run("canary-cleanup-inspect", self.podman_argv("container", "inspect", "--format", "json", self.canary_name),
                                      timeout=5, as_json=True, ignore_stop=True)
        item = data[0] if isinstance(data, list) and len(data) == 1 and isinstance(data[0], dict) else {}
        config = item.get("Config", {})
        labels = config.get("Labels", {}) if isinstance(config, dict) else {}
        cid = item.get("Id", item.get("ID", ""))
        if (status["status"] != "completed" or not isinstance(labels, dict) or
                labels.get("io.capagent.collector") != self.canary_label or
                not isinstance(cid, str) or not re.fullmatch(r"[0-9a-f]{64}", cid)):
            self.canary_cleanup = "unverified_manual_review_needed"
            return
        status, _out, _data = self.run("canary-cleanup-remove", self.podman_argv("rm", "--force", cid), timeout=5, ignore_stop=True)
        self.canary_cleanup = "removed" if status["status"] == "completed" else "failed_manual_review_needed"

    def finalize(self):
        try:
            self.cleanup_canary()
        except (OSError, ValueError, TypeError, RecursionError):
            self.canary_cleanup = "failed_manual_review_needed"
            self.warnings.append("canary_cleanup_error")
        self.summary["canary_cleanup"] = self.canary_cleanup
        self.summary["interrupted_by_signal"] = STOP_SIGNAL or None
        self.summary["collection_budget_exhausted"] = time.monotonic() >= self.deadline
        incomplete = [r for r in self.io_records if r["result"].get("status") not in
                      ("ok", "missing", "unset", "symlink_content_omitted") or r["result"].get("truncated")]
        self.summary["incomplete_filesystem_checks"] = len(incomplete)
        self.summary["warning_codes"] = self.warnings
        self.summary["privacy"] = "raw_sensitive" if self.a.no_redact else "best_effort_redacted"
        self.summary["raw_text_logs_requested"] = self.a.include_logs
        self.save("collection/io.json", self.io_records, essential=True)
        self.save("collection/commands.json", self.checks, essential=True)
        self.save("summary.json", self.summary, essential=True)
        self.save("metadata.json", {"script_version": VERSION, "collected_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "python_version": "%d.%d.%d" % sys.version_info[:3], "uid": self.uid,
            "archive_scope": "current host namespace and current effective identity",
            "systemd_scope_requested": self.a.systemd_scope, "systemd_scope_selected": self.systemd_scope(),
            "limits": {"command_seconds": self.a.timeout, "podman_seconds": self.a.podman_timeout,
                       "io_seconds": self.a.io_timeout, "collection_seconds": self.a.total_timeout,
                       "per_stream_bytes": self.a.max_bytes, "content_bytes": self.a.max_total_bytes,
                       "artifact_count": self.a.max_files},
            "privacy_notice": "Structured secrets/environment fields removed, known identity and IPs anonymized. Redaction is not a proof that arbitrary content is secret-free. Review before sharing; --include-logs increases exposure.",
            "limitations": ["No privilege escalation, credential changes, service changes, daemon-reload or prune/reset.",
                "Active commands may change trusted Podman startup state. Passive includes optional trusted capagent execution and read workers.",
                "No portable guarantee against a compromised binary, configuration hooks, escaped processes, SIGKILL or uninterruptible kernel I/O.",
                "Filesystem writes/archiving are not covered by the collection deadline; use a local output filesystem.",
                "Metadata and aggregate reports are reserved outside the content-byte budget.",
                "On Python <3.11, TOML contents omitted unless --no-redact; no regex TOML fallback.",
                "Candidate configuration inventory is not a version-specific effective-config resolver."]}, essential=True)
        return publish(self.files, self.a.output_dir)


def publish(files, output_dir):
    """Private archive, scrubbed tar ownership, atomic no-clobber publication.

    Build under a private directory ON the output filesystem. link() publishes
    a complete file atomically and refuses an existing destination. Without
    hardlink support, publish inside that exclusively created private directory.
    A filesystem that cannot enforce private mode bits is rejected.
    """
    output_dir = os.path.abspath(output_dir)
    os.makedirs(output_dir, mode=0o700, exist_ok=True)
    work = tempfile.mkdtemp(prefix=".capagent-private-", dir=output_dir)
    os.chmod(work, 0o700)
    if stat.S_IMODE(os.stat(work).st_mode) != 0o700:
        os.rmdir(work)
        raise PermissionError("output filesystem does not enforce private directory permissions")
    keep_directory = False
    partial = os.path.join(work, "bundle.partial")
    name = "podman-diagnostics-" + time.strftime("%Y%m%d_%H%M%SZ", time.gmtime()) + "-" + secrets.token_hex(8) + ".tar.gz"
    destination = os.path.join(output_dir, name)
    try:
        fd = os.open(partial, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as raw:
            if stat.S_IMODE(os.fstat(raw.fileno()).st_mode) != 0o600:
                raise PermissionError("output filesystem does not enforce private archive permissions")
            with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as zipped:
                with tarfile.open(fileobj=zipped, mode="w|", format=tarfile.PAX_FORMAT) as archive:
                    for path, data in sorted(files.items()):
                        if path.startswith("/") or ".." in path.split("/"):
                            raise ValueError("unsafe internal archive name")
                        info = tarfile.TarInfo("podman-diagnostics/" + path)
                        info.size, info.mode = len(data), 0o600
                        info.uid = info.gid = info.mtime = 0
                        info.uname = info.gname = ""
                        archive.addfile(info, io.BytesIO(data))
            raw.flush()
            os.fsync(raw.fileno())
        try:
            os.link(partial, destination)  # Atomic, no replacement of files or symlinks.
        except OSError as exc:
            unsupported = {errno.EOPNOTSUPP, errno.ENOTSUP, errno.EPERM, errno.ENOSYS, errno.EXDEV}
            if exc.errno not in unsupported:
                raise
            # No insecure "copy then chmod" or overwrite-prone rename into a
            # shared directory. The destination here is inside our private,
            # exclusively created directory; it has never existed.
            destination = os.path.join(work, name)
            os.rename(partial, destination)
            keep_directory = True
        return destination
    finally:
        try:
            os.unlink(partial)
        except FileNotFoundError:
            pass
        if not keep_directory:
            os.rmdir(work)


def positive_float(value):
    number = float(value)
    if not (0 < number <= 3600):
        raise argparse.ArgumentTypeError("must be greater than 0 and at most 3600")
    return number


def positive_int(value):
    number = int(value)
    if not 1 <= number <= 268435456:
        raise argparse.ArgumentTypeError("must be between 1 and 268435456")
    return number


def arguments(argv):
    p = argparse.ArgumentParser(prog="collect-podman-host.sh", description="Private, bounded Linux Podman diagnostics. Default: passive. Python >=3.8, stdlib only.",
        epilog="Run as the intended Podman user, normally WITHOUT sudo. Archives are local only, never uploaded. Exit 0 means archive creation, not a healthy host; signals exit 128+signal.")
    p.add_argument("-o", "--output-dir", default=".")
    p.add_argument("--active", action="store_true", help="Permit local Podman inspection and applicable systemd queries; may create runtime state.")
    p.add_argument("--systemd-scope", choices=("auto", "system", "user"), default="auto",
                   help="Systemd applicability: auto uses system for effective UID 0, user otherwise. Active system-manager checks always run; user-manager/linger queries only in user scope. Does not change the Podman identity.")
    p.add_argument("--canary", action="store_true", help="Also run one isolated /bin/echo canary; requires --active and --image.")
    p.add_argument("--image", help="Fully qualified tagged/digest image, or full local sha256 image ID. No default image.")
    p.add_argument("--allow-pull", action="store_true", help="Permit pulling a missing canary image; requires --canary.")
    p.add_argument("--podman-path", help="Explicit Podman binary; never falls back if unusable.")
    group = p.add_mutually_exclusive_group()
    group.add_argument("--capagent", help="Explicit trusted capagent binary. No working-directory auto-execution.")
    group.add_argument("--no-capagent", action="store_true", help="Disable fixed-path capagent auto-detection.")
    p.add_argument("--helper-versions", action="store_true", help="Execute selected helper --version commands; requires --active. Mapping helpers are metadata-only.")
    p.add_argument("--unit-file", action="append", default=[], help="Selected Quadlet/systemd file. Repeatable; metadata only unless --include-logs or --no-redact.")
    p.add_argument("--include-logs", action="store_true", help="Include best-effort-redacted free-form stdout/stderr and kernel command line. REVIEW BEFORE SHARING.")
    p.add_argument("--no-redact", action="store_true", help="Explicitly retain sensitive raw content. Private archive, NOT safe to share.")
    p.add_argument("--timeout", type=positive_float, default=8, help="General command budget, seconds (8).")
    p.add_argument("--podman-timeout", type=positive_float, default=30, help="Podman info/canary budget, seconds (30).")
    p.add_argument("--pull-timeout", type=positive_float, default=120, help="Explicit image-pull budget, seconds (120).")
    p.add_argument("--io-timeout", type=positive_float, default=2, help="Per filesystem-read worker budget, seconds (2).")
    p.add_argument("--total-timeout", type=positive_float, default=180, help="Collection budget, excluding bounded cleanup and archiving (180).")
    p.add_argument("--max-bytes", type=positive_int, default=1048576, help="Read/command-stream byte limit (1048576).")
    p.add_argument("--max-total-bytes", type=positive_int, default=33554432, help="Content budget, excluding manifest/metadata (33554432).")
    p.add_argument("--max-files", type=positive_int, default=512, help="Artifact/directory-entry bound (512, maximum 4096).")
    p.add_argument("-q", "--quiet", action="store_true", help="Only archive path on stdout; diagnostics stay on stderr.")
    p.add_argument("-v", "--version", action="version", version="collect-podman-host.sh " + VERSION)
    a = p.parse_args(argv)
    if a.max_files > 4096 or a.max_bytes > 16777216:
        p.error("--max-files <=4096 and --max-bytes <=16777216 are required")
    if a.canary and (not a.active or not a.image):
        p.error("--canary requires --active and --image")
    if a.allow_pull and not a.canary:
        p.error("--allow-pull requires --canary")
    if a.image and not a.canary:
        p.error("--image requires --canary")
    if a.helper_versions and not a.active:
        p.error("--helper-versions requires --active")
    if len(a.unit_file) > 32:
        p.error("at most 32 --unit-file arguments are allowed")
    if any(not x.endswith((".container", ".volume", ".network", ".build", ".pod", ".kube", ".image", ".artifact", ".service", ".conf")) for x in a.unit_file):
        p.error("--unit-file requires a Quadlet, .service or .conf file")
    if a.image:
        local_id = bool(re.fullmatch(r"(?:sha256:)?[0-9a-f]{64}", a.image))
        qualified = re.fullmatch(r"(?:localhost|[a-zA-Z0-9-]+(?:\.[a-zA-Z0-9-]+)+)(?::[0-9]+)?/"
                                 r"[a-z0-9][a-z0-9._/-]*(?::[a-zA-Z0-9_][a-zA-Z0-9_.-]*|@sha256:[0-9a-f]{64})", a.image)
        if not local_id and not qualified:
            p.error("image must be a full local SHA256 ID or registry/path:tag (or @sha256:digest); short names rejected")
        if local_id and a.allow_pull:
            p.error("cannot pull a local image ID; supply a fully qualified reference")
    return a


def main(argv):
    if not sys.platform.startswith("linux") or sys.version_info < (3, 8):
        print("Error: Linux and Python 3.8 or newer are required.", file=sys.stderr)
        return 64
    args = arguments(argv)
    os.umask(0o077)
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, on_signal)
    collector = Collector(args)
    if args.no_redact:
        print("WARNING: raw sensitive data requested. Do not share this archive without reviewing it.", file=sys.stderr)
    elif args.include_logs:
        print("WARNING: free-form log redaction is best effort. Review the archive before sharing.", file=sys.stderr)
    if "SUDO_USER" in os.environ:
        print("Notice: collecting the effective UID, not automatically the sudo caller.", file=sys.stderr)
    try:
        collector.initialize()
        for label, method in (("Host", collector.host), ("Identity", collector.identity),
                              ("Configuration", collector.configurations),
                              ("Helpers / Quadlet", collector.helpers_and_quadlet),
                              ("Runtime", collector.runtime_inspection)):
            if not collector.available():
                break
            if not args.quiet:
                print("Collecting: " + label, file=sys.stderr)
            try:
                method()
            except (OSError, ValueError, TypeError, RecursionError) as exc:
                # Preserve unrelated evidence; never put arbitrary exception
                # messages containing paths/credentials into the bundle.
                collector.warnings.append(label.lower().replace(" ", "_") + "_" + type(exc).__name__)
        archive = collector.finalize()
    except Exception as exc:
        try:
            collector.cleanup_canary()
        except Exception:
            pass
        print("Collection failed: %s (errno=%s). No complete archive was published." %
              (type(exc).__name__, getattr(exc, "errno", None)), file=sys.stderr)
        return 128 + STOP_SIGNAL if STOP_SIGNAL else 1
    if not args.quiet:
        print(json.dumps(collector.privacy.tree(collector.summary), indent=2), file=sys.stderr)
        print("Archive created; collection status is in summary.json. Review before sharing.", file=sys.stderr)
    print(archive)
    return 128 + STOP_SIGNAL if STOP_SIGNAL else 0


if __name__ == "__main__":
    # The shell launcher supplies its path as argv[1]. It is deliberately not
    # executed or used for auto-discovery relative to the working directory.
    sys.exit(main(sys.argv[2:]))

__CAPAGENT_PYTHON__
