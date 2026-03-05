#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

IMAGE_NAME="${IMAGE_NAME:-cli-build-example}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
OUTPUT_DIR="${OUTPUT_DIR:-${PROJ_ROOT}/.bin}"

BUILD_VER="${BUILD_VER:-$(git -C "${PROJ_ROOT}" describe --tags --abbrev=0 2>/dev/null || echo 'dev')}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
BUILD_COMMIT="${BUILD_COMMIT:-$(git -C "${PROJ_ROOT}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"

export_binaries(){
    printf "\n=== exporting binaries ===\n"
    docker build --rm --no-cache \
        --file "${SCRIPT_DIR}/Dockerfile" \
        --build-arg BUILD_VER="${BUILD_VER}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg BUILD_COMMIT="${BUILD_COMMIT}" \
        --target export \
        --output "${OUTPUT_DIR}" \
        --tag "${IMAGE_NAME}:${IMAGE_TAG}" \
        "${PROJ_ROOT}"

    printf "\nBinaries exported to: %s\n" "${OUTPUT_DIR}"
}

e2e_tests(){
    printf "\n=== starting end-to-end tests ===\n"
    docker compose -f "${PROJ_ROOT}/tests/docker-compose.yaml" up --remove-orphans --exit-code-from tests
    docker compose -f "${PROJ_ROOT}/tests/docker-compose.yaml" down --remove-orphans > /dev/null 2>&1 || true
}

main(){
    export_binaries
    e2e_tests
}

main "$@"
