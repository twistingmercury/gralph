#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJ_ROOT="${PROJ_ROOT:-$(cd "${SCRIPT_DIR}/.." && pwd)}"

OUTPUT_DIR="${OUTPUT_DIR:-${PROJ_ROOT}/.bin}"
DIST_DIR="${DIST_DIR:-${PROJ_ROOT}/.dist}"

readonly HOWTO_FILE="${PROJ_ROOT}/docs/howto.md"
readonly LICENSE_FILE="${PROJ_ROOT}/LICENSE"
readonly PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"
readonly VERSION_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+$'

STAGE_DIR=""

validate_args() {
    if [ -z "${BUILD_VER:-}" ]; then
        printf "ERROR: BUILD_VER is required, for example BUILD_VER=v1.2.3\n" >&2
        return 1
    fi

    if ! printf "%s" "${BUILD_VER}" | grep -Eq "${VERSION_PATTERN}"; then
        printf "ERROR: BUILD_VER must look like v1.2.3, got: %s\n" "${BUILD_VER}" >&2
        return 1
    fi

    return 0
}

cleanup() {
    if [ -n "${STAGE_DIR}" ]; then
        rm -rf "${STAGE_DIR}"
    fi

    return 0
}

binary_path() {
    local platform="${1}"
    local os="${platform%/*}"
    local arch="${platform#*/}"

    printf "%s" "${OUTPUT_DIR}/${arch}/${os}/gralph"
    return 0
}

check_file() {
    local file_path="${1}"

    if [ ! -f "${file_path}" ]; then
        printf "ERROR: missing file: %s\n" "${file_path}" >&2
        return 1
    fi

    return 0
}

# Every input is checked before anything is written, so a failed run never
# leaves a partial set of archives that could be uploaded.
check_inputs() {
    local platform
    local binary

    if ! check_file "${HOWTO_FILE}"; then
        return 1
    fi

    if ! check_file "${LICENSE_FILE}"; then
        return 1
    fi

    for platform in ${PLATFORMS}; do
        binary="$(binary_path "${platform}")"
        if ! check_file "${binary}"; then
            return 1
        fi
    done

    return 0
}

# Only this script's own files are removed, so a wrong DIST_DIR cannot cost
# anything else.
clear_dist() {
    mkdir -p "${DIST_DIR}"
    rm -f "${DIST_DIR}"/gralph_*.tar.gz "${DIST_DIR}/checksums.txt"
    return 0
}

package_platform() {
    local platform="${1}"
    local os="${platform%/*}"
    local arch="${platform#*/}"
    local stage="${STAGE_DIR}/${os}_${arch}"
    local archive="${DIST_DIR}/gralph_${BUILD_VER}_${os}_${arch}.tar.gz"
    local binary

    binary="$(binary_path "${platform}")"
    mkdir -p "${stage}"
    cp "${binary}" "${stage}/gralph"
    chmod 755 "${stage}/gralph"
    cp "${HOWTO_FILE}" "${stage}/howto.md"
    cp "${LICENSE_FILE}" "${stage}/LICENSE"
    tar -czf "${archive}" -C "${stage}" gralph howto.md LICENSE
    return 0
}

package_all() {
    local platform

    for platform in ${PLATFORMS}; do
        if ! package_platform "${platform}"; then
            printf "ERROR: could not package %s\n" "${platform}" >&2
            return 1
        fi
    done

    return 0
}

# Linux has sha256sum, macOS has shasum; both print the same line format.
sha256_lines() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$@"
        return 0
    fi

    shasum -a 256 "$@"
    return 0
}

# Runs inside DIST_DIR so checksums.txt holds bare file names, which is what
# `sha256sum -c` needs next to the downloads.
write_checksums() {
    (cd "${DIST_DIR}" && sha256_lines gralph_"${BUILD_VER}"_*.tar.gz >checksums.txt)
    return 0
}

main() {
    if ! validate_args; then
        return 1
    fi

    if ! check_inputs; then
        return 1
    fi

    STAGE_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'gralph-package')"
    trap cleanup EXIT

    if ! clear_dist; then
        return 1
    fi

    if ! package_all; then
        return 1
    fi

    if ! write_checksums; then
        printf "ERROR: could not write %s/checksums.txt\n" "${DIST_DIR}" >&2
        return 1
    fi

    return 0
}

main "$@"
