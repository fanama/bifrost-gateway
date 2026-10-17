# TODO

Architecture and maintainability backlog for Bridge Gateway.

## Done

- [x] **Layer dependency audit** — `domain` imports no project package;
  `application` and `infrastructure` depend only on `domain`; `handlers` and
  `web` depend on their consumer ports and `domain`; the composition root is
  isolated in `app.go`, `app_server.go` and `wiring.go`.
- [x] **Typed error taxonomy** — `domain/errs` provides validation, not-found,
  invalid-request and conflict errors with stable codes; `domain/llm` provides
  `ProviderError`; HTTP mapping is centralized in `handlers/httpx`.
- [x] **Feature subpackages** — `domain` (`chat`, `config`, `embedding`, `errs`,
  `llm`, `monitoring`, `project`, `provider`, `routing`, `shared`) and
  `application` (`auth`, `catalog`, `chat`, `config`, `embedding`, `entity`,
  `project`, `provider`), plus `handlers` (`chat`, `embeddings`, `models`,
  `httpx`) and `infrastructure` (`config`, `metrics`).
- [x] **Routing rules in the domain** — tier policy, failure classification and
  cooldown keys live in `domain/routing`; `infrastructure` only implements the
  ladder and adapters.
- [x] **Readability caps** — every production file is at most 300 lines, every
  function at most 40 lines, and every function stays within 3 nesting levels.
  `tools/caps` enforces this in `make caps` and CI.
- [x] **`web.NewServer` returns an error** — template parsing no longer calls
  `log.Fatalf`; callers propagate the failure.
- [x] **Composition root split** — `main.go` only parses flags; `app.go` builds
  stores, use cases and handlers; `app_server.go` owns routing and shutdown;
  `wiring.go` keeps provider decorators and seeding helpers.
- [x] **Package-local tests** — `domain/routing`, `domain/errs`,
  `domain/embedding`, `application/chat`, `handlers/httpx`, `handlers` CORS,
  `infrastructure/config` and `infrastructure/metrics` have focused tests in
  addition to the end-to-end suite in `tests/`. The web embedding tester is
  covered with a consumer-port stub.
- [x] **CI** — `.github/workflows/ci.yml` runs gofmt, `go vet`, caps, race tests
  and a full build.
- [x] **Build/deploy cleanup** — English comments in `config.yaml`,
  `compose.yml`, `Dockerfile` and `Makefile`; valid `render.yaml`;
  documented `.env`; compose port and service wiring aligned.
- [x] **Documentation** — English `README.md` and `CONTRIBUTING.md`; the UI
  remains in French.

## Next

- [ ] **Translate remaining legacy inline comments** — new packages and all
  documentation are in English, but older adapters (mostly `infrastructure/`
  and ONNX code) still carry French comments. Translate them incrementally
  without touching behavior or user-visible strings.
- [ ] **More application-layer unit tests** — `application/chat` now has
  package-local tests; add equivalent coverage for the `catalog`, `embedding`
  and `project` use cases.
- [ ] **Broader web controller tests** — extend the stub-based approach used by
  the embedding tester to the chat and project controllers.
- [ ] **Routing observability** — expose the active tier, classifier verdict
  and cooldown state in `/monitoring/metrics.json` for debugging.
- [ ] **ONNX test matrix** — keep artifact-dependent tests skippable, and add a
  small fixture-based test for the tokenizer/decoder paths that do not need the
  full model.
- [ ] **Coverage reporting** — publish a coverage summary in CI once the
  package-local tests above exist.

## Deliberately not planned

- Merging `handlers` and `web`: the OpenAI API and the HTMX UI have different
  contracts and failure modes.
- A dependency-injection framework: explicit wiring in the composition root is
  sufficient at this size.
- Translating user-facing UI or API error strings: keep the existing French UI
  and English API messages.
