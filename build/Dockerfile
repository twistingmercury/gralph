FROM ghcr.io/twistingmercury/golang-tooling:alpine AS builder

RUN apk add --no-cache git bash

ARG BUILD_VER
ARG BUILD_DATE
ARG BUILD_COMMIT

ARG LD_FLAGS="-s -w -X 'github/twistingmercury/gralph/internal/version.version=${BUILD_VER}' -X 'github/twistingmercury/gralph/internal/version.buildDate=${BUILD_DATE}' -X 'github/twistingmercury/gralph/internal/version.gitCommit=${BUILD_COMMIT}'"

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod tidy

COPY . .

## Run linters, formatters, security scanners, etc
RUN goimports -w .
RUN golangci-lint run
RUN govulncheck ./...
RUN gosec ./...

RUN go test -v ./internal/looper/...

RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=darwin go build --ldflags "${LD_FLAGS}" -o /.bin/amd64/darwin/gralph ./cmd/main/main.go
RUN CGO_ENABLED=0 GOARCH=arm64 GOOS=darwin go build --ldflags "${LD_FLAGS}" -o /.bin/amd64/darwin/gralph ./cmd/main/main.go
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build --ldflags "${LD_FLAGS}" -o /.bin/amd64/linux/gralph ./cmd/main/main.go


#---------------------------------------------------------------------
FROM scratch AS export

COPY --from=builder /.bin/ .
