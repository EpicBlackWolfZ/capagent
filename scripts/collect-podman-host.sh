#!/usr/bin/env bash
# ==============================================================================
# collect-podman-host.sh — Diagnostic collector for Podman host environments
# ==============================================================================
# Collects passive host, kernel, identity, cgroups, systemd, helper binaries,
# Quadlet generators, and container configurations into a sanitized .tar.gz bundle.
#
# Designed for high portability: requires only standard Linux coreutils, bash/sh,
# tar, and gzip. Opportunistically leverages jq, python3, and capagent if available.
# ==============================================================================
# shellcheck disable=SC2012

set -euo pipefail

SCRIPT_VERSION="1.0.0"
OUTPUT_DIR="."
ACTIVE_MODE="false"
REDACT="true"
CAPAGENT_BIN=""
QUIET="false"

# ------------------------------------------------------------------------------
# Usage and CLI Parsing
# ------------------------------------------------------------------------------
usage() {
    cat <<'EOF'
Usage: collect-podman-host.sh [OPTIONS]

Collects host, kernel, user identity, systemd, helper binary, and Podman
configuration diagnostics into a timestamped, sanitized archive (.tar.gz).

Options:
  -o, --output-dir DIR   Directory where the archive will be saved (default: .)
      --active           Enable active verification (test canary container)
      --no-redact        Disable redaction (preserve raw usernames, hostnames, IPs)
      --capagent PATH    Path to capagent binary to run alongside raw collection
  -q, --quiet            Suppress terminal summary, output only archive path
  -h, --help             Show this help message and exit
  -v, --version          Print script version and exit

Examples:
  ./collect-podman-host.sh
  ./collect-podman-host.sh -o /tmp --active
  ./collect-podman-host.sh --capagent bin/capagent
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        -o|--output-dir)
            if [ $# -lt 2 ]; then
                echo "Error: $1 requires an argument" >&2
                exit 1
            fi
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --active)
            ACTIVE_MODE="true"
            shift
            ;;
        --no-redact)
            REDACT="false"
            shift
            ;;
        --capagent)
            if [ $# -lt 2 ]; then
                echo "Error: $1 requires an argument" >&2
                exit 1
            fi
            CAPAGENT_BIN="$2"
            shift 2
            ;;
        -q|--quiet)
            QUIET="true"
            shift
            ;;
        -v|--version)
            echo "collect-podman-host.sh version ${SCRIPT_VERSION}"
            exit 0
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Error: unrecognized option '$1'" >&2
            usage >&2
            exit 1
            ;;
    esac
done

# ------------------------------------------------------------------------------
# Prerequisites & Initialization
# ------------------------------------------------------------------------------
TIMESTAMP=$(date -u +"%Y%m%d_%H%M%SZ")
REAL_USER=$(id -un 2>/dev/null || whoami 2>/dev/null || echo "unknown")
REAL_UID=$(id -u 2>/dev/null || echo "0")
REAL_HOME="${HOME:-/home/${REAL_USER}}"
REAL_HOST=$(hostname 2>/dev/null || uname -n 2>/dev/null || echo "localhost")

mkdir -p "${OUTPUT_DIR}"
ARCHIVE_HOSTNAME="captured-host"
if [ "${REDACT}" = "false" ]; then
    ARCHIVE_HOSTNAME="${REAL_HOST}"
fi

BUNDLE_NAME="podman-diagnostics-${ARCHIVE_HOSTNAME}-${TIMESTAMP}"
TEMP_ROOT=$(mktemp -d -t capagent-collect-XXXXXX)
STAGING_DIR="${TEMP_ROOT}/${BUNDLE_NAME}"
mkdir -p "${STAGING_DIR}"

cleanup() {
    rm -rf "${TEMP_ROOT}"
}
trap cleanup EXIT INT TERM

# ------------------------------------------------------------------------------
# Redaction Stream Filter
# ------------------------------------------------------------------------------
REAL_HOST_FQDN=$(hostname -f 2>/dev/null || echo "")

