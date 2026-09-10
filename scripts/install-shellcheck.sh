#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
[[ "$(uname -sm)" == 'Linux x86_64' ]] || { echo 'ShellCheck bootstrap supports Linux amd64' >&2; exit 1; }
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf -- "$TEMP_DIR"' EXIT
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
    --connect-timeout 10 --max-time 60 --max-filesize 20000000 \
    --output "${TEMP_DIR}/shellcheck.tar.gz" \
    https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.linux.x86_64.tar.gz
printf '%s  %s\n' b7af85e41cc99489dcc21d66c6d5f3685138f06d34651e6d34b42ec6d54fe6f6 \
    "${TEMP_DIR}/shellcheck.tar.gz" | sha256sum --check --status
tar -xOf "${TEMP_DIR}/shellcheck.tar.gz" shellcheck-v0.11.0/shellcheck > "${TEMP_DIR}/shellcheck"
chmod 755 "${TEMP_DIR}/shellcheck"
mkdir -p "${SCRIPT_DIR}/../bin/dev-tools"
mv -- "${TEMP_DIR}/shellcheck" "${SCRIPT_DIR}/../bin/dev-tools/shellcheck"
