#!/usr/bin/env bats

load 'test_helper/bats-support/load'
load 'test_helper/bats-assert/load'

SCRIPT_PATH="${BATS_TEST_DIRNAME}/../../build/package.sh"
PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"

setup() {
    # The space proves the script quotes every path it touches.
    export TEST_DIR="${BATS_TEST_TMPDIR}/test $$"
    export PROJ_ROOT="${TEST_DIR}"
    export BUILD_VER="v1.2.3"
    DIST_DIR="${TEST_DIR}/.dist"

    mkdir -p "${TEST_DIR}/docs"
    printf "the howto\n" >"${TEST_DIR}/docs/howto.md"
    printf "the license\n" >"${TEST_DIR}/LICENSE"

    local platform
    for platform in ${PLATFORMS}; do
        write_binary "${platform}"
    done
}

teardown() {
    rm -rf "${TEST_DIR}"
}

# Each fake binary holds its own platform name, so a test can tell that an
# archive got the right one. Mode 644 proves the script sets the executable bit.
write_binary() {
    local platform="${1}"
    local os="${platform%/*}"
    local arch="${platform#*/}"
    local binary_dir="${TEST_DIR}/.bin/${arch}/${os}"

    mkdir -p "${binary_dir}"
    printf "gralph for %s\n" "${platform}" >"${binary_dir}/gralph"
    chmod 644 "${binary_dir}/gralph"
}

archive_path() {
    local os="${1}"
    local arch="${2}"

    printf "%s" "${DIST_DIR}/gralph_${BUILD_VER}_${os}_${arch}.tar.gz"
}

verify_checksums() {
    if command -v sha256sum >/dev/null 2>&1; then
        (cd "${DIST_DIR}" && sha256sum -c checksums.txt)
        return
    fi

    (cd "${DIST_DIR}" && shasum -a 256 -c checksums.txt)
}

count_dist_files() {
    find "${DIST_DIR}" -type f | wc -l | tr -d ' '
}

@test "package with all four binaries - writes four archives and checksums.txt" {
    run "${SCRIPT_PATH}"

    assert_success
    assert [ -f "$(archive_path linux amd64)" ]
    assert [ -f "$(archive_path linux arm64)" ]
    assert [ -f "$(archive_path darwin amd64)" ]
    assert [ -f "$(archive_path darwin arm64)" ]
    assert [ -f "${DIST_DIR}/checksums.txt" ]
    assert_equal "$(count_dist_files)" "5"
}

@test "package archive - holds gralph, howto.md, and LICENSE at the top level" {
    run "${SCRIPT_PATH}"
    assert_success

    run tar -tzf "$(archive_path linux amd64)"

    assert_success
    assert_line "gralph"
    assert_line "howto.md"
    assert_line "LICENSE"
    assert_equal "${#lines[@]}" "3"
}

@test "package archive - gralph is that platform's binary and is executable" {
    local extract_dir="${TEST_DIR}/extract"
    mkdir -p "${extract_dir}"

    run "${SCRIPT_PATH}"
    assert_success

    tar -xzf "$(archive_path darwin arm64)" -C "${extract_dir}"

    assert [ -x "${extract_dir}/gralph" ]
    run cat "${extract_dir}/gralph"
    assert_output "gralph for darwin/arm64"
    run cat "${extract_dir}/howto.md"
    assert_output "the howto"
    run cat "${extract_dir}/LICENSE"
    assert_output "the license"
}

@test "package checksums.txt - lists the four archives by bare name and verifies" {
    run "${SCRIPT_PATH}"
    assert_success

    run verify_checksums

    assert_success
    assert_line "gralph_v1.2.3_linux_amd64.tar.gz: OK"
    assert_line "gralph_v1.2.3_linux_arm64.tar.gz: OK"
    assert_line "gralph_v1.2.3_darwin_amd64.tar.gz: OK"
    assert_line "gralph_v1.2.3_darwin_arm64.tar.gz: OK"
}

@test "package without BUILD_VER - fails saying it is required" {
    run env -u BUILD_VER "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR: BUILD_VER is required"
    assert [ ! -e "${DIST_DIR}" ]
}

@test "package with a version that is not vMAJOR.MINOR.PATCH - fails naming it" {
    run env BUILD_VER="1.2" "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR: BUILD_VER must look like v1.2.3, got: 1.2"
}

@test "package with a path in the version - fails and writes nothing" {
    run env BUILD_VER="v1.2.3/../x" "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR: BUILD_VER must look like v1.2.3"
    assert [ ! -e "${DIST_DIR}" ]
}

