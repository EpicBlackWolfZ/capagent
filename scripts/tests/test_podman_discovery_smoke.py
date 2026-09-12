"""The smoke verifier must reject attempts, including unsuccessful mutations."""
import importlib.util
from pathlib import Path
import unittest

MODULE_PATH = Path(__file__).resolve().parents[1] / "podman-discovery-smoke.py"
SPEC = importlib.util.spec_from_file_location("podman_discovery_smoke", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class PassiveTraceTests(unittest.TestCase):
    def test_read_only_process_with_runtime_threads(self):
        MODULE.verify_trace(
            '1 execve("/capagent", ["/capagent"], 0x123) = 0\n'
            '1 openat2(3, "usr/bin/podman", {flags=O_PATH|O_CLOEXEC}, 24) = 4\n'
            '1 clone3({flags=CLONE_VM|CLONE_THREAD}, 88) = 2\n', Path("/capagent"))

    def test_side_effects_cannot_hide_behind_success_or_failure(self):
        for operation in (
            'execve("/usr/bin/podman", ["podman", "--version"], 0x123) = 0',
            'execveat(3, "", ["podman"], 0x123, AT_EMPTY_PATH) = 0',
            'clone(child_stack=NULL, flags=SIGCHLD) = 2',
            'fork() = 2', 'vfork() = 2',
            'mkdirat(AT_FDCWD, "/run/libpod", 0700) = -1 EEXIST',
            'fchmodat(AT_FDCWD, "/run/libpod", 01700) = 0',
            'openat(AT_FDCWD, "/state", O_RDWR) = -1 EACCES',
            'connect(4, {sa_family=AF_UNIX}, 110) = -1 ENOENT',
            'unshare(CLONE_NEWUSER) = 0',
        ):
            with self.subTest(operation=operation), self.assertRaises(RuntimeError):
                MODULE.verify_trace('1 execve("/capagent", [], 0x123) = 0\n2 ' + operation, Path("/capagent"))


if __name__ == "__main__":
    unittest.main()
