.PHONY: build help install uninstall local test e2e build-local docs-check

GIT_COMMIT := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
GIT_TAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOBIN := ${HOME}/go/bin

GOOS :=  $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

LOCAL_BUILD := $(CURDIR)/.bin/local

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n\033[1mAvailable targets:\033[0m\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

local: ## Performs only a local build of gralph
	@go build \
	--ldflags="-X 'github.com/twistingmercury/gralph/internal/version.version=${GIT_TAG}' -X 'github.com/twistingmercury/gralph/internal/version.buildDate=${BUILD_DATE}' -X 'github.com/twistingmercury/gralph/internal/version.gitCommit=${GIT_COMMIT}'" \
	-o ${LOCAL_BUILD}/gralph \
	./cmd/main

build: ## Performs a full build of gralph (Docker-based; use `make local` for a quick local binary)
	./build/build.sh

install: ## Install gralph to $GOBIN
	cp ${LOCAL_BUILD}/gralph ${GOBIN}/gralph

uninstall: ## Uninstall gralph to $GOBIN
	rm ${GOBIN}/gralph

test: ## Runs unit tests only (internal packages). Run `make e2e` for integration tests.
	go test -v ./internal/...

e2e: local ## Runs e2e integration tests locally against the built binary
	cd tests/e2e && GRALPH_BINARY=${LOCAL_BUILD}/gralph go test -v .

docs-check: local ## Verify captured CLI help and local Markdown links
	go run ./cmd/docscheck --root . --help-file docs/cli-help.txt --binary ${LOCAL_BUILD}/gralph

analyze: ## Run linters, formatters, security scanners, etc
	goimports -w .
	golangci-lint run
	govulncheck ./cmd/... ./internal/...
	gosec -quiet -exclude-dir=tests ./...
