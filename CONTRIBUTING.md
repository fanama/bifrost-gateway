# Contributing to Bridge Gateway

Thanks for contributing. This document covers local setup, the architecture
rules, the readability budget, and the checks a change must pass.

## Requirements

- Go 1.26 or newer.
- Optional: Podman or Docker with Compose.
- Optional: a local Ollama instance and the ONNX artifacts under
  `models/onnx/` for the local engines.

## Setup

```bash
cp .env.example .env       # fill in BIFROST_MASTER_KEY
make build                 # bin/bridge-gateway
make run                   # uses config.yaml and BRIDGE_PORT
go run . --config config.yaml --port 8080
```

The application creates `data/bridge.db` on first start, migrates the schema,
and imports legacy JSON files from `data/` when present. `models/onnx/` is
downloaded on first start unless `ONNX_AUTO_DOWNLOAD=0`.

## Architecture

Dependencies point inward only:

```
handlers/  web/  ──►  application/  ──►  domain/
infrastructure/  ──────────────────────►  domain/
app.go + app_server.go + wiring.go = composition root
```

- `domain/` contains entities, ports, value objects and pure rules. It imports
  no project package.
- `application/` orchestrates use cases and imports only `domain/`.
- `infrastructure/` implements ports with SQLite, Bifrost, ONNX, caches and
  metrics. It imports only `domain/`.
- `handlers/` and `web/` are delivery layers. They depend on the narrow
  consumer ports they declare (`handlers/ports.go`, `web/ports.go`), never on
  concrete infrastructure.
- The composition root is the only place that wires concrete types together.

When adding a feature, start with the domain rule or port, then the use case,
then the adapter or delivery handler, and finally wire it in the composition
root. Do not make `domain` or `application` import a framework, SDK or
database driver.

## Readability budget

Production Go code must stay within:

| Limit | Value |
|---|---:|
| Lines per file | 300 |
| Lines per function | 40 |
| Nested control-flow levels | 3 |

Run the checker with:

```bash
make caps     # go run ./tools/caps .
```

Tests are excluded from the size limits. Keep comments focused on *why* a
decision was made; do not narrate what the code already says.

## Language

- Code, identifiers, comments, commit messages, README and this guide: English.
- User-facing UI strings, API error messages and French UI templates keep their
  current wording. Do not translate user-visible text as part of a refactor.

## Errors

Use the typed errors in `domain/errs`:

- `ValidationError` for malformed input;
- `NotFoundError` for missing entities;
- `InvalidRequestError` for request-level API errors;
- `ProviderError` for upstream/provider failures.

HTTP mapping lives in `handlers/httpx`. Add a stable code rather than matching
on error strings. Preserve existing status codes and response shapes.

## Tests

```bash
make test        # fast integration suite (tests/ only)
make test-all    # go test ./... -count=1
make test-race   # go test ./... -race -count=1
make vet
make ci          # fmt + vet + race + build
```

Guidelines:

- Add package-local tests next to the code they exercise.
- Use the delivery ports to test handlers and web controllers with small
  stubs; a full SQLite stack is only needed for integration tests.
- Do not skip tests, weaken assertions or add lint/type suppressions to make a
  check pass. Fix the cause.
- A behavior-preserving refactor must keep the existing tests green without
  editing their expectations.

## Commit and pull request checklist

- [ ] `make ci` passes.
- [ ] `make caps` passes.
- [ ] New behavior has tests; refactors keep the existing tests unchanged.
- [ ] Layer import rules are respected.
- [ ] Documentation (`README.md`, `CONTRIBUTING.md`, `config.yaml` comments)
      is updated when the setup or architecture changes.
- [ ] No secrets, `.env`, `data/`, `models/`, `bin/` or `certs/` are committed.

## Build and deployment files

- `Dockerfile` compiles from source in a glibc builder and runs on a Debian
  slim image (the ONNX purego runtime needs glibc).
- `compose.yml` is self-contained: one gateway, SQLite, in-memory cache.
- `render.yaml` must remain valid YAML; do not put prose before `services:`.
- `Makefile` is the source of truth for local commands; keep it in sync with
  CI and the README.

Thank you for helping keep the gateway small, explicit and easy to reason
about.