redact_stream() {
    if [ "${REDACT}" = "true" ]; then
        local user_pattern=""
        if [ "${REAL_USER}" != "root" ] && [ -n "${REAL_USER}" ] && [ "${#REAL_USER}" -ge 2 ]; then
            user_pattern="-e s|\\b${REAL_USER}\\b|captured-user|g"
        fi
        local host_pattern=""
        if [ "${REAL_HOST}" != "localhost" ] && [ -n "${REAL_HOST}" ] && [ "${#REAL_HOST}" -ge 2 ]; then
            host_pattern="-e s|\\b${REAL_HOST}\\b|captured-host|g"
        fi
        local fqdn_pattern=""
        if [ -n "${REAL_HOST_FQDN}" ] && [ "${REAL_HOST_FQDN}" != "${REAL_HOST}" ]; then
            fqdn_pattern="-e s|\\b${REAL_HOST_FQDN}\\b|captured-host|g"
        fi

        # Redact home directories, usernames, hostnames, and sensitive auth tokens
        # shellcheck disable=SC2086
        sed -E \
            -e "s|${REAL_HOME}|/home/captured-user|g" \
            -e "s|/home/[^/[:space:]\"';)]+|/home/captured-user|g" \
            ${user_pattern} \
            ${host_pattern} \
            ${fqdn_pattern} \
            -e 's/(auth|password|identityToken|secret|token)[[:space:]]*=[[:space:]]*"[^"]*"/\1 = "[REDACTED]"/gI' \
            -e 's/(auth|password|identityToken|secret|token)[[:space:]]*:[[:space:]]*"[^"]*"/\1: "[REDACTED]"/gI'
    else
        cat
    fi
}

redact_file_to() {
    local src="$1"
    local dest="$2"
    if [ -f "${src}" ]; then
        redact_stream < "${src}" > "${dest}"
    fi
}

run_and_record() {
    local outfile="$1"
    shift
    # Run command safely without breaking under set -e
    if "$@" > "${outfile}.tmp" 2>&1; then
        redact_stream < "${outfile}.tmp" > "${outfile}"
    else
        redact_stream < "${outfile}.tmp" > "${outfile}"
    fi
    rm -f "${outfile}.tmp"
}

# ------------------------------------------------------------------------------
# 1. Host & Kernel Facts
# ------------------------------------------------------------------------------
mkdir -p "${STAGING_DIR}/host"

run_and_record "${STAGING_DIR}/host/uname.txt" uname -a

if [ -f /etc/os-release ]; then
    redact_file_to /etc/os-release "${STAGING_DIR}/host/os-release"
elif [ -f /usr/lib/os-release ]; then
    redact_file_to /usr/lib/os-release "${STAGING_DIR}/host/os-release"
fi

if [ -r /proc/cmdline ]; then
    redact_file_to /proc/cmdline "${STAGING_DIR}/host/cmdline.txt"
fi

# Cgroups
CGROUP_TYPE="unknown"
if stat -f -c %T /sys/fs/cgroup >/dev/null 2>&1; then
    CGROUP_TYPE=$(stat -f -c %T /sys/fs/cgroup)
elif stat -fc %T /sys/fs/cgroup >/dev/null 2>&1; then
    CGROUP_TYPE=$(stat -fc %T /sys/fs/cgroup)
fi
echo "${CGROUP_TYPE}" > "${STAGING_DIR}/host/cgroup_type.txt"

if [ -f /proc/cgroups ] && [ -r /proc/cgroups ]; then
    cat /proc/cgroups > "${STAGING_DIR}/host/proc_cgroups.txt"
fi
if [ -f /proc/self/cgroup ] && [ -r /proc/self/cgroup ]; then
    cat /proc/self/cgroup > "${STAGING_DIR}/host/self_cgroup.txt"
