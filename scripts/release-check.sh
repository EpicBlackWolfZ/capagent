#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}/.."
MODE="${1:-snapshot}"
for tool in go goreleaser syft python3; do
    command -v "${tool}" >/dev/null || { echo "Required release tool missing: ${tool}" >&2; exit 1; }
done
python3 -B scripts/release_artifacts.py check-tools
BUILD_ARGS=(--snapshot)
PACKAGE_ARGS=(--snapshot --skip=sign)
case "${MODE}" in
    snapshot) ;;
    tag)
        TAG="$(git describe --exact-match --tags HEAD)"
        [[ "${TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9.-]+)?$ ]] || { echo "Invalid release tag" >&2; exit 1; }
        BUILD_ARGS=()
        PACKAGE_ARGS=("--skip=publish,sign")
        ;;
    *) echo "usage: release-check.sh [snapshot|tag]" >&2; exit 1 ;;
esac
goreleaser check --config .goreleaser.yaml
goreleaser check --config .goreleaser.release.yaml
./scripts/ensure-microfat.sh
goreleaser build --config .goreleaser.yaml "${BUILD_ARGS[@]}" --clean
./scripts/bundle-fat.sh full
./scripts/bundle-fat.sh minimal
# Do not clean or rebuild here: the metadata archives consume verified binaries.
goreleaser release --config .goreleaser.release.yaml "${PACKAGE_ARGS[@]}"
python3 -B scripts/release_artifacts.py finalize
