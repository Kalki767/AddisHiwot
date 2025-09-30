# API image, built in two stages.
# 1. build: compile a static Go binary with the full Go toolchain.
# 2. run:   copy only that binary into distroless (no shell, no package manager,
#           runs as a non-root user), so the image has almost nothing to attack.

FROM golang:1.27.3-trixie@sha256:fbb7ae40b62ff8afe31662df570d39b4f5052ac32b90dae8e60d7657a20a74f4 AS build
WORKDIR /src

# Dependencies first: this layer is reused until go.mod or go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/api /api
USER nonroot:nonroot
EXPOSE 8080
# No curl in this image: the binary checks its own /readyz.
HEALTHCHECK --interval=5s --timeout=4s --start-period=5s --retries=12 CMD ["/api", "-healthcheck"]
ENTRYPOINT ["/api"]
