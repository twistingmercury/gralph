FROM golang:1.26-alpine AS runner

RUN apk add --no-cache git bash

# Copy the pre-built binary exported by build/Dockerfile into the container.
# build/build.sh must run before docker compose up so the binary exists in .bin/.
ARG GOARCH=amd64
ARG GOOS=linux
COPY .bin/${GOARCH}/${GOOS}/gralph /usr/local/bin/gralph

# Copy the e2e test source only — no main module needed.
WORKDIR /workspace/tests/e2e
COPY tests/e2e/ .

RUN go mod download

ENV GRALPH_BINARY=/usr/local/bin/gralph

CMD ["go", "test", "-v", "./..."]
