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
STAGE_OUT_DIR=""

validate_args() {
    if [ -z "${BUILD_VER:-}" ]; then
        printf "ERROR: BUILD_VER is required, for example BUILD_VER=v1.2.3\n" >&2
        return 1
    fi

    # grep matches per line, so a value with a newline must be refused first.
    case "${BUILD_VER}" in
        *[!v0-9.]*)
            printf "ERROR: BUILD_VER must look like v1.2.3, got: %s\n" "${BUILD_VER}" >&2
            return 1
            ;;
    esac

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

make_stage_dir() {
    if ! STAGE_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'gralph-package')"; then
        printf "ERROR: could not create a staging directory\n" >&2
        return 1
    fi

    # Everything is built here first, so a failure cannot touch DIST_DIR.
    STAGE_OUT_DIR="${STAGE_DIR}/out"
    if ! mkdir -p "${STAGE_OUT_DIR}"; then
        printf "ERROR: could not create %s\n" "${STAGE_OUT_DIR}" >&2
        return 1
    fi

    return 0
}

stage_files() {
    local platform="${1}"
    local stage="${2}"
    local binary

    binary="$(binary_path "${platform}")"
    if ! mkdir -p "${stage}"; then
        printf "ERROR: could not create %s\n" "${stage}" >&2
        return 1
    fi

    if ! cp "${binary}" "${stage}/gralph"; then
        printf "ERROR: could not copy %s\n" "${binary}" >&2
        return 1
    fi

    if ! chmod 755 "${stage}/gralph"; then
        printf "ERROR: could not make %s/gralph executable\n" "${stage}" >&2
        return 1
    fi

    if ! cp "${HOWTO_FILE}" "${stage}/howto.md"; then
        printf "ERROR: could not copy %s\n" "${HOWTO_FILE}" >&2
        return 1
    fi

    if ! cp "${LICENSE_FILE}" "${stage}/LICENSE"; then
        printf "ERROR: could not copy %s\n" "${LICENSE_FILE}" >&2
        return 1
    fi

    return 0
}

package_platform() {
    local platform="${1}"
    local os="${platform%/*}"
    local arch="${platform#*/}"
    local stage="${STAGE_DIR}/${os}_${arch}"
    local archive="${STAGE_OUT_DIR}/gralph_${BUILD_VER}_${os}_${arch}.tar.gz"

    if ! stage_files "${platform}" "${stage}"; then
        return 1
    fi

    if ! tar -czf "${archive}" -C "${stage}" gralph howto.md LICENSE; then
        printf "ERROR: could not write %s\n" "${archive}" >&2
        return 1
    fi

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
        return
    fi

    shasum -a 256 "$@"
}

# Runs inside the output folder so checksums.txt holds bare file names, which
# is what `sha256sum -c` needs next to the downloads.
write_checksums() {
    if ! (cd "${STAGE_OUT_DIR}" && sha256_lines gralph_"${BUILD_VER}"_*.tar.gz >checksums.txt); then
        printf "ERROR: could not write %s/checksums.txt\n" "${STAGE_OUT_DIR}" >&2
        return 1
    fi

    return 0
}

# Only this script's own files are removed, so a wrong DIST_DIR cannot cost
# anything else. This runs last, once every new file is known to exist.
publish() {
    if ! mkdir -p "${DIST_DIR}"; then
        printf "ERROR: could not create %s\n" "${DIST_DIR}" >&2
        return 1
    fi

    if ! rm -f "${DIST_DIR}"/gralph_*.tar.gz "${DIST_DIR}/checksums.txt"; then
        printf "ERROR: could not remove old files from %s\n" "${DIST_DIR}" >&2
        return 1
    fi

    if ! mv "${STAGE_OUT_DIR}"/gralph_*.tar.gz "${STAGE_OUT_DIR}/checksums.txt" "${DIST_DIR}/"; then
        printf "ERROR: could not move the new files into %s\n" "${DIST_DIR}" >&2
        return 1
    fi

    return 0
}

main() {
    if ! validate_args; then
        return 1
    fi

    if ! check_inputs; then
        return 1
    fi

    trap cleanup EXIT
    if ! make_stage_dir; then
        return 1
    fi

    if ! package_all; then
        return 1
    fi

    if ! write_checksums; then
        return 1
    fi

    if ! publish; then
        return 1
    fi

    return 0
}

main "$@"
