#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# scripts/bundle-fat.sh — Assemble universal microfat binaries for capagent
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

# Helper to resolve variant path with retry/wait for concurrent GoReleaser builds
resolve_variant_with_wait() {
    local timeout=60
    local elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        for p in "$@"; do
            if [[ -f "$p" ]]; then
                echo "$p"
                return 0
            fi
        done
        sleep 0.5
        elapsed=$((elapsed + 1))
    done
    echo ""
}

echo "==> Starting microfat universal fat binary assembly..."

# 2. Assemble AMD64 Universal Fat Binary (v1, v2, v3, v4)
V1=$(resolve_variant_with_wait "${DIST_DIR}/capagent-amd64_linux_amd64_v1/capagent")
V2=$(resolve_variant_with_wait "${DIST_DIR}/capagent-amd64_linux_amd64_v2/capagent")
V3=$(resolve_variant_with_wait "${DIST_DIR}/capagent-amd64_linux_amd64_v3/capagent")
V4=$(resolve_variant_with_wait "${DIST_DIR}/capagent-amd64_linux_amd64_v4/capagent")
FAT_AMD64_DIR="${DIST_DIR}/fat/amd64"
OUT_AMD64="${FAT_AMD64_DIR}/capagent"

if [[ -n "$V1" && -n "$V2" && -n "$V3" && -n "$V4" && -f "$STUB_AMD64" ]]; then
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
else
    echo "⚠️  AMD64 build variants or stub not found; skipping AMD64 fat bundling."
fi

# 3. Assemble ARM64 Universal Fat Binary (v8.0, v8.2, v9.0)
ARM_V80=$(resolve_variant_with_wait "${DIST_DIR}/capagent-arm64_linux_arm64_v8.0/capagent")
ARM_V82=$(resolve_variant_with_wait "${DIST_DIR}/capagent-arm64_linux_arm64_v8.2/capagent")
ARM_V90=$(resolve_variant_with_wait "${DIST_DIR}/capagent-arm64_linux_arm64_v9.0/capagent")
FAT_ARM64_DIR="${DIST_DIR}/fat/arm64"
OUT_ARM64="${FAT_ARM64_DIR}/capagent"

if [[ -n "$ARM_V80" && -n "$ARM_V82" && -n "$ARM_V90" && -f "$STUB_ARM64" ]]; then
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
else
    echo "⚠️  ARM64 build variants or stub not found; skipping ARM64 fat bundling."
fi

echo "✔ Fat binary packaging phase completed."
