.PHONY: build help install uninstall local build

GIT_COMMIT := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
GIT_TAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GOBIN := ${HOME}/go/bin

GOOS :=  $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

LOCAL_BUILD := "./.bin/${GOOS}/${GOARCH}"

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n\033[1mAvailable targets:\033[0m\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

local: ## Performs only a local build of gralph
	@go build \
	--ldflags="-X 'github.com/twistingmercury/gralph/internal/version.version=${GIT_TAG}' -X 'github.com/twistingmercury/gralph/internal/version.buildDate=${BUILD_DATE}' -X 'github.com/twistingmercury/gralph/internal/version.gitCommit=${GIT_COMMIT}'" \
	-o ${LOCAL_BUILD}/gralph \
	./cmd/main/main.go

build: ## Performs a full build of gralph
	./build/build.sh
	docker system prune -f

install: ## Install gralph to $GOBIN
	cp ${LOCAL_BUILD}/gralph ${GOBIN}/gralph

uninstall: ## Uninstall gralph to $GOBIN
	rm ${GOBIN}/gralph