fi
if [ -f /sys/fs/cgroup/cgroup.controllers ] && [ -r /sys/fs/cgroup/cgroup.controllers ]; then
    cat /sys/fs/cgroup/cgroup.controllers > "${STAGING_DIR}/host/cgroup_controllers.txt"
fi
if [ -f /sys/fs/cgroup/cgroup.subtree_control ] && [ -r /sys/fs/cgroup/cgroup.subtree_control ]; then
    cat /sys/fs/cgroup/cgroup.subtree_control > "${STAGING_DIR}/host/cgroup_subtree_control.txt"
fi
if [ -f /proc/mounts ] && [ -r /proc/mounts ]; then
    grep cgroup /proc/mounts > "${STAGING_DIR}/host/mounts_cgroup.txt" || true
fi

# Security Modules
{
    echo "--- SELinux ---"
    if command -v getenforce >/dev/null 2>&1; then
        getenforce || true
    elif [ -f /sys/fs/selinux/enforce ] && [ -r /sys/fs/selinux/enforce ]; then
        cat /sys/fs/selinux/enforce
    else
        echo "not active / not installed"
    fi

    echo "--- AppArmor ---"
    if [ -f /sys/module/apparmor/parameters/enabled ] && [ -r /sys/module/apparmor/parameters/enabled ]; then
        cat /sys/module/apparmor/parameters/enabled
    else
        echo "not enabled"
    fi

    echo "--- Seccomp ---"
    if [ -d /proc/sys/kernel/seccomp ]; then
        echo "seccomp directory present"
        if [ -f /proc/sys/kernel/seccomp/actions_avail ] && [ -r /proc/sys/kernel/seccomp/actions_avail ]; then
            echo "actions_avail: $(cat /proc/sys/kernel/seccomp/actions_avail)"
        fi
        if [ -f /proc/sys/kernel/seccomp/actions_logged ] && [ -r /proc/sys/kernel/seccomp/actions_logged ]; then
            echo "actions_logged: $(cat /proc/sys/kernel/seccomp/actions_logged)"
        fi
    elif [ -f /proc/sys/kernel/seccomp ] && [ -r /proc/sys/kernel/seccomp ]; then
        cat /proc/sys/kernel/seccomp
    elif grep -q CONFIG_SECCOMP=y "/boot/config-$(uname -r)" 2>/dev/null; then
        echo "supported (kernel config)"
    else
        echo "unknown"
    fi
} > "${STAGING_DIR}/host/security.txt"

# Virtualization & Hardware
VIRT_SYSTEM="none/bare-metal"
if command -v systemd-detect-virt >/dev/null 2>&1; then
    VIRT_SYSTEM=$(systemd-detect-virt || echo "none/bare-metal")
fi
echo "${VIRT_SYSTEM}" > "${STAGING_DIR}/host/virtualization.txt"

{
    echo "arch: $(uname -m)"
    echo "cpus: $(nproc 2>/dev/null || echo "unknown")"
    if [ -r /proc/cpuinfo ]; then
        grep -m1 "model name" /proc/cpuinfo || true
    fi
    if [ -r /proc/meminfo ]; then
        grep "MemTotal" /proc/meminfo || true
    fi
} > "${STAGING_DIR}/host/hardware.txt"

# ------------------------------------------------------------------------------
# 2. Identity & Context
# ------------------------------------------------------------------------------
mkdir -p "${STAGING_DIR}/identity"

run_and_record "${STAGING_DIR}/identity/id.txt" id

if [ -r /etc/subuid ]; then
    redact_stream < /etc/subuid > "${STAGING_DIR}/identity/subuid.txt"
else
    echo "/etc/subuid unreadable or missing" > "${STAGING_DIR}/identity/subuid.txt"
fi

if [ -r /etc/subgid ]; then
    redact_stream < /etc/subgid > "${STAGING_DIR}/identity/subgid.txt"
else
    echo "/etc/subgid unreadable or missing" > "${STAGING_DIR}/identity/subgid.txt"
fi

