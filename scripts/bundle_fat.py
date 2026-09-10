#!/usr/bin/env python3
"""Bundle only complete static payload sets using private verified tooling."""
import os
import subprocess
import sys

from microfat_tooling import ROOT, filename, snapshot, install_exit_handlers
from release_artifacts import VARIANTS, verify_bundles, verify_elf


def main():
    mode = sys.argv[1] if len(sys.argv) == 2 else os.environ.get("MICROFAT_STUB_MODE", "full")
    if len(sys.argv) > 2 or mode not in ("full", "minimal"):
        raise ValueError("launcher mode must be full or minimal")
    with snapshot() as (tooling, _, rows):
        for arch, levels in VARIANTS.items():
            stub = tooling / filename(next(r for r in rows if r["role"] == mode and r["arch"] == arch))
            verify_elf(stub, arch)
            args = [str(tooling / "microfat"), "pack", "--stub", str(stub), "--name", "capagent",
                    "--arch", arch, "--profile", "balanced", "--dict"]
            for level in levels:
                payload = ROOT / "dist" / f"capagent-{arch}_linux_{arch}_{level}" / "capagent"
                verify_elf(payload, arch)
                args.extend(["-v", f"{level}={payload}"])
            output = ROOT / "dist/fat" / mode / arch / "capagent"
            output.parent.mkdir(parents=True, exist_ok=True)
            subprocess.run([*args, "-o", str(output)], check=True, timeout=180)
            output.chmod(0o755)
    verify_bundles(mode)


if __name__ == "__main__":
    install_exit_handlers()
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        print(f"bundling failed: {error}", file=sys.stderr)
        sys.exit(1)
