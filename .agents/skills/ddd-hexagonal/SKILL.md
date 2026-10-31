---
name: ddd-hexagonal
description: Enforce Domain-Driven Design and Hexagonal Architecture (Ports & Adapters) in this Go repository. Use when adding, reviewing, or refactoring code in domain/, application/, infrastructure/, handlers/, web/, or the composition root, and before finishing any change that must respect the layer boundaries and the 300-line file / 40-line function caps.
---

# DDD & Hexagonal Architecture (Go)

Apply this skill to every change under `domain/`, `application/`,
`infrastructure/`, `handlers/`, `web/` or the composition root (`app.go`,
`app_server.go`, `wiring.go`, `main.go`). Place new code by responsibility
first, then run the checks at the bottom.

## Non-negotiable rules

1. **Size caps** — at most **300 lines per file**, **40 lines per function**
   and **3 nested control-flow levels**. `tools/caps` enforces all three
   (`make caps`); tests are excluded. Split by responsibility, never into
   arbitrary line chunks.
2. **Dependency rule** — dependencies point inward only. Domain knows nothing
   about delivery, persistence, HTTP, databases or vendor SDKs.
3. **Ports** — outbound ports are interfaces owned by `domain/`. Inbound
   behaviour is implemented by use cases in `application/`. Delivery declares
   the narrow interfaces it consumes.
4. **Adapters** — SQL, HTTP/SSE framing, JSON mapping, templates, SDK calls and
   framework plumbing live only in `infrastructure/`, `handlers/` and `web/`.
5. **Composition root** — only `app.go`, `app_server.go`, `wiring.go` and
   `main.go` assemble concrete types.
6. **Behaviour preservation** — do not rename API fields, change JSON shapes,
   reorder routes or translate user-visible text unless the task asks for it.

## Layer map

| Layer | Packages | Owns | May import |
|---|---|---|---|
| Domain | `domain/...` | entities, value objects, aggregates, invariants, domain services and events, outbound ports, typed errors | stdlib and `domain/...` |
| Application | `application/...` | use case orchestration, transaction ordering, domain + port composition | stdlib and `domain/...` |
| Infrastructure | `infrastructure/...` | SQLite stores, Bifrost/ONNX adapters, caches, metrics, config loading | stdlib, `domain/...`, third-party SDKs |
| Delivery | `handlers/...`, `web/...` | HTTP/SSE/JSON/HTMX translation, transport validation, error rendering | stdlib, `domain/...`, `application/...`, own subpackages |
| Composition root | root `*.go` files | wiring, decorators, startup and shutdown | everything |

`go list` is the source of truth; `scripts/check-architecture.sh` enforces this
table. `tests/` is an end-to-end package and is intentionally exempt.

## DDD building blocks in Go

- **Bounded context** = a `domain/<context>` package. One ubiquitous language
  per context; do not leak another context's types across the boundary.
- **Entity** — identity and lifecycle. Mutations go through intent-revealing
  methods (`Activate`, `Issue`) or a validating use case, never through blind
  setters.
- **Value object** — immutable, equality by value, validation in its
  constructor. Prefer it over a primitive when the primitive has rules.
- **Aggregate root** — the only entry point for changes to its cluster. One
  transaction changes one aggregate. Reference other aggregates by ID, not by
  pointer, and enforce every invariant inside the aggregate.
- **Domain service** — stateless rule that spans several entities or value
  objects and does not belong to one of them.
- **Domain event** — a past-tense fact (`InvoiceIssued`) raised by the
  aggregate. It carries domain data only, never infrastructure handles.
- **Repository** — a domain port with collection semantics
  (`Get`/`List`/`Create`/`Update`/`Delete`). No query builders, SQL or ORM
  types in the signature. See `domain/shared/store.go` for the generic base.
- **Errors** — return domain/typed errors (`domain/errs`, `errs.ValidationError`,
  `errs.Code`). Adapter errors must not escape an adapter unchanged.

## Ports & adapters in this repository

The canonical layout (`application/port/in`, `application/port/out`) maps to a
flatter, established Go convention here. Follow the existing one:

