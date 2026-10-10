# Bridge Gateway — builds on every platform, from a clean clone.
#
# Two stages:
#   * build   : compiles the gateway binary from the repo sources
#               (CGO_ENABLED=0) for the requested linux/$TARGETARCH;
#   * runtime : a small glibc image. `scratch` is NOT usable here: the local
#               ONNX engine loads the runtime with purego, whose nocgo path
#               (`//go:cgo_import_dynamic ... "libdl.so.2"` / `"libc.so.6"`)
#               makes the binary dynamically linked against glibc.
#
# Because that binary is dynamically linked, its ELF interpreter must exist in
# the runtime image, and it is chosen by the Go linker *on the build host*
# (cmd/link/internal/ld/elf.go): it starts from the target's glibc loader
# (/lib64/ld-linux-x86-64.so.2, /lib/ld-linux-aarch64.so.1) and falls back to
# the musl loader (/lib/ld-musl-<arch>.so.1) when the glibc one is missing —
# which is exactly the case inside Alpine. Building in Alpine therefore emitted
# the musl loader while the runtime here is Debian, and the container died at
# startup with the spectacularly misleading `exec /bridge-gateway: no such file
# or directory` (ENOENT: the kernel could not find the ELF interpreter, not the
# binary). The build stage is glibc-based for this reason, and the runtime
# stage executes the binary once at build time so any such mismatch fails the
# build with a real message instead of producing a container that cannot start.
#
# Nothing gitignored is required: `certs/` (host CA copy) and `models/` (ONNX
# assets, downloaded at first start) are no longer COPYed, so a clean clone —
# including Render's — builds without them.

ARG GO_VERSION=1.26

# ---- Build stage ----------------------------------------------------------
# $BUILDPLATFORM: the toolchain runs natively and cross-compiles to
# $TARGETARCH; the builder libc (glibc, bookworm) matches the runtime one.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Dependencies first so their layer is cached across source edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/bridge-gateway .

# ---- Runtime stage: glibc + CA bundle -------------------------------------
# $TARGETPLATFORM: same platform as the binary compiled just above.
FROM --platform=$TARGETPLATFORM debian:bookworm-slim

# CA bundle for TLS to every LLM provider (also the trust store `ssl` expects).
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Configuration (model_list, providers, cache, routing) — served from disk.
COPY config.yaml /config.yaml

# Local model files (ONNX embeddings + chat) are NOT baked in: `models/onnx/`
# is gitignored, so the app downloads the missing artefacts on first start
# (ONNX_AUTO_DOWNLOAD env var; disable with 0 for an offline image).

# Gateway binary.
COPY --from=build /out/bridge-gateway /bridge-gateway

# Guard: run the entry point once, here. `--help` only parses flags and exits,
# so this is fast and offline, and it fails the build if the binary and the
# image do not agree on the ELF interpreter / architecture.
RUN timeout 30 /bridge-gateway --help > /dev/null 2>&1 \
    || { echo "ERROR: /bridge-gateway cannot run in this image (wrong architecture or missing ELF interpreter)"; exit 1; }

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt

EXPOSE 4000

ENTRYPOINT ["/bridge-gateway"]
CMD ["--config", "/config.yaml", "--port", "4000"]
