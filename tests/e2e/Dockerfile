FROM golang:1.25-alpine AS builder

ENV GOTOOLCHAIN=auto

RUN apk add --no-cache curl bash

WORKDIR /e2e

COPY tests/e2e/go.mod tests/e2e/go.sum ./
RUN go mod download

COPY .bin/amd64/linux/* /go/bin/
COPY tests/e2e/cli_test.go ./
COPY tests/e2e/test-runner.sh /test-runner.sh

RUN chmod +x /test-runner.sh

ENTRYPOINT ["/test-runner.sh"]

