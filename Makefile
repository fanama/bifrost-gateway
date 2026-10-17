.PHONY: build build-linux run fmt vet test test-all test-race caps ci \
	docker-up docker-down docker-logs docker-reset clean

GOOS ?= linux
GOARCH ?= $(shell go env GOARCH)
COMPOSE ?= podman compose

build:
	go build -o bin/bridge-gateway .

build-linux:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="-s -w" -o bin/bridge-gateway-linux .

# run listens on BRIDGE_PORT (.env, default 4000); "--port" forces a value.
run: build
	./bin/bridge-gateway --config config.yaml

fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./tests/ -v -count=1

test-all:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

# Enforce the readability caps documented in CONTRIBUTING.md.
caps:
	@go run ./tools/caps .

ci: fmt vet test-race build

# The image compiles the binary from source (see Dockerfile); build-linux is a
# local Linux binary and is not a prerequisite of these targets.
docker-up:
	$(COMPOSE) up --build -d

docker-down:
	$(COMPOSE) down

docker-logs:
	$(COMPOSE) logs -f

docker-reset:
	$(COMPOSE) down -v
	$(COMPOSE) up --build -d

clean:
	rm -rf bin/ certs/