# User systemd & runtime directory
{
    echo "XDG_RUNTIME_DIR: ${XDG_RUNTIME_DIR:-<unset>}"
    if [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -d "${XDG_RUNTIME_DIR}" ]; then
        ls -ld "${XDG_RUNTIME_DIR}"
    fi

    echo "--- Sockets ---"
    USER_SOCKET="/run/user/${REAL_UID}/systemd/private"
    USER_BUS="/run/user/${REAL_UID}/bus"
    if [ -S "${USER_SOCKET}" ]; then
        echo "systemd private socket: present (${USER_SOCKET})"
    else
        echo "systemd private socket: absent"
    fi
    if [ -S "${USER_BUS}" ]; then
        echo "dbus user bus socket: present (${USER_BUS})"
    else
        echo "dbus user bus socket: absent"
    fi

    echo "--- Systemd User Manager ---"
    if command -v systemctl >/dev/null 2>&1; then
        systemctl --user is-system-running 2>&1 || true
    else
        echo "systemctl command unavailable"
    fi
} | redact_stream > "${STAGING_DIR}/identity/systemd_user.txt"

# Linger check
{
    echo "--- Linger Filesystem ---"
    if [ -f "/var/lib/systemd/linger/${REAL_USER}" ]; then
        echo "linger active (/var/lib/systemd/linger/${REAL_USER} exists)"
    else
        echo "linger inactive (/var/lib/systemd/linger/${REAL_USER} absent)"
    fi

    echo "--- loginctl Linger Property ---"
    if command -v loginctl >/dev/null 2>&1; then
        loginctl show-user "${REAL_USER}" --property=Linger 2>&1 || true
    else
        echo "loginctl unavailable"
    fi
} | redact_stream > "${STAGING_DIR}/identity/linger.txt"

# ------------------------------------------------------------------------------
# 3. Podman Introspection
# ------------------------------------------------------------------------------
mkdir -p "${STAGING_DIR}/podman"

PODMAN_EXEC=$(command -v podman 2>/dev/null || echo "")
if [ -n "${PODMAN_EXEC}" ]; then
    echo "path: ${PODMAN_EXEC}" > "${STAGING_DIR}/podman/executable.txt"
    ls -l "${PODMAN_EXEC}" >> "${STAGING_DIR}/podman/executable.txt" 2>&1 || true
    run_and_record "${STAGING_DIR}/podman/version.txt" "${PODMAN_EXEC}" --version
    run_and_record "${STAGING_DIR}/podman/version.json" "${PODMAN_EXEC}" version --format json
    run_and_record "${STAGING_DIR}/podman/info.json" "${PODMAN_EXEC}" info --format json
else
    echo "podman executable not found in PATH" > "${STAGING_DIR}/podman/executable.txt"
fi

# ------------------------------------------------------------------------------
# 4. Helper Binaries Inventory
# ------------------------------------------------------------------------------
mkdir -p "${STAGING_DIR}/helpers"

HELPER_ROLES="crun runc conmon netavark aardvark-dns pasta slirp4netns fuse-overlayfs"
HELPER_DIRS="/usr/local/bin /usr/bin /bin /usr/local/libexec/podman /usr/libexec/podman /usr/local/lib/podman /usr/lib/podman"

{
    echo "# Helper Binaries Inventory"
    echo "# Generated: ${TIMESTAMP}"
    echo ""
    for role in ${HELPER_ROLES}; do
        echo "=== Helper: ${role} ==="
        found="false"

        # Check PATH first
        in_path=$(command -v "${role}" 2>/dev/null || echo "")
        if [ -n "${in_path}" ]; then
            found="true"
            echo "  [PATH] ${in_path}"
            ls -l "${in_path}" 2>&1 | sed 's/^/    /'
            if [ -x "${in_path}" ]; then
                "${in_path}" --version 2>&1 | head -n 2 | sed 's/^/    version: /' || true
            fi
        fi

        # Check standard helper directories
        for dir in ${HELPER_DIRS}; do
            candidate="${dir}/${role}"
            if [ -e "${candidate}" ] && [ "${candidate}" != "${in_path}" ]; then
                found="true"
                echo "  [DIR]  ${candidate}"
                ls -l "${candidate}" 2>&1 | sed 's/^/    /'
                if [ -x "${candidate}" ]; then
                    "${candidate}" --version 2>&1 | head -n 2 | sed 's/^/    version: /' || true
                fi
            fi
        done

        if [ "${found}" = "false" ]; then
            echo "  [NOT FOUND]"
        fi
        echo ""
    done
} > "${STAGING_DIR}/helpers/inventory.txt"

