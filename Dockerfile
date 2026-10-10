# Bridge Gateway - Render.com (Scratch image, CGO_ENABLED=0)
#
# Builds a zero-dependency Linux binary from a local cross-compile and uses it
# as the `scratch` image. No glibc, no shell, no CA bundle dynamics: a CA
# certificate is copied into the image so TLS works without host deps.
#
# The.binaries/bundles used here are produced by:
#   make build-linux        # linux host (amd64)
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/bridge-gateway-linux .
#   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/bridge-gateway-linux-arm64 .

ARG TARGETARCH=amd64

FROM scratch

# Runtime CA bundle (used by GoTLS and by the SQLite/WAL layer when needed).
COPY certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Configuration (model_list, providers, cache, routing) — served from disk.
COPY config.yaml /config.yaml

# Local model files (ONNX embeddings + chat) — ships with the image or is
# downloaded at first start by the app (ONNX_AUTO_DOWNLOAD env var).
# These paths stay relative to the working directory; the app resolves them
# from the binary location.
COPY models models

# Gateway binary (linux/$TARGETARCH).
COPY bin/bridge-gateway-linux /bridge-gateway

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt

EXPOSE 4000

ENTRYPOINT ["/bridge-gateway"]
CMD ["--config", "/config.yaml", "--port", "4000"]
