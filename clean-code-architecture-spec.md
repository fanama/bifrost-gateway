# Clean Code & Architecture — Specification

| Field | Value |
|---|---|
| Status | Draft — ready for implementation planning |
| Date | 2026-10-17 |
| Repository | `bridge-gateway` (Bridge Gateway / BifrostAI) |
| Request | "I want clean code and architecture." |
| Scope | Entire Go codebase + build/deploy configuration |
| Behavior contract | Strictly behavior-preserving |
| Delivery | One large PR, split into internal implementation phases |
| Audience | Open-source maintainers/community |

> **Interpretation of "apply all of these".** This file applies every answer
> from the architecture interview to the requested specification. Per the
> original instruction ("do not make any code changes yet; stop after creating
> the spec file"), no Go, template, config or build file has been modified.
> This document is the implementation blueprint.

---

## 1. Purpose

Bridge Gateway is a working multi-provider LLM gateway with a Clean
Architecture-style layout, but it has accumulated structural debt: a 7.1k-line
infrastructure package, business routing rules living in adapters, no stable
error taxonomy, a few oversized functions/files, no CI, and French/English
documentation mixed for an increasingly open-source audience.

This spec defines a **behavior-preserving** refactor that:

1. makes layer boundaries explicit and enforceable;
2. reorganizes each layer into feature subpackages;
3. moves routing/tier policy into the domain;
4. standardizes typed errors with stable codes and centralized HTTP mapping;
5. enforces hard readability caps (300-line files, 40-line functions, 3 nesting
   levels);
6. improves testability and adds the minimal test guardrail requested
   (`go test -race ./...` in CI);
7. cleans build/deploy configuration and documents contribution conventions for
   open-source maintainers.

No feature, API, UI, configuration or deployment behavior may change.

---

## 2. Current-state audit (baseline)

Measured on the current working tree before any refactor.

### 2.1 Size and package layout

| Area | Production LOC | Files | Notes |
|---|---:|---:|---|
| `domain/` | 1,572 | 22 | Pure entities, ports, enrichment, schema |
| `application/` | 693 | 10 | Use cases; no package-local tests |
| `infrastructure/` | 7,122 | 64 | SQLite, Bifrost, routing, ONNX, embeddings, cache, metrics, config |
| `handlers/` | 950 | 11 | OpenAI-compatible API |
| `web/` | 1,586 | 20 | HTMX UI controllers + templates |
| `docs/` | 105 | — | Embedded OpenAPI spec |
| `main.go` + `wiring.go` | 504 | 2 | Flags, composition, server, seeds |

Total Go source is ~20.4k lines including tests.

### 2.2 Verified dependency graph

The current layering is already correct:

- `domain/` imports **no** project package.
- `application/` imports only `domain/`.
- `infrastructure/` imports only `domain/` (never `application/`).
- `handlers/` and `web/` production code do not import `infrastructure/`.
- `main.go` / `wiring.go` are the only composition root.

This spec preserves that direction and makes it explicit per feature
subpackage.

### 2.3 Known debt and TODO backlog

From `TODO.md`:

- `web.NewServer` calls `log.Fatalf` when templates fail to parse; it should
  return `(*Server, error)` (call sites: `main.go` + 6 `web` tests).
- Routing/tier selection and failure classification live in
  `infrastructure/routing.go`, `routing_ladder.go`, `routing_stream.go`,
  `failover.go`, `cooldown.go`.
- Delivery consumer ports exist but are not used for light unit tests.
- `application/` has no package-local tests; it is only exercised through
  `tests/application_test.go`.

### 2.4 Measured cap violations

Strict caps: **300 lines/file, 40 lines/function, 3 nesting levels**.

Files over 300 lines (production only):

| Lines | File |
|---:|---|
| 533 | `infrastructure/metrics.go` |

Functions over 40 lines (rough brace measurement, top offenders):

| Lines | Location | Function |
|---:|---|---|
| 230 | `main.go:24` | `main` |
| 128 | `infrastructure/onnxchat_kv.go:28` | `(*OnnxChatProvider).step` |
| 111 | `web/embeddings.go:52` | `computeEmbeddings` |
| 87 | `infrastructure/metrics.go:162` | `finishRequest` |
| 84 | `infrastructure/onnxembedder_infer.go:15` | `runLocked` |
| 78 | `infrastructure/onnxembedder_session.go:47` | `stateLocked` |
| 72 | `domain/schema.go:56` | `BuildJSONSchema` |
| 69 | `infrastructure/onnxchat_decode.go:17` | `generate` |
| 68 | `infrastructure/onnxembedder.go:70` | `Embed` |
| 68 | `infrastructure/onnxchat_pretokenizer.go:122` | `scanGPT2Pattern` |
| 68 | `infrastructure/cachedllm.go:139` | `ChatStream` |
| 67 | `infrastructure/jsonimport.go:95` | `ImportLegacyStoreData` |
| 66 | `handlers/embeddings.go:25` | `HandleEmbedding` |
| 64 | `infrastructure/onnxchat_decode.go:91` | `sampleToken` |
| 63 | `infrastructure/routing_ladder.go:19` | `ladder` |
| 63 | `infrastructure/routing.go:71` | `Chat` |
| 62 | `infrastructure/routing_stream.go:22` | `ChatStream` |
| 61 | `handlers/stream.go:85` | `writeSSE` |
| 60 | `web/forms.go:39` | `parseSchemaRows` |
| 60 | `infrastructure/bifrost_provider.go:122` | `ChatStream` |
| 56 | `infrastructure/provider_mapping.go:62` | `ToModelProvider` |
| 53 | `web/forms.go:167` | `schemaRowsFromStored` |
| 52 | `infrastructure/onnxembedder_infer.go:123` | `poolOutput` |
| 52 | `infrastructure/onnxchat_generate.go:77` | `ChatStream` |
| 52 | `application/chat.go:87` | `prepare` |

Nesting depth is not yet measured; it must be checked per function during the
readability pass.

### 2.5 Tests and tooling

- 30 `_test.go` files: domain unit tests, infrastructure adapter tests, web
  controller tests, and six integration/contract test files under `tests/`.
- `tests/openai_contract_test.go` (817 lines) is the main behavioral safety
  net for the OpenAI-compatible API.
- No `.github/`, no `golangci-lint`, `staticcheck`, `.editorconfig` or
  pre-commit configuration.
- `make test` runs **only** `go test ./tests/`, not `./...`.
- `go build ./...`, `go vet ./...` and `go test ./...` currently pass.

### 2.6 Error handling today

Mixed conventions:

- sentinel errors in `domain/errors.go` (`ErrConfigNotFound`, …);
- typed errors (`ValidationError`, `InvalidRequestError`,
  `MissingAttributionError`);
- HTTP-specific translation in `handlers/errors.go`
  (`writeError`, `writeAPIError`, `writeChatError`);
- UI toasts render raw error strings.

There is no stable machine-readable error code shared by API, UI, logs and
tests.

### 2.7 Build/deploy baseline

- `Dockerfile`: multi-stage, glibc builder + Debian runtime; ONNX/purego
  constraints documented at length; build-time `--help` guard.
- `compose.yml`: single `gateway` service, SQLite volume, `.env`, Ollama host
  bridge; comments in French.
- `render.yaml`: Docker web service; contains a top-level `networks` block that
  is not part of Render's service schema and should be audited.
- `config.yaml`: French comments; routing policy duplicated in prose and code.
- `.env.example`: missing `ONNX_AUTO_DOWNLOAD` and `BRIDGE_DB` is commented;
  consistency with README/Docker/render must be reviewed.
- `README.md`: French, detailed, and currently the primary architecture
  reference (open-source documentation should become English).

### 2.8 Duplication candidates (audit, not blind abstraction)

- Three cache decorators (`cachedstore.go`, `cachedapikeystore.go`,
  `cachedprojectstore.go`) with near-identical invalidation logic.
- Model-list merging across gateway config, catalog and chat configurations
  (`domain/models.go`, `application/model.go`, `web`).
- Form parsing/schema-row rehydration (`web/forms.go`, `web/server.go`,
  `domain/schema_form.go`).
- Response building helpers in `handlers/response.go`.
- Repeated wiring in `main.go` / `wiring.go`.

Abstraction is only introduced where behavior is provably identical and the
result is smaller than the duplication.

---

## 3. Goals and non-goals

### 3.1 Goals

1. **Clear layer boundaries** with feature subpackages in every layer.
2. **Domain purity**: routing/tier/failover/cooldown *rules* in `domain/`;
   only I/O and provider calls remain in adapters.
3. **Typed errors with stable codes**, mapped centrally to HTTP and UI.
4. **Readability caps** enforced on production code.
5. **Testability**: consumer-side ports and light stubs; package-local tests.
6. **Open-source onboarding**: English code/comments/docs, `CONTRIBUTING.md`,
   accurate README tree, French UI retained.
7. **Minimal guardrails**: gofmt/vet plus `go test -race ./...` in CI.
8. **Build/deploy hygiene**: Docker/compose/render/config/.env audited and
   documented, with behavior unchanged.

### 3.2 Non-goals

- No API, UI, config-key, CLI-flag, database-schema or response-shape changes.
- No performance optimization or benchmark gate (explicit decision).
- No Postgres/Redis adapters: only swappable ports and documented seams.
- No new features (observability expansion, auth/RBAC, cost accounting,
  multi-instance runtime are future work).
- No DI framework, framework migration or rewrite of ONNX/Bifrost internals.
- No `docs/architecture.md`, ADR suite or `doc.go` mandate (not selected);
  documentation deliverables are `CONTRIBUTING.md` + README.
- No `make test` semantic change unless the open decision in §12 is resolved.
- No dependency upgrades and no new runtime dependencies.

---

## 4. Interview decisions (authoritative)

| Area | Decision |
|---|---|
| Primary outcome | Architecture audit + prioritized refactor plan; testability/test strategy; documentation/onboarding |
| Scope | Entire Go codebase + build/deploy configuration |
| Trigger | Upcoming feature work, general craftsmanship, external review/audit |
| Behavior | Strictly behavior-preserving |
| Priorities | Readability/function-file size; error handling; duplication/abstraction; testability/coverage; layer boundaries; documentation/onboarding (performance and package cohesion were not prioritized) |
| Upcoming work to prepare for | Observability expansion; embeddings/ONNX; more providers/routing; multi-tenant/auth/permissions; deployment/multi-instance readiness |
| External audience | Open-source maintainers/community |
| Guardrails | Minimal: gofmt/vet + lightweight CI test workflow |
| Package layout | Layered roots with feature subfolders (not vertical slices) |
| Cross-feature imports | Direct cross-feature imports within a layer are allowed |
| Domain purity | Move routing/tier policy into `domain/` |
| Error convention | Typed errors with stable codes |
| Caps | Strict: 300 lines/file, 40 lines/function, 3 nesting levels |
| Language | English code/comments/docs; French UI retained |
| TODO.md backlog | All four open items |
| Test guardrail | `go test -race ./...` in CI |
| Multi-instance readiness | Design seams only |
| Performance evidence | Behavior and tests only |
| Build/deploy | Lightweight CI; clean Dockerfile/compose/render; review config/.env consistency |
| Documentation | `CONTRIBUTING.md` + README updated to new tree |
| Delivery | One large PR |
| New-test scope | **No answer recorded** (session ended) — see §12 |
| Makefile test target | **No answer recorded** — see §12 |
| Behavior-proof method | **No answer recorded** — see §12 |

---

## 5. Target architecture

### 5.1 Principles

1. **Layered roots, feature subfolders.** Keep `domain/`, `application/`,
   `infrastructure/`, `handlers/`, `web/`; organize their contents by feature.
2. **Dependencies point inward.** Outer layers may import inner layers, never
   the reverse.
3. **Cross-feature imports within a layer are allowed** (explicit decision), but
   must not create cycles. Shared primitives go to a `shared` subpackage.
4. **Ports are consumer-side.** `handlers/` and `web/` declare only the methods
   they use; `infrastructure/` implements domain ports.
5. **Composition root stays explicit.** Manual wiring in `main.go` /
   `wiring.go`; no DI framework.
6. **Small units.** One primary responsibility per package/file; caps enforced.

### 5.2 Target tree

```
bridge-gateway/
├── main.go                     # flags, signals, calls run()
├── wiring.go                   # composition helpers; split if >300 lines
├── domain/
│   ├── chat/                   # chat.go, services.go (enrichment)
│   ├── llm/                    # llm.go (ports), ProviderError (moved from infrastructure/bifrost.go)
│   ├── config/                 # config.go, config_tier.go, config_response_format.go, schema*.go
│   ├── project/                # project.go, apikey.go
│   ├── provider/               # provider.go, model.go, modelcatalog.go, models.go
│   ├── embedding/              # embedding.go
│   ├── monitoring/             # monitoring.go
│   ├── routing/                # NEW: Tier policy, failure reasons, cooldown rules
│   ├── errs/                   # NEW: typed errors + stable codes
│   └── shared/                 # store.go, cache.go, id.go
├── application/
│   ├── chat/
│   ├── config/
│   ├── project/                # project.go, apikey.go
│   ├── auth/
│   ├── provider/
│   ├── catalog/                # model.go, modelcatalog.go
│   ├── embedding/
│   └── entity/                 # generic CRUD use case
├── infrastructure/
│   ├── sqlite/                 # sqlstore, sqlschema, *_store, jsonimport
│   ├── cache/                  # memorycache, cached store/key/project decorators
│   ├── llm/                    # bifrost*, routing*, failover*, cooldown I/O, classifier, cachedllm
│   ├── onnx/                   # assets/download, chat*, embedder*, tokenizer, wordpiece
│   ├── embedding/              # localembedder, local_hasher, embeddingrouter, format
│   ├── metrics/                # collector, middleware, snapshot, llm decorator
│   └── config/                 # config.yaml loader, port resolution
├── handlers/
│   ├── routes.go               # aggregator (shared with behavior tests)
│   ├── httpx/                  # errors.go (central mapping), cors.go, Authenticator port
│   ├── chat/                   # chat.go, stream.go, sse.go, response.go
│   ├── embeddings/
│   ├── models/
│   └── ... (tests move with their package)
├── web/
│   ├── server.go               # facade + routing
│   ├── ui/                     # renderer/shared helpers, form parsing, ports
│   ├── chat/
│   ├── projects/
│   ├── models/
│   ├── providers/
│   ├── embeddings/
│   ├── monitoring/
│   ├── templates/              # embedded by web root (paths unchanged)
│   └── static/
├── docs/
├── tests/
└── .github/workflows/ci.yml    # NEW
```

**Template/static handling.** `web/templates/` and `web/static/` stay under the
`web` root so the existing `go:embed` directives and asset URLs remain
unchanged. The root package parses them and passes a renderer to `web/ui`;
subcontrollers depend on `web/ui`, never on the root package (no cycle).

**Delivery subpackages are intentional.** `handlers/` and `web/` are small
today, but the layout must match the chosen architecture and remain stable for
upcoming observability/auth features. The split is mechanical and
behavior-preserving.

### 5.3 File mapping (current → target)

#### Domain

| Current | Target |
|---|---|
| `chat.go`, `services.go` | `domain/chat/` |
| `llm.go` (ports), `ProviderError` from `infrastructure/bifrost.go` | `domain/llm/` |
| `config.go`, `config_tier.go`, `config_response_format.go`, `schema.go`, `schema_property.go`, `schema_form.go` | `domain/config/` |
| `project.go`, `apikey.go` | `domain/project/` |
| `provider.go`, `model.go`, `modelcatalog.go`, `models.go` | `domain/provider/` |
| `embedding.go` | `domain/embedding/` |
| `monitoring.go` | `domain/monitoring/` |
| routing rules from `infrastructure/routing*.go`, `failover.go`, `cooldown.go` | `domain/routing/` (new, pure) |
| `errors.go` | `domain/errs/` (rewritten taxonomy; sentinels preserved) |
| `store.go`, `cache.go`, `id.go` | `domain/shared/` |

#### Application

| Current | Target |
|---|---|
| `chat.go` | `application/chat/` |
| `config.go` | `application/config/` |
| `project.go`, `apikey.go` | `application/project/` |
| `auth.go` | `application/auth/` |
| `provider.go` | `application/provider/` |
| `model.go`, `modelcatalog.go` | `application/catalog/` |
| `embedding.go` | `application/embedding/` |
| `entity.go` | `application/entity/` |

#### Infrastructure

| Current | Target |
|---|---|
| `sqlstore.go`, `sqlschema.go`, `*_store.go`, `jsonimport.go` | `infrastructure/sqlite/` |
| `memorycache.go`, `cachedstore.go`, `cachedapikeystore.go`, `cachedprojectstore.go` | `infrastructure/cache/` |
| `bifrost*.go`, `adapter.go`, `account.go`, `dynamic_account.go`, `chatrouter.go`, `classifier.go`, `provider_mapping.go`, `routing*.go`, `failover.go`, `cooldown.go`, `cachedllm.go` | `infrastructure/llm/` |
| `onnx*.go`, `wordpiece.go` | `infrastructure/onnx/` |
| `localembedder.go`, `local_hasher.go`, `embeddingrouter.go`, `embedding_format.go` | `infrastructure/embedding/` |
| `metrics.go` (split by concern), `metricsllm.go` | `infrastructure/metrics/` |
| `config.go`, `port.go` | `infrastructure/config/` |

#### Handlers / web

| Current | Target |
|---|---|
| `handlers/errors.go`, `handlers/cors.go`, shared auth port | `handlers/httpx/` |
| `handlers/chat.go`, `stream.go`, `sse.go`, `response.go` | `handlers/chat/` |
| `handlers/embeddings.go` | `handlers/embeddings/` |
| `handlers/models.go` | `handlers/models/` |
| `handlers/routes.go` | stays `handlers/routes.go` (aggregator) |
| `web/server.go`, templates/static | stays at `web/` root |
| `web/ports.go`, `uiShared`, shared render helpers, `forms.go` | `web/ui/` |
| `web/chat.go` | `web/chat/` |
| `web/projects.go`, `project_configs.go`, `project_keys.go` | `web/projects/` |
| `web/models.go` | `web/models/` |
| `web/providers.go` | `web/providers/` |
| `web/embeddings.go`, `embedding_choices.go`, `embedding_view.go` | `web/embeddings/` |
| `web/monitoring.go` | `web/monitoring/` |

### 5.4 Dependency rules

| Package | May import |
|---|---|
| `domain/*` | stdlib, `domain/*` (direct cross-feature allowed) |
| `application/*` | stdlib, `domain/*` |
| `infrastructure/*` | stdlib, external modules, `domain/*` |
| `handlers/*` | stdlib, `application/*`, `domain/*`, `handlers/*` |
| `web/*` | stdlib, `application/*`, `domain/*`, `web/*` |
| `main.go`, `wiring.go` | everything (composition root) |

Forbidden:

- `domain` → `application`, `infrastructure`, `handlers`, `web`
- `application` → `infrastructure`, `handlers`, `web`
- `infrastructure` → `application`, `handlers`, `web`
- `handlers` / `web` → `infrastructure`
- Any import cycle, including cycles introduced by cross-feature imports.

`main.go` / `wiring.go` remain the only place that instantiates SQLite,
Bifrost, ONNX, caches and HTTP handlers.

### 5.5 Composition root and `NewServer`

- `main()` must shrink below 40 lines: parse flags → `run()`.
- Extract `run`, `buildApp`, `buildMux`, `seed`, `serve`/`shutdown` into
  `main.go`/`wiring.go`, each ≤40 lines.
- `web.NewServer` returns `(*Server, error)`; `main` handles the error with
  `log.Fatalf` at the composition root only.
- Update call sites: `main.go` + `web/chat_config_test.go`,
  `web/project_config_test.go`, `web/embedding_test.go`,
  `web/model_kind_test.go`, `web/embedding_local_test.go`,
  `web/monitoring_test.go`.
- No `log.Fatalf`, `os.Exit` or panic outside `main`/`wiring` except the
  documented `crypto/rand` failure in `domain/project/apikey.go`.

### 5.6 Domain routing

Move **rules**, not I/O. There is one source of truth per concept:

- **Stays in `domain/config`**: tier names, ranks, `ChatConfig.TierRank()` and
  `TierOptions()` (`config_tier.go` is already framework-free).
- **Moves to `domain/routing`**:
  - `ProviderError` moves to `domain/llm` (pure status/code/message/detail),
    because it is the provider-facing error type; infrastructure constructs it.
  - failure classification (`rate_limit`, `model_unavailable`,
    `context_length`, `unavailable`, `auth`, `client`, `none`) from
    `infrastructure/failover.go`, operating on `*llm.ProviderError` and
    `context.Canceled` via `errors.Is`/`errors.As`;
  - ladder construction/ordering and tier start selection as a pure function
    over the active config, the candidate configs, the classifier rank and a
    cooldown predicate;
  - cooldown keying/eligibility rule (not the clock or storage).
- **Stays in `infrastructure/llm`**:
  - provider calls, Bifrost retries, classifier invocation;
  - listing project configs and reading the clock;
  - cooldown storage/TTL (`cooldownStore`) and streaming substitution
    mechanics;
  - mapping Bifrost errors into `domain/llm.ProviderError`.
- The infrastructure adapter keeps orchestration: list candidates → classify
  → call `routing.BuildLadder` → try targets → record cooldown.
- Add table-driven tests for every failure classification and ladder decision,
  moved together with the code.
- Behavior must be bit-for-bit equivalent: same tier order, same retry limits,
  same cooldown skipping, same "don't prefer silence" fallback.

### 5.7 Typed errors with stable codes

Create `domain/errs`:

```go
package errs

type Code string

const (
    CodeValidation          Code = "validation_error"
    CodeInvalidRequest      Code = "invalid_request"
    CodeNotFound            Code = "not_found"
    CodeConflict            Code = "conflict"
    CodeUnauthorized        Code = "unauthorized"
    CodeForbidden           Code = "forbidden"
    CodeRateLimited         Code = "rate_limited"
    CodeProviderUnavailable Code = "provider_unavailable"
    CodeTimeout             Code = "timeout"
    CodeCanceled            Code = "canceled"
    CodeInternal            Code = "internal_error"
)

type Error struct {
    Code    Code
    Message string   // user-facing text; preserved verbatim in this pass
    Param   string   // request parameter / form field, optional
    Cause   error    // wrapped original error, optional
}
```

Requirements:

- Implement `Error()`, `Unwrap()`, and code lookup (`CodeOf(err)` via
  `errors.As`).
- Existing sentinels (`ErrConfigNotFound`, `ErrNoActiveConfig`, …) remain
  discoverable through `errors.Is`; typed errors wrap them as `Cause` or the
  sentinel itself is promoted to a typed error with the same identity.
- Adapter errors are wrapped at the adapter boundary, never re-mapped in each
  handler.
- **Central mapping**:
  - `handlers/httpx` maps `Code` → HTTP status and OpenAI error envelope
    (`validation/invalid_request` → 400, `unauthorized` → 401, `forbidden` →
    403, `not_found` → 404, `conflict` → 409, `rate_limited` → 429,
    `canceled` → 499/no body, `timeout` → 504,
    `provider_unavailable` → 502/503, `internal` → 500).
  - `web/ui` maps `Code` → toast/UI behavior. User-visible French strings are
    preserved in this refactor; English applies to code, comments and
    developer docs, not to the existing UI wording.
- No new `fmt.Errorf("...")` without `%w` for a cause.
- No error swallowed to silence a check.
- `ValidationError` / `InvalidRequestError` are either migrated to `errs.Error`
  or kept as compatibility wrappers with the same messages.

### 5.8 Readability and conventions

Strict caps on **production** `.go` files:

| Rule | Limit |
|---|---|
| File length | 300 lines |
| Function length | 40 lines |
| Nesting depth | 3 levels |

Interpretation:

- Caps apply to non-test production Go files. Test files are exempt from the
  300-line file cap; test functions should still stay reviewable.
- A cap may be exceeded only with a written justification in the PR and a
  nearby comment; silent exceptions are not allowed. Exceptions are expected
  for generated/unavoidable low-level code (e.g. ONNX tensor plumbing) and must
  be listed in the PR.
- Split `infrastructure/metrics.go` into `collector.go`, `middleware.go`,
  `snapshot.go`; each ≤300 lines and functions ≤40.
- Split `main()` into `run` + build/seed/serve helpers.
- Prefer early returns and guard clauses to reduce nesting.
- One primary reason to change per file; avoid `helpers.go`/`utils.go`
  dumping grounds.
- Exported identifiers documented in English; comments explain *why*, not
  *what*; stale French comments are translated.
- Formatting: `gofmt`; imports grouped stdlib / third-party / project.
- Naming: English identifiers, no abbreviations that obscure meaning;
  package names short and responsibility-oriented.
- Interfaces: consumer-side ports only where substitution/testing justifies
  them; "accept interfaces, return structs" except for declared ports. Avoid
  interface explosion during the split.
- Duplication is removed only when the shared version is simpler; the audit
  must state behavior parity for each consolidation.

### 5.9 Language policy

- Code, identifiers, comments, commit messages, README and `CONTRIBUTING.md`:
  **English**.
- UI labels, templates, toasts, validation messages shown to users: **French**,
  unchanged in this pass.
- API error messages: preserve existing English text unless a typed code adds
  structure without changing the message.
- `config.yaml` and compose/Docker comments: English; keys and values
  unchanged.

### 5.10 Test strategy and testability

Guardrail (confirmed): CI runs `go test -race ./...`.

Open items (no answer recorded) are in §12. Recommended defaults:

1. Add tests where a refactor touches untested behavior; do not pursue a
   blanket coverage number.
2. Keep `tests/openai_contract_test.go` assertions unchanged; only import paths
   and helper locations may change.
3. Add package-local tests for every new subpackage (domain rules, application
   use cases, adapters).
4. Use light stubs at the consumer ports for `handlers`/`web`, avoiding the
   full SQLite/Bifrost chain.
5. No coverage gate (not selected).

Testability requirements:

- Constructors accept ports, not concrete stores/providers.
- No package-level mutable state in new code; clocks/randomness injectable
  where behavior depends on them (cooldown, metrics, sampling).
- Fakes live next to the package under test (`_test.go`), not in a global test
  utility package unless shared by integration tests.
- Integration tests continue to exercise the real SQLite schema and HTTP
  contract.

### 5.11 Build, deploy and CI

- Add `.github/workflows/ci.yml`:
  - `gofmt -l .` must be empty;
  - `go vet ./...`;
  - `go test -race ./...`.
- No golangci-lint/staticcheck (minimal guardrails decision).
- Audit and clean:
  - `Dockerfile`: comments in English, no stale build assumptions, behavior
    and glibc/ONNX constraints preserved.
  - `compose.yml`: English comments; `.env` handling documented; service
    names/ports unchanged.
  - `render.yaml`: remove/repair the top-level `networks` block if invalid;
    keep env vars aligned with the Dockerfile/README.
  - `config.yaml`: English comments only; values and keys unchanged.
  - `.env.example`: add/align `BRIDGE_PORT`, `BRIDGE_DB`,
    `BIFROST_MASTER_KEY`, `OLLAMA_HOST`, `ONNX_AUTO_DOWNLOAD`; no secrets.
- `make test` semantics: open decision (§12). Recommended: keep `make test`
  as the fast `tests/` integration run and add `test-all` (`go test ./...`)
  and `test-race` (`go test -race ./...`).
- Multi-instance: document the swappable seams (store/cache ports, config,
  stateless HTTP) without implementing Postgres/Redis.

### 5.12 Documentation

Deliverables (confirmed):

- **`CONTRIBUTING.md`** (English): setup, commands, architecture summary,
  dependency rules, caps, naming/language policy, test policy, PR conventions,
  how to add a provider/feature without breaking layering.
- **`README.md`**: translated to English; installation/deploy instructions
  preserved; project tree updated to the target layout; UI remains described
  as French.
- Update `TODO.md`: close completed architecture items, add follow-ups.
- No `docs/architecture.md`, ADR suite or `doc.go` mandate in this pass.
- `docs/openapi.yaml` (if present) stays behaviorally identical; any comment
  translation must not change the spec semantics.

---

## 6. Prioritized refactor plan

One large PR, implemented in ordered internal phases. Each phase ends with
`gofmt`, `go vet ./...`, `go test -race ./...`, and any phase-specific proof.

### Phase 0 — Baseline (no production change)

1. Create a working branch and record the baseline:
   - `go build ./...`, `go vet ./...`, `go test -race ./...`;
   - `make test`;
   - build and run the Docker image;
   - capture the smoke matrix results (§7.1).
2. Record the import graph and cap violations (already measured in §2).
3. Ensure `git status` is clean before restructuring.

**Exit criteria:** baseline artifacts saved in the PR description.

### Phase 1 — Typed errors (P0)

1. Add `domain/errs` with codes, `Error`, `CodeOf`, constructors.
2. Migrate `domain/errors.go` sentinels/typed errors with identity preserved.
3. Wrap adapter errors at boundaries (`infrastructure/*`, SQLite, Bifrost,
   ONNX, embeddings).
4. Add central mapping in `handlers/httpx`; replace per-handler status
   decisions.
5. Map UI toasts in `web/ui` while keeping French text identical.
6. Add table tests for code→status mapping.

**Exit criteria:** API responses byte-identical for representative success and
error cases; contract tests pass; no bare status literals in handlers.

### Phase 2 — Domain routing (P0)

1. Move `ProviderError` to `domain/llm` (pure data).
2. Extract failure classification from `infrastructure/failover.go` and ladder
   construction from `routing_ladder.go` into `domain/routing`; keep provider
   calls, classifier I/O, clock and cooldown storage in `infrastructure/llm`.
3. Add table tests for failure reasons, ladder selection, cooldown eligibility.
4. Verify routing disabled/enabled behavior and log output unchanged.

**Exit criteria:** identical retry/fallback decisions in existing routing tests;
domain has no I/O imports.

### Phase 3 — Package restructure (P0)

Order (leaves first, then aggregators):

1. `domain` subpackages + update internal imports.
2. `application` subpackages.
3. `infrastructure` subpackages (sqlite, cache, llm, onnx, embedding, metrics,
   config).
4. `handlers` subpackages + `httpx`.
5. `web` subpackages + `web/ui` renderer extraction.
6. `main.go` / `wiring.go` imports.
7. Move tests alongside their packages; update import paths only, not
   assertions.
8. Split `infrastructure/metrics.go` while moving it.

**Exit criteria:** `go list` graph matches §5.4; no cycles; all routes and
package tests pass.

### Phase 4 — Caps and readability (P1)

1. Split every production file >300 lines.
2. Reduce every production function >40 lines (start with §2.4).
3. Flatten nesting to ≤3 levels.
4. Translate comments to English; remove stale/duplicated comments.
5. Consolidate duplication only where behavior parity is demonstrated:
   cache decorators, model merging, form parsing.
6. Keep `web.NewServer` error return and `main()` shrinking from Phase 3/5.

**Exit criteria:** cap checker output empty or exceptions documented and
approved; English comments throughout non-UI code.

### Phase 5 — Composition and testability (P1/P2)

1. `web.NewServer` returns `(*Server, error)`; update 7 call sites.
2. Extract `main()` helpers; remove `log.Fatalf` from non-root packages.
3. Add package-local application tests with fakes.
4. Add delivery-port unit tests (handlers/web) with stubs.
5. Add tests for extracted subpackages missing coverage.
6. Keep the contract suite untouched except import paths.

**Exit criteria:** no `log.Fatalf` outside `main`/`wiring`; new tests pass;
race detector clean.

### Phase 6 — Build, CI, deploy (P2)

1. Add `.github/workflows/ci.yml` (gofmt, vet, race tests).
2. Audit `Dockerfile`, `compose.yml`, `render.yaml`, `config.yaml`,
   `.env.example`; fix inconsistencies without changing behavior.
3. Add Makefile targets per §12 decision.
4. Verify `docker build` + run + health/chat/embeddings/monitoring smoke.

**Exit criteria:** CI green; image builds and runs; env/config docs consistent.

### Phase 7 — Documentation and cleanup (P2)

1. Write `CONTRIBUTING.md` (English).
2. Translate/update `README.md` and its tree.
3. Update `TODO.md`; remove completed items, add follow-ups.
4. Final grep for French comments in Go/build files; French UI strings remain.

**Exit criteria:** docs link to the real tree; no stale instructions.

### Phase 8 — Final verification

1. Full `gofmt`/`vet`/`go test -race ./...`.
2. Import-boundary verification.
3. Cap verification.
4. Smoke matrix + Docker verification.
5. Diff review for accidental behavior changes (routes, payloads, defaults,
   strings, schema).
6. Prepare the single large PR description with phase commits and exceptions.

---

## 7. Behavior-preservation contract

### 7.1 Invariants

- All HTTP routes, methods, status codes, headers and JSON shapes unchanged.
- UI HTML behavior and French strings unchanged (modulo whitespace/imports in
  templates).
- CLI flags (`--config`, `--port`, `--db`, `--import-json`), env vars and
  precedence unchanged.
- `config.yaml` keys, defaults, seeds and routing behavior unchanged.
- SQLite schema, migrations and legacy import behavior unchanged.
- ONNX/Bifrost behavior and error semantics unchanged.
- Monitoring metrics semantics unchanged (counts, windows, caps).
- Log messages may change only where the refactor requires it; routing
  decisions must remain traceable.

### 7.2 Smoke matrix (minimum)

| Surface | Check |
|---|---|
| `GET /health/liveness`, `/health/readiness`, `/health/test_connection` | 200 + JSON |
| `GET /` | chat page renders |
| `POST /chat/send` | reply fragment; no config → existing error |
| `GET /models`, `POST /models`, `DELETE /models/{id}` | list/add/remove |
| `GET/POST /providers`, edit/delete | CRUD + masked key |
| `GET/POST /projects`, detail, configs, keys | CRUD/activate/duplicate/revoke |
| `GET /embeddings-test`, `POST /embeddings-test/compute` | local + ONNX choice behavior |
| `GET /monitoring`, `/monitoring/stats`, `/monitoring/metrics.json` | page/fragment/JSON |
| `POST /v1/chat/completions` (master + project key, stream/non-stream) | contract suite |
| `POST /v1/embeddings` | contract suite |
| `GET /v1/models`, `/v1/models/{model}` | contract suite |
| Unknown `/v1/*` | OpenAI 404 envelope |
| `GET /swagger`, `/openapi.yaml` | docs served |

### 7.3 Proof commands

```bash
gofmt -l .                                   # must be empty
go vet ./...
go test -race ./...
go build ./...

# NewServer call sites compile and template parse errors are returned.
go test ./web/... ./tests/...

# Import boundaries (manual review of output until an optional checker exists)
go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./... | sort
```

Contract tests must keep their assertions; only import paths/helpers may change.
Any intentional behavioral difference is out of scope and must be rejected or
split into a separate decision.

---

## 8. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Package move introduces import cycles | Build broken mid-refactor | Move leaves first; check `go list` after each step; keep `web/ui` free of controller imports |
| Typed-error migration changes API responses | Contract break | Map codes to the existing status/envelope; snapshot representative responses before/after; contract suite |
| Routing extraction changes decisions | Wrong tier/failover | Table tests from current behavior first; keep logging; compare routing tests |
| Strict caps force unsafe splits | Behavior drift/large diff | Split mechanically; no logic edits in the same commit as moves; document exceptions |
| One large PR is hard to review | Review fatigue/regressions | Ordered internal commits; PR description links phases; run full verification per phase |
| English translation touches user-visible text | UI behavior change | Translate only developer comments/docs; keep French UI/error text |
| Docker/render cleanup breaks deploy | Deployment outage | Verify image build/run; keep glibc/ONNX constraints; treat render schema changes cautiously |
| Hidden test gaps in application/web | Silent regressions | Add package-local tests where touched; contract suite + smoke matrix |
| Session-selected "one large PR" conflicts with safe rollout | Risk concentration | Behavior freeze + phase gates + local verification; owner controls merge timing |

---

## 9. Acceptance criteria

- [ ] `gofmt -l .` is empty; `go vet ./...` and `go test -race ./...` pass.
- [ ] `go build ./...` passes; Docker image builds and runs; health checks pass.
- [ ] Layer graph matches §5.4; no forbidden imports or cycles.
- [ ] Routing rules live in `domain/routing`; infrastructure only does I/O.
- [ ] Typed errors with stable codes; central HTTP mapping; no bare status
      decisions in handlers.
- [ ] `web.NewServer` returns an error; no `log.Fatalf`/`os.Exit` outside the
      composition root.
- [ ] Every production file ≤300 lines and every function ≤40 lines, or an
      explicit approved exception is documented.
- [ ] Nesting depth ≤3 in refactored production functions.
- [ ] English code/comments/docs; French UI strings unchanged.
- [ ] `tests/openai_contract_test.go` assertions unchanged and green.
- [ ] Smoke matrix passes for UI, API, monitoring and docs.
- [ ] `CONTRIBUTING.md` exists; README tree matches the new layout.
- [ ] CI workflow runs gofmt, vet and race tests on push/PR.
- [ ] Docker/compose/render/config/.env audited; no undocumented drift.
- [ ] `TODO.md` reflects completed work and remaining follow-ups.
- [ ] No API, UI, config, CLI, schema or deployment behavior changes.

---

## 10. Open decisions

These were not answered before the interview session ended. Recommended
defaults are listed; confirm or override before Phase 1.

1. **New-test scope.** Recommended: add tests only where a refactor touches
   untested behavior, keep the contract suite untouched, add package-local
   tests for extracted subpackages, and use light stubs at consumer ports. No
   coverage gate.
2. **Makefile test target.** Recommended: leave `make test` as the fast
   `tests/` run, add `make test-all` (`go test ./...`) and `make test-race`
   (`go test -race ./...`). Do not silently broaden `make test` if scripts
   depend on its speed.
3. **Behavior-proof method.** Recommended minimum: contract suite unchanged +
   the smoke matrix in §7.2 + Docker build/run. Golden HTML snapshots are not
   required unless requested.
4. **Caps on tests.** Recommended: caps apply to production files; test files
   are exempt from the 300-line cap but test functions should stay ≤40 lines
   where practical.
5. **`main.go`/`wiring.go` location.** Recommended: keep both at the repo root
   (composition root) and extract helpers; do not introduce a `bootstrap/`
   package in this pass.
6. **Package-name choices.** Recommended names in §5.2 (`domain/llm`,
   `domain/errs`, `application/catalog`, `infrastructure/llm`,
   `handlers/httpx`, `web/ui`); confirm before moving files.
7. **`render.yaml` top-level `networks`.** Recommended: remove it (not part of
   Render's schema) unless it is known to be required by the current account.

---

## Appendix A — Interview record

### Round 1

| Question | Answer |
|---|---|
| Primary outcome | Architecture audit + prioritized refactor plan; testability/test strategy; documentation/onboarding |
| Scope | Entire Go codebase; build/deploy configuration too |
| Trigger | Upcoming feature work; general readability/craftsmanship; external review/audit |
| Behavior contract | Strictly behavior-preserving |

### Round 2

| Question | Answer |
|---|---|
| Quality priorities | Readability/function-file size; error handling; duplication/abstraction; testability/coverage; layer boundaries; documentation/onboarding |
| Upcoming work | Observability expansion; embeddings/ONNX; more providers/routing; multi-tenant/auth/permissions; deployment/multi-instance readiness |
| External audience | Open-source maintainers/community |
| Guardrails | Minimal: gofmt/vet + lightweight CI test workflow |

### Round 3

| Question | Answer |
|---|---|
| Layout direction | Full subpackage layout (clarified in Round 4) |
| Domain purity | Move routing rules into `domain/` |
| Error convention | Typed errors with stable codes |
| Readability limits | Yes, strict caps |

### Round 4

| Question | Answer |
|---|---|
| Concrete layout | Layered roots with feature subfolders |
| Cross-feature imports | Direct imports allowed |
| Caps | 300 lines/file, 40 lines/function, 3 nesting levels |
| Language | English code/comments/docs; French UI stays |

### Round 5

| Question | Answer |
|---|---|
| TODO backlog | All four open items |
| Test bar | `go test -race ./...` in CI |
| Multi-instance | Design seams only |
| Performance | Behavior and tests only |
| Build/deploy | Lightweight CI; clean Docker/compose/render; review config/.env consistency |

### Round 6 (partial — session ended)

| Question | Answer |
|---|---|
| New-test scope | No selection recorded |
| Makefile test target | No selection recorded |
| Behavior-proof method | No selection recorded |
| Documentation artifacts | `CONTRIBUTING.md`; README updated to the new tree |
| Delivery | One large PR |

---

## Appendix B — What is explicitly excluded

- Adding Postgres/Redis or making the app horizontally scalable now.
- Performance tuning, benchmark gates, allocation budgets.
- New observability features beyond the existing monitoring surface.
- Auth/RBAC, quotas, multi-user tenancy.
- Changing the OpenAI API surface or the French UI wording.
- Rewriting ONNX/Bifrost internals or upgrading dependencies.
- Introducing a DI framework or a generated-code layer.
- Architecture docs beyond `CONTRIBUTING.md` + README (not selected).
