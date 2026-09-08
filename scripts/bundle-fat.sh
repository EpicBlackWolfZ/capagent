#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# scripts/bundle-fat.sh — Assemble universal microfat binaries for capagent
#
# This script runs AFTER all GoReleaser builds have completed. All variant
# binaries must already exist in dist/ — any missing variant is a fatal error.
# ==============================================================================

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
BIN_DIR="${ROOT_DIR}/bin"

# Determine stub mode: "minimal" (for production releases) or "full" (for testing/debug)
STUB_MODE="${1:-${MICROFAT_STUB_MODE:-"full"}}"

# 1. Ensure microfat CLI and stubs are present
"${ROOT_DIR}/scripts/ensure-microfat.sh"

MICROFAT="${BIN_DIR}/microfat"
if [[ ! -x "${MICROFAT}" ]] && command -v microfat &>/dev/null; then
    MICROFAT="$(command -v microfat)"
fi

if [[ "${STUB_MODE}" == "minimal" ]]; then
    STUB_AMD64="${BIN_DIR}/microfat-stub-minimal-amd64"
    STUB_ARM64="${BIN_DIR}/microfat-stub-minimal-arm64"
    echo "==> Using MINIMAL stub (production release mode)"
else
    STUB_AMD64="${BIN_DIR}/microfat-stub-full-amd64"
    STUB_ARM64="${BIN_DIR}/microfat-stub-full-arm64"
    echo "==> Using FULL stub (testing and debugging mode)"
fi

# Helper to assert that a required file exists, failing hard if missing.
# This script is called AFTER all builds complete; missing files indicate
# a build failure, not a timing issue. Never silently skip.
require_file() {
    local path="$1"
    local desc="${2:-"artifact"}"
    if [[ ! -f "$path" ]]; then
        echo "❌ FATAL: Required ${desc} missing: ${path}" >&2
        echo "   Release packaging invariant violated. Failing hard." >&2
        exit 1
    fi
}

echo "==> Starting microfat universal fat binary assembly..."

# 2. Assemble AMD64 Universal Fat Binary (v1, v2, v3, v4)
echo "==> Verifying required AMD64 variant artifacts..."
V1="${DIST_DIR}/capagent-amd64_linux_amd64_v1/capagent"
V2="${DIST_DIR}/capagent-amd64_linux_amd64_v2/capagent"
V3="${DIST_DIR}/capagent-amd64_linux_amd64_v3/capagent"
V4="${DIST_DIR}/capagent-amd64_linux_amd64_v4/capagent"

require_file "${STUB_AMD64}" "AMD64 launcher stub (${STUB_MODE})"
require_file "${V1}" "AMD64 v1 variant binary"
require_file "${V2}" "AMD64 v2 variant binary"
require_file "${V3}" "AMD64 v3 variant binary"
require_file "${V4}" "AMD64 v4 variant binary"

FAT_AMD64_DIR="${DIST_DIR}/fat/amd64"
OUT_AMD64="${FAT_AMD64_DIR}/capagent"

mkdir -p "${FAT_AMD64_DIR}"
echo "==> Bundling capagent AMD64 universal fat binary (v1, v2, v3, v4) with shared dictionary..."
"${MICROFAT}" pack \
    --stub "${STUB_AMD64}" \
    --name capagent \
    --arch amd64 \
    --profile balanced \
    --dict \
    -v v1="${V1}" \
    -v v2="${V2}" \
    -v v3="${V3}" \
    -v v4="${V4}" \
    -o "${OUT_AMD64}"
chmod +x "${OUT_AMD64}"

if [[ "$(uname -m)" == "x86_64" ]]; then
    echo "==> Smoke testing self-bundled AMD64 fat binary..."
    "${OUT_AMD64}" --help > /dev/null
    "${OUT_AMD64}" --version
    "${MICROFAT}" inspect "${OUT_AMD64}"
    "${MICROFAT}" verify "${OUT_AMD64}"
fi
echo "✔ capagent AMD64 universal fat binary successfully bundled: ${OUT_AMD64}"

# 3. Assemble ARM64 Universal Fat Binary (v8.0, v8.2, v9.0)
echo "==> Verifying required ARM64 variant artifacts..."
ARM_V80="${DIST_DIR}/capagent-arm64_linux_arm64_v8.0/capagent"
ARM_V82="${DIST_DIR}/capagent-arm64_linux_arm64_v8.2/capagent"
ARM_V90="${DIST_DIR}/capagent-arm64_linux_arm64_v9.0/capagent"

require_file "${STUB_ARM64}" "ARM64 launcher stub (${STUB_MODE})"
require_file "${ARM_V80}" "ARM64 v8.0 variant binary"
require_file "${ARM_V82}" "ARM64 v8.2 variant binary"
require_file "${ARM_V90}" "ARM64 v9.0 variant binary"

FAT_ARM64_DIR="${DIST_DIR}/fat/arm64"
OUT_ARM64="${FAT_ARM64_DIR}/capagent"

mkdir -p "${FAT_ARM64_DIR}"
echo "==> Bundling capagent ARM64 universal fat binary (v8.0, v8.2, v9.0) with shared dictionary..."
"${MICROFAT}" pack \
    --stub "${STUB_ARM64}" \
    --name capagent \
    --arch arm64 \
    --profile balanced \
    --dict \
    -v v8.0="${ARM_V80}" \
    -v v8.2="${ARM_V82}" \
    -v v9.0="${ARM_V90}" \
    -o "${OUT_ARM64}"
chmod +x "${OUT_ARM64}"

if [[ "$(uname -m)" == "aarch64" ]]; then
    echo "==> Smoke testing self-bundled ARM64 fat binary..."
    "${OUT_ARM64}" --help > /dev/null
    "${OUT_ARM64}" --version
    "${MICROFAT}" inspect "${OUT_ARM64}"
    "${MICROFAT}" verify "${OUT_ARM64}"
fi
echo "✔ capagent ARM64 universal fat binary successfully bundled: ${OUT_ARM64}"

echo "✔ Fat binary packaging phase completed successfully."
