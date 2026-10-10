# Bridge Gateway — builds on every platform, from a clean clone.
#
# Two stages:
#   * build   : compiles the static gateway binary from the repo sources
#               (CGO_ENABLED=0) for the requested linux/$TARGETARCH;
#   * runtime : a small glibc image. `scratch` is NOT usable here: the local
#               ONNX engine loads the runtime with purego, whose nocgo path
#               (`//go:cgo_import_dynamic ... "libdl.so.2"` / `"libc.so.6"`)
#               makes the binary dynamically linked against glibc.
#
# Nothing gitignored is required: `certs/` (host CA copy) and `models/` (ONNX
# assets, downloaded at first start) are no longer COPYed, so a clean clone —
# including Render's — builds without them.

ARG GO_VERSION=1.26

# ---- Build stage ----------------------------------------------------------
FROM golang:${GO_VERSION}-alpine AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

# Dependencies first so their layer is cached across source edits.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/bridge-gateway .

# ---- Runtime stage: glibc + CA bundle -------------------------------------
FROM debian:bookworm-slim

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

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt

EXPOSE 4000

ENTRYPOINT ["/bridge-gateway"]
CMD ["--config", "/config.yaml", "--port", "4000"]
