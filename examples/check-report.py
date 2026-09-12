#!/usr/bin/env python3
"""Fail closed on a selected flat capability key; read one JSON report on stdin."""
import json
import sys


def allowed(report, capability_id="runtime.podman.netavark"):
    if not isinstance(report, dict) or type(report.get("schema_version")) is not int:
        return False
    if report["schema_version"] != 1:
        return False
    capabilities = report.get("capabilities", {})
    evaluation = report.get("evaluation", {})
    if not isinstance(capabilities, dict) or not isinstance(evaluation, dict):
        return False
    capability = capabilities.get(capability_id, {})
    requirement = evaluation.get("requirement", {})
    return (
        isinstance(capability, dict)
        and isinstance(requirement, dict)
        and capability.get("state") == "supported"
        and capability.get("confidence") in ("verified", "derived")
        and isinstance(capability.get("evidence"), list)
        and bool(capability["evidence"])
        and requirement.get("state") == "SATISFIED"
    )


def main():
    if len(sys.argv) > 2:
        return 2
    capability_id = sys.argv[1] if len(sys.argv) == 2 else "runtime.podman.netavark"
    if capability_id not in ("runtime.podman.netavark", "runtime.podman"):
        return 2
    try:
        report = json.load(sys.stdin)
    except (ValueError, OSError):
        return 2
    return 0 if allowed(report, capability_id) else 1


if __name__ == "__main__":
    sys.exit(main())