# ------------------------------------------------------------------------------
# 5. Quadlet Prerequisite Telemetry
# ------------------------------------------------------------------------------
mkdir -p "${STAGING_DIR}/quadlet"

{
    echo "# Quadlet System and User Generators"
    echo ""
    echo "--- User Generators (Rootless) ---"
    for path in \
        /run/systemd/user-generators/podman-user-generator \
        /etc/systemd/user-generators/podman-user-generator \
        /usr/local/lib/systemd/user-generators/podman-user-generator \
        /usr/lib/systemd/user-generators/podman-user-generator; do
        if [ -e "${path}" ] || [ -L "${path}" ]; then
            echo "PRESENT: ${path}"
            ls -l "${path}" 2>&1 | sed 's/^/  /'
        else
            echo "ABSENT:  ${path}"
        fi
    done

    echo ""
    echo "--- System Generators (Rootful) ---"
    for path in \
        /run/systemd/system-generators/podman-system-generator \
        /etc/systemd/system-generators/podman-system-generator \
        /usr/local/lib/systemd/system-generators/podman-system-generator \
        /usr/lib/systemd/system-generators/podman-system-generator; do
        if [ -e "${path}" ] || [ -L "${path}" ]; then
            echo "PRESENT: ${path}"
            ls -l "${path}" 2>&1 | sed 's/^/  /'
        else
            echo "ABSENT:  ${path}"
        fi
    done
} > "${STAGING_DIR}/quadlet/generators.txt"

{
    echo "# Quadlet Unit Search Roots"
    echo ""
    CONFIG_HOME="${XDG_CONFIG_HOME:-${REAL_HOME}/.config}"
    RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/${REAL_UID}}"

    echo "--- Rootless Candidate Roots ---"
    for root in \
        "${CONFIG_HOME}/containers/systemd" \
        "${RUNTIME_DIR}/containers/systemd" \
        "/etc/containers/systemd/users" \
        "/etc/containers/systemd/users/${REAL_UID}"; do
        if [ -d "${root}" ]; then
            echo "EXISTS:   ${root}"
            ls -ld "${root}" 2>&1 | sed 's/^/  /'
        else
            echo "ABSENT:   ${root}"
        fi
    done

    echo ""
    echo "--- Rootful Candidate Roots ---"
    for root in \
        "/run/containers/systemd" \
        "/etc/containers/systemd" \
        "/usr/share/containers/systemd"; do
        if [ -d "${root}" ]; then
            echo "EXISTS:   ${root}"
            ls -ld "${root}" 2>&1 | sed 's/^/  /'
        else
            echo "ABSENT:   ${root}"
        fi
    done
} | redact_stream > "${STAGING_DIR}/quadlet/unit_roots.txt"

# ------------------------------------------------------------------------------
# 6. Container Configurations (Redacted)
# ------------------------------------------------------------------------------
mkdir -p "${STAGING_DIR}/configs"

copy_config_safe() {
    local src="$1"
    local rel_name="$2"
    if [ -f "${src}" ]; then
        local target="${STAGING_DIR}/configs/${rel_name}"
        mkdir -p "$(dirname "${target}")"
        redact_stream < "${src}" > "${target}"
    fi
}

copy_config_safe /etc/containers/containers.conf "etc/containers.conf"
copy_config_safe /etc/containers/storage.conf "etc/storage.conf"
copy_config_safe /etc/containers/registries.conf "etc/registries.conf"
copy_config_safe /etc/containers/policy.json "etc/policy.json"

