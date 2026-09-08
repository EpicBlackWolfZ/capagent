#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# scripts/ensure-microfat.sh — Download pre-built microfat CLI & stubs from releases
# ==============================================================================

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
VERSION_TAG="${MICROFAT_VERSION:-"v0.2.2"}"
VERSION_NUM="${VERSION_TAG#v}" # strip leading 'v', e.g. 0.2.2
RELEASE_BASE="https://github.com/EpicBlackWolfZ/microfat/releases/download/${VERSION_TAG}"

mkdir -p "${BIN_DIR}"

MICROFAT_CLI="${BIN_DIR}/microfat"
STUB_FULL_AMD64="${BIN_DIR}/microfat-stub-full-amd64"
STUB_MIN_AMD64="${BIN_DIR}/microfat-stub-minimal-amd64"
STUB_FULL_ARM64="${BIN_DIR}/microfat-stub-full-arm64"
STUB_MIN_ARM64="${BIN_DIR}/microfat-stub-minimal-arm64"

# Check if all binaries are already present
if [[ -x "${MICROFAT_CLI}" && -f "${STUB_FULL_AMD64}" && -f "${STUB_MIN_AMD64}" && -f "${STUB_FULL_ARM64}" && -f "${STUB_MIN_ARM64}" ]]; then
    echo "✔ microfat CLI and stubs (${VERSION_TAG}) already present in ${BIN_DIR}"
    exit 0
fi

echo "==> Downloading pre-built microfat release assets (${VERSION_TAG})..."

TMP_DOWNLOAD="$(mktemp -d)"
trap 'rm -rf "${TMP_DOWNLOAD}"' EXIT

# Determine host architecture for downloading the matching microfat CLI fat binary
HOST_ARCH="$(uname -m)"
case "${HOST_ARCH}" in
    x86_64|amd64)
        CLI_ARCH="amd64"
        ;;
    aarch64|arm64)
        CLI_ARCH="arm64"
        ;;
    *)
        echo "❌ Unsupported host architecture: ${HOST_ARCH}" >&2
        exit 1
        ;;
esac

# 1. Download and unpack host microfat CLI
if [[ ! -x "${MICROFAT_CLI}" ]]; then
    CLI_TAR="microfat_linux_${CLI_ARCH}_fat.tar.gz"
    echo "==> Downloading microfat CLI (${CLI_TAR})..."
    curl -sSL -f "${RELEASE_BASE}/${CLI_TAR}" -o "${TMP_DOWNLOAD}/${CLI_TAR}"
    tar -xzf "${TMP_DOWNLOAD}/${CLI_TAR}" -C "${TMP_DOWNLOAD}" ./microfat || tar -xzf "${TMP_DOWNLOAD}/${CLI_TAR}" -C "${TMP_DOWNLOAD}" microfat
    mv "${TMP_DOWNLOAD}/microfat" "${MICROFAT_CLI}"
    chmod +x "${MICROFAT_CLI}"
fi

# 2. Download AMD64 stubs (full and minimal)
if [[ ! -f "${STUB_FULL_AMD64}" ]]; then
    STUB_TAR="microfat-stub_${VERSION_NUM}_linux_amd64_v1.tar.gz"
    echo "==> Downloading AMD64 full stub (${STUB_TAR})..."
    curl -sSL -f "${RELEASE_BASE}/${STUB_TAR}" -o "${TMP_DOWNLOAD}/${STUB_TAR}"
    tar -xzf "${TMP_DOWNLOAD}/${STUB_TAR}" -C "${TMP_DOWNLOAD}" microfat-stub
    mv "${TMP_DOWNLOAD}/microfat-stub" "${STUB_FULL_AMD64}"
    chmod +x "${STUB_FULL_AMD64}"
fi

if [[ ! -f "${STUB_MIN_AMD64}" ]]; then
    STUB_TAR="microfat-stub-minimal_${VERSION_NUM}_linux_amd64_v1.tar.gz"
    echo "==> Downloading AMD64 minimal stub (${STUB_TAR})..."
    curl -sSL -f "${RELEASE_BASE}/${STUB_TAR}" -o "${TMP_DOWNLOAD}/${STUB_TAR}"
    tar -xzf "${TMP_DOWNLOAD}/${STUB_TAR}" -C "${TMP_DOWNLOAD}" microfat-stub-minimal
    mv "${TMP_DOWNLOAD}/microfat-stub-minimal" "${STUB_MIN_AMD64}"
    chmod +x "${STUB_MIN_AMD64}"
fi

# 3. Download ARM64 stubs (full and minimal)
if [[ ! -f "${STUB_FULL_ARM64}" ]]; then
    STUB_TAR="microfat-stub_${VERSION_NUM}_linux_arm64_v8.0.tar.gz"
    echo "==> Downloading ARM64 full stub (${STUB_TAR})..."
    curl -sSL -f "${RELEASE_BASE}/${STUB_TAR}" -o "${TMP_DOWNLOAD}/${STUB_TAR}"
    tar -xzf "${TMP_DOWNLOAD}/${STUB_TAR}" -C "${TMP_DOWNLOAD}" microfat-stub
    mv "${TMP_DOWNLOAD}/microfat-stub" "${STUB_FULL_ARM64}"
    chmod +x "${STUB_FULL_ARM64}"
fi

if [[ ! -f "${STUB_MIN_ARM64}" ]]; then
    STUB_TAR="microfat-stub-minimal_${VERSION_NUM}_linux_arm64_v8.0.tar.gz"
    echo "==> Downloading ARM64 minimal stub (${STUB_TAR})..."
    curl -sSL -f "${RELEASE_BASE}/${STUB_TAR}" -o "${TMP_DOWNLOAD}/${STUB_TAR}"
    tar -xzf "${TMP_DOWNLOAD}/${STUB_TAR}" -C "${TMP_DOWNLOAD}" microfat-stub-minimal
    mv "${TMP_DOWNLOAD}/microfat-stub-minimal" "${STUB_MIN_ARM64}"
    chmod +x "${STUB_MIN_ARM64}"
fi

echo "✔ Successfully prepared microfat tooling in ${BIN_DIR}:"
echo "  - CLI:                ${MICROFAT_CLI}"
echo "  - Full AMD64 stub:    ${STUB_FULL_AMD64}"
echo "  - Minimal AMD64 stub: ${STUB_MIN_AMD64}"
echo "  - Full ARM64 stub:    ${STUB_FULL_ARM64}"
echo "  - Minimal ARM64 stub: ${STUB_MIN_ARM64}"
