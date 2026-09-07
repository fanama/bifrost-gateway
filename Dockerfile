FROM scratch

ARG TARGETARCH

COPY config.yaml /config.yaml
COPY certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY bin/bridge-gateway-linux /bridge-gateway

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt

EXPOSE 8080

ENTRYPOINT ["/bridge-gateway"]
CMD ["--config", "/config.yaml", "--port", "8080"]