USER_CONTAINERS_DIR="${XDG_CONFIG_HOME:-${REAL_HOME}/.config}/containers"
copy_config_safe "${USER_CONTAINERS_DIR}/containers.conf" "user/containers.conf"
copy_config_safe "${USER_CONTAINERS_DIR}/storage.conf" "user/storage.conf"
copy_config_safe "${USER_CONTAINERS_DIR}/registries.conf" "user/registries.conf"
copy_config_safe "${USER_CONTAINERS_DIR}/policy.json" "user/policy.json"

# Scan for drop-in configuration directories
if [ -d /etc/containers/containers.conf.d ]; then
    mkdir -p "${STAGING_DIR}/configs/etc/containers.conf.d"
    for f in /etc/containers/containers.conf.d/*.conf; do
        [ -e "${f}" ] || continue
        copy_config_safe "${f}" "etc/containers.conf.d/$(basename "${f}")"
    done
fi

# ------------------------------------------------------------------------------
# 7. Capagent Integration (Optional / Auto-detected)
# ------------------------------------------------------------------------------
mkdir -p "${STAGING_DIR}/capagent"

# Resolve capagent binary
if [ -z "${CAPAGENT_BIN}" ]; then
    if command -v capagent >/dev/null 2>&1; then
        CAPAGENT_BIN=$(command -v capagent)
    elif [ -x "./bin/capagent" ]; then
        CAPAGENT_BIN="./bin/capagent"
    fi
fi

if [ -n "${CAPAGENT_BIN}" ] && [ -x "${CAPAGENT_BIN}" ]; then
    echo "Found capagent: ${CAPAGENT_BIN}" > "${STAGING_DIR}/capagent/status.txt"
    run_and_record "${STAGING_DIR}/capagent/version.txt" "${CAPAGENT_BIN}" --version
    run_and_record "${STAGING_DIR}/capagent/report-passive.json" "${CAPAGENT_BIN}" --runtime podman --json --pretty

    if [ "${ACTIVE_MODE}" = "true" ]; then
        run_and_record "${STAGING_DIR}/capagent/report-active.json" "${CAPAGENT_BIN}" --runtime podman --active --json --pretty
    fi
else
    echo "capagent binary not found or not executable" > "${STAGING_DIR}/capagent/status.txt"
fi

# ------------------------------------------------------------------------------
# 8. Active Canary Test (Optional: --active)
# ------------------------------------------------------------------------------
if [ "${ACTIVE_MODE}" = "true" ]; then
    mkdir -p "${STAGING_DIR}/active"
    {
        echo "=== Active Canary Run ==="
        echo "Timestamp: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
        if [ -n "${PODMAN_EXEC}" ]; then
            echo "Command: podman run --rm docker.io/library/alpine:latest echo capagent-canary-ok"
            if "${PODMAN_EXEC}" run --rm docker.io/library/alpine:latest echo "capagent-canary-ok" 2>&1; then
                echo "CANARY_STATUS=SUCCESS"
            else
                echo "CANARY_STATUS=FAILED (attempting local/fallback run)"
                if "${PODMAN_EXEC}" run --rm alpine echo "capagent-canary-ok" 2>&1; then
                    echo "CANARY_STATUS=SUCCESS_FALLBACK"
                else
                    echo "CANARY_STATUS=FAILED_ALL"
                fi
            fi
        else
            echo "CANARY_STATUS=SKIPPED_NO_PODMAN"
        fi
    } | redact_stream > "${STAGING_DIR}/active/canary.txt"
fi

# ------------------------------------------------------------------------------
# 9. Summary & Metadata Generation
# ------------------------------------------------------------------------------
# Metadata file
cat <<EOF > "${STAGING_DIR}/metadata.env"
SCRIPT_VERSION="${SCRIPT_VERSION}"
COLLECTED_AT="${TIMESTAMP}"
ARCHIVE_NAME="${BUNDLE_NAME}.tar.gz"
REDACTED="${REDACT}"
ACTIVE_MODE="${ACTIVE_MODE}"
REAL_UID="${REAL_UID}"
ARCH="$(uname -m)"
KERNEL="$(uname -r)"
CGROUP_TYPE="${CGROUP_TYPE}"
VIRTUALIZATION="${VIRT_SYSTEM}"
EOF

# Extract quick summary values
OS_NAME="Linux"
if [ -f "${STAGING_DIR}/host/os-release" ]; then
    OS_NAME=$(grep -E "^PRETTY_NAME=" "${STAGING_DIR}/host/os-release" | cut -d= -f2- | tr -d '"' || echo "Linux")
fi

PODMAN_VER="not installed"
if [ -f "${STAGING_DIR}/podman/version.txt" ]; then
    PODMAN_VER=$(head -n 1 "${STAGING_DIR}/podman/version.txt")
fi

SUBUID_STATUS="missing/unconfigured"
if [ -s "${STAGING_DIR}/identity/subuid.txt" ] && ! grep -q "unreadable or missing" "${STAGING_DIR}/identity/subuid.txt"; then
    SUBUID_STATUS="configured"
fi

QUADLET_USER_GEN="missing"
if grep -q "^PRESENT:" "${STAGING_DIR}/quadlet/generators.txt" 2>/dev/null; then
    QUADLET_USER_GEN=$(grep "^PRESENT:" "${STAGING_DIR}/quadlet/generators.txt" | head -n 1 | awk '{print $2}')
fi

SUMMARY_FILE="${STAGING_DIR}/summary.txt"
{
    echo "================================================================================"
    echo "                  Podman Host Compatibility Diagnostics Summary                 "
    echo "================================================================================"
    echo " Collection Time  : ${TIMESTAMP}"
    echo " Host Platform    : ${OS_NAME} ($(uname -m))"
    echo " Kernel Release   : $(uname -r)"
    echo " Virtualization   : ${VIRT_SYSTEM}"
    echo " Cgroup Hierarchy : ${CGROUP_TYPE}"
    echo " Execution Identity: UID=${REAL_UID} ($([ "${REAL_UID}" = "0" ] && echo "rootful" || echo "rootless"))"
    echo " SubUID Mapping   : ${SUBUID_STATUS}"
    echo " User Systemd     : $([ -S "/run/user/${REAL_UID}/systemd/private" ] && echo "active (socket present)" || echo "unverified / socket absent")"
    echo " Podman CLI       : ${PODMAN_VER}"
    echo " Quadlet Generator: ${QUADLET_USER_GEN}"
    echo " Active Testing   : $([ "${ACTIVE_MODE}" = "true" ] && echo "enabled" || echo "disabled (passive collection)")"
    echo " Privacy/Redaction: $([ "${REDACT}" = "true" ] && echo "enabled (hostnames/usernames anonymized)" || echo "disabled (raw)")"
    echo " Capagent Report  : $([ -f "${STAGING_DIR}/capagent/report-passive.json" ] && echo "generated" || echo "skipped / not found")"
    echo "================================================================================"
} > "${SUMMARY_FILE}"

# ------------------------------------------------------------------------------
# 10. Archive Packaging
# ------------------------------------------------------------------------------
ARCHIVE_PATH="${OUTPUT_DIR}/${BUNDLE_NAME}.tar.gz"
tar -czf "${ARCHIVE_PATH}" -C "${TEMP_ROOT}" "${BUNDLE_NAME}"

# Print results
if [ "${QUIET}" = "false" ]; then
    cat "${SUMMARY_FILE}"
    echo ""
    echo "Archive created successfully:"
    echo "  => ${ARCHIVE_PATH}"
    echo ""
    echo "To inspect archive contents:"
    echo "  tar -ztvf ${ARCHIVE_PATH}"
else
    echo "${ARCHIVE_PATH}"
fi
