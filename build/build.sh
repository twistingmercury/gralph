#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJ_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

IMAGE_NAME="${IMAGE_NAME:-gralph}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
OUTPUT_DIR="${OUTPUT_DIR:-${PROJ_ROOT}/.bin}"

BUILD_VER="${BUILD_VER:-$(git -C "${PROJ_ROOT}" describe --tags --abbrev=0 2>/dev/null || echo 'dev')}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
BUILD_COMMIT="${BUILD_COMMIT:-$(git -C "${PROJ_ROOT}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')}"

run_quiet() {
	local log
	local rc=0
	log="$(mktemp 2>/dev/null || mktemp -t 'gralph-build')"
	"$@" >"${log}" 2>&1 || rc=$?
	if [ "${rc}" -ne 0 ]; then
		cat "${log}"
	fi

	rm -f "${log}"
	return "${rc}"
}

build() {
	printf "building gralph..."
	run_quiet docker build --rm --no-cache --pull --progress=plain \
		--file "${SCRIPT_DIR}/Dockerfile" \
		--build-arg BUILD_VER="${BUILD_VER}" \
		--build-arg BUILD_DATE="${BUILD_DATE}" \
		--build-arg BUILD_COMMIT="${BUILD_COMMIT}" \
		--target export \
		--output "type=local,dest=${OUTPUT_DIR}" \
		--tag "${IMAGE_NAME}:${IMAGE_TAG}" \
		"${PROJ_ROOT}"

	printf "done\n"
}

e2e_tests() {
	printf "starting end-to-end tests..."
	local rc=0
	run_quiet docker compose --progress=plain -f "${PROJ_ROOT}/tests/docker-compose.yaml" build
	docker compose -f "${PROJ_ROOT}/tests/docker-compose.yaml" up --remove-orphans --exit-code-from tests || rc=$?
	docker compose -f "${PROJ_ROOT}/tests/docker-compose.yaml" down --remove-orphans >/dev/null 2>&1 || true

	printf "done\n"
	return "${rc}"
}

main() {
	build
	e2e_tests
}

main "$@"
