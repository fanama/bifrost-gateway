# Review Checklist — DDD & Hexagonal

Run this pass on every diff that touches production Go code. Report violations
by file and line; fix the cause, not the checker.

## 1. Placement

- [ ] The type lives in the layer that owns its responsibility (rule → domain,
      orchestration → application, technology → infrastructure, transport →
      handlers/web).
- [ ] A new package is a real bounded context, not a single feature.
- [ ] The context's own language is used; another context's types are not
      leaked in.
- [ ] Wiring changes are confined to the composition root.

## 2. Dependency rule

- [ ] `domain/` imports only stdlib and `domain/...`.
- [ ] `application/` imports only stdlib and `domain/...`.
- [ ] `infrastructure/` imports `domain/...` plus third-party adapters — never
      `application/`, `handlers/` or `web/`.
- [ ] `handlers/` and `web/` never import `infrastructure/`; they depend on
      consumer ports.
- [ ] No package-level mutable singleton or `init()` side effect hides a
      dependency.
- [ ] `bash .agents/skills/ddd-hexagonal/scripts/check-architecture.sh
      --boundaries-only` passes.

## 3. Domain model

- [ ] Entities have identity; value objects are immutable and validated at
      construction.
- [ ] Aggregates expose intent-revealing methods, not setters that bypass
      invariants.
- [ ] A single transaction changes one aggregate; other aggregates are
      referenced by ID.
- [ ] Domain services are stateless and only exist for cross-entity rules.
- [ ] Domain events are past-tense facts carrying domain data only.
- [ ] Errors are typed (`domain/errs`), stable and actionable; no raw adapter
      error is returned to a caller who cannot classify it.
- [ ] No HTTP status, SQL, JSON-wire, template or SDK concept in the domain.

## 4. Ports and adapters

- [ ] Each use case depends on the smallest outbound interface it needs (ISP).
- [ ] Ports use domain types only; no `*sql.DB`, `http.Request`, SDK structs or
      `map[string]any` in their signatures.
- [ ] CRUD ports compose `shared.CrudRepository` / `shared.UpdatableRepository`
      rather than duplicating methods.
- [ ] Each adapter has a compile-time assertion against its port.
- [ ] Mapping between storage/vendor shapes and domain types happens in the
      adapter, never in the use case.
- [ ] Cache/failover/routing decorators wrap ports in the composition root and
      remain substitutable for the port (LSP).

## 5. Use cases and delivery

- [ ] One exported use case per operation; constructor injection for ports and
      side-effect collaborators (clock, ID generator, hasher).
- [ ] The use case contains no transport concern (status code, header, SSE
      frame, HTML) and no persistence concern.
- [ ] Delivery validates the transport shape, calls one use case, renders the
      result, and maps errors through the existing helper.
- [ ] Consumer interfaces are declared by the consumer, not widened for a new
      use case method.
- [ ] User-visible behaviour and French UI text are unchanged unless requested.

## 6. Readability budget

- [ ] No file exceeds 300 lines.
- [ ] No function exceeds 40 lines.
- [ ] No function nests more than 3 control-flow levels.
- [ ] Oversized code was split by responsibility (not by line count), with
      names that explain the split.
- [ ] Comments explain *why*; no narration of obvious code.
- [ ] `make caps` passes.

## 7. Tests and verification

- [ ] Domain rules are covered by table-driven tests in `domain/...`.
- [ ] Use cases are tested against fakes of their ports (no database).
- [ ] Adapters are tested against real SQLite or recorded vendor responses.
- [ ] End-to-end behaviour is covered under `tests/` when the request touches
      HTTP or the UI.
- [ ] `go test ./... -count=1` passes; `make test-race` for non-trivial changes.
- [ ] `make fmt vet` passes.