@test "package with one binary missing - fails naming it and writes nothing" {
    rm "${TEST_DIR}/.bin/arm64/darwin/gralph"

    run "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR: missing file: ${TEST_DIR}/.bin/arm64/darwin/gralph"
    assert [ ! -e "${DIST_DIR}" ]
}

@test "package with the HOWTO missing - fails naming it" {
    rm "${TEST_DIR}/docs/howto.md"

    run "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR: missing file: ${TEST_DIR}/docs/howto.md"
}

@test "package with the LICENSE missing - fails naming it" {
    rm "${TEST_DIR}/LICENSE"

    run "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR: missing file: ${TEST_DIR}/LICENSE"
}

@test "package with DIST_DIR a regular file - fails and leaves the file alone" {
    printf "not a folder\n" >"${TEST_DIR}/dist_file"

    run env DIST_DIR="${TEST_DIR}/dist_file" "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR:"
    run cat "${TEST_DIR}/dist_file"
    assert_output "not a folder"
}

@test "package with a newline in the version - fails and writes nothing" {
    run env BUILD_VER=$'v1.2.3\nx' "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR: BUILD_VER must look like v1.2.3"
    assert [ ! -e "${DIST_DIR}" ]
}

# A fake tar that always fails stands in for any packaging error after the
# input check, without relying on file permissions (this must pass as root).
@test "package failing while archiving - fails and leaves an existing DIST_DIR as it was" {
    local fake_bin="${TEST_DIR}/fake_bin"
    mkdir -p "${fake_bin}" "${DIST_DIR}"
    printf '#!/bin/sh\nexit 1\n' >"${fake_bin}/tar"
    chmod 755 "${fake_bin}/tar"
    printf "old\n" >"${DIST_DIR}/gralph_v0.0.1_linux_amd64.tar.gz"
    printf "mine\n" >"${DIST_DIR}/notes.txt"

    run env PATH="${fake_bin}:${PATH}" "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR:"
    assert [ -f "${DIST_DIR}/gralph_v0.0.1_linux_amd64.tar.gz" ]
    assert [ -f "${DIST_DIR}/notes.txt" ]
    assert_equal "$(count_dist_files)" "2"
}

# A fake mv that always fails stands in for a full disk at the last step,
# without relying on file permissions (this must pass as root).
@test "package when moving the new files fails - keeps the earlier archives" {
    local fake_bin="${TEST_DIR}/fake_bin"
    mkdir -p "${fake_bin}" "${DIST_DIR}"
    printf '#!/bin/sh\nexit 1\n' >"${fake_bin}/mv"
    chmod 755 "${fake_bin}/mv"
    printf "old\n" >"${DIST_DIR}/gralph_v0.0.1_linux_amd64.tar.gz"
    printf "mine\n" >"${DIST_DIR}/notes.txt"

    run env PATH="${fake_bin}:${PATH}" "${SCRIPT_PATH}"

    assert_failure 1
    assert_output --partial "ERROR:"
    assert [ -f "${DIST_DIR}/gralph_v0.0.1_linux_amd64.tar.gz" ]
    assert [ -f "${DIST_DIR}/notes.txt" ]
}

@test "package run twice - replaces an older version's archives and keeps other files" {
    mkdir -p "${DIST_DIR}"
    printf "old\n" >"${DIST_DIR}/gralph_v0.0.1_linux_amd64.tar.gz"
    printf "old\n" >"${DIST_DIR}/checksums.txt"
    printf "mine\n" >"${DIST_DIR}/notes.txt"

    run "${SCRIPT_PATH}"
    assert_success
    run "${SCRIPT_PATH}"
    assert_success

    assert [ ! -e "${DIST_DIR}/gralph_v0.0.1_linux_amd64.tar.gz" ]
    assert [ -f "${DIST_DIR}/notes.txt" ]
    assert_equal "$(count_dist_files)" "6"
    run verify_checksums
    assert_success
}

@test "package with OUTPUT_DIR and DIST_DIR set - reads and writes there" {
    mv "${TEST_DIR}/.bin" "${TEST_DIR}/binaries"
    local binaries_dir="${TEST_DIR}/binaries"
    local out_dir="${TEST_DIR}/out"

    run env OUTPUT_DIR="${binaries_dir}" DIST_DIR="${out_dir}" "${SCRIPT_PATH}"

    assert_success
    assert [ -f "${out_dir}/gralph_v1.2.3_linux_amd64.tar.gz" ]
    assert [ ! -e "${TEST_DIR}/.dist" ]
}

@test "package run from another directory - still finds the project" {
    cd /

    run "${SCRIPT_PATH}"

    assert_success
    assert [ -f "${DIST_DIR}/checksums.txt" ]
}