- **Outbound port** — interface declared in `domain/`, next to the aggregate
  (`domain.ProjectRepository` in `domain/project/project.go`,
  `domain.LLMProvider` in `domain/llm`). Implemented by `infrastructure/`
  (SQLite stores, Bifrost, ONNX, cache decorators).
- **Inbound port** — exported use case struct in `application/<context>/`
  (`project.APIKeyUseCase`), constructed with its ports and pure collaborators
  (`idGen`, `now`, `clock`). `Execute`-style methods, one per use case.
- **Consumer port** — the minimal interface delivery actually needs, declared
  in `handlers/ports.go` or `web/ports.go` and satisfied by the use case.
  DIP + ISP: a new use case method must not widen the consumer's surface.

Do not add a `port/in` / `port/out` tree: it would duplicate the domain-owned
ports and break the house layout. Anti-corruption mapping (Bifrost/OpenAI/ONNX
shapes) belongs in the adapter, e.g. `infrastructure/bifrost_convert.go`.

## Workflow

1. **Locate the context.** Reuse an existing `domain/<context>`; create one only
   for a genuinely new bounded context, not for a single feature.
2. **Model first.** Add the entity/value object/aggregate with constructor and
   invariant checks, plus domain errors. No framework import, no persistence
   concern, no HTTP concept.
3. **Declare ports.** Add the smallest outbound interface the use case needs.
   Compose `shared.CrudRepository` / `shared.UpdatableRepository` instead of
   re-declaring CRUD methods.
4. **Write the use case.** In `application/<context>/`, inject ports through the
   constructor, orchestrate, map adapter/domain errors, return domain types.
   Keep it free of SQL, `http.Request`, JSON encoding and SDK types.
5. **Write the adapter.** In `infrastructure/`, implement the port and map
   rows/JSON/SDK structures to domain types. Add the compile-time assertion:
   `var _ domain.ProjectRepository = (*ProjectStore)(nil)`.
6. **Write delivery.** In `handlers/` or `web/`, parse and validate transport
   input, call the use case through the consumer port, and render
   success/errors. No business rule, no direct infrastructure access.
7. **Wire it.** Touch the composition root last; decorators (cache, failover,
   routing) wrap ports there, not inside use cases.
8. **Verify.** Run the commands below and the tests that cover the behaviour.

## Red flags

- `domain/` importing `net/http`, `database/sql`, an SDK, or another layer.
- Business rules (tier choice, retries, defaults, validation of domain state)
  inside `handlers/`, `web/` or `infrastructure/`.
- A use case reaching for a concrete store, global, or package-level singleton.
- HTTP status codes, `http.Request`, template names or SSE framing in
  `domain/` or `application/`.
- Delivery importing `infrastructure/` "just to construct one thing".
- A repository method shaped like a query (`FindByStatusOrderByCreatedAt`):
  express the need as a domain concept instead.
- A 45-line function or an inverted dependency hidden behind an unexported
  helper.
- Duplicated cross-context models instead of an explicit mapping in the
  adapter.

## Verification

```bash
# Layer boundaries + readability caps (300 / 40 / 3)
bash .agents/skills/ddd-hexagonal/scripts/check-architecture.sh

# Individual checks
bash .agents/skills/ddd-hexagonal/scripts/check-architecture.sh --boundaries-only
make caps
make fmt vet

# Behaviour and coverage
go test ./... -count=1        # full suite
make test-race                # before finishing a non-trivial change
```

For a read-only Mermaid view of the current package graph:

```bash
bash .agents/skills/ddd-hexagonal/scripts/context-map.sh
```

## Definition of done

- The dependency rule still holds and `check-architecture.sh` passes.
- Every touched Go file is within the caps; oversized code was split by
  responsibility.
- New behaviour has a domain test (rules) and, where relevant, a use case or
  delivery test; end-to-end paths are covered under `tests/`.
- Delivery and adapters contain no business rule; the use case contains no
  transport or persistence detail.
- Public API shapes, UI strings and behaviour outside the task are unchanged.

See `references/templates.md` for copy-ready Go skeletons and
`references/review-checklist.md` for the review pass.
