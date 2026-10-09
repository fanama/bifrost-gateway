.PHONY: build build-linux run test docker-up docker-down docker-logs docker-reset clean

GOOS ?= linux
GOARCH ?= $(shell go env GOARCH)

build:
	go build -o bin/bridge-gateway .

build-linux:
	@mkdir -p bin certs
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="-s -w" -o bin/bridge-gateway-linux .
	@cp /etc/ssl/cert.pem certs/ca-certificates.crt

# run ecoute sur BRIDGE_PORT (.env, defaut 4000) ; "--port" force une valeur.
run: build
	./bin/bridge-gateway --config config.yaml

test:
	go test ./tests/ -v -count=1

docker-up: build-linux
	podman compose up --build -d

docker-down:
	podman compose down

docker-logs:
	podman compose logs -f

docker-reset: build-linux
	podman compose down -v
	podman compose up --build -d

clean:
	rm -rf bin/ certs/