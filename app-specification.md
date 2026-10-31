# Application Specification — Bridge Gateway with Retrieval-Augmented Generation

| Field | Value |
|---|---|
| Version | 2.0 — 31 October 2026 |
| Repository | `bridge-gateway` (Bridge Gateway / BifrostAI) |
| Scope | The whole Go application, including the RAG bounded context |
| Audience | Developers, integrators and operators |
| Related docs | `README.md`, `clean-code-architecture-spec.md`, `CONTRIBUTING.md` |

This document describes what the application does and how it is built. It
reflects the code as it exists today, including the retrieval-augmented
generation (RAG) feature added on top of the existing LLM gateway.

---

## 1. Purpose

Bridge Gateway is an OpenAI-compatible gateway written in Go and built on the
BifrostAI SDK. It routes chat and embedding requests to local and remote
providers (Ollama, OpenAI, Mistral, Gemini, Anthropic, Groq, Azure, Vertex,
OpenRouter, vLLM and any OpenAI-compatible endpoint), applies per-project
enrichment, and exposes an HTMX administration UI, monitoring and a SQLite
persistence layer.

The **RAG bounded context** adds four capabilities on top of that gateway:

1. **Ingestion** — upload a text document for a project, split it into
   passages, embed the passages and store them in SQLite.
2. **Retrieval** — embed a question and rank the project's passages by cosine
   similarity.
3. **Grounded answering** — send the top passages to the project's active chat
   model with strict citation rules, and return the answer together with its
   sources.
4. **Per-project tuning** — save the embedding model, `top_k`, chunking and an
   extra system instruction, then exercise ingestion and querying from an
   HTMX test page.

Retrieval is driven by the application, not by the model. The chat model has
no tools and no network access; it only sees the passages selected by the
gateway. Any endpoint that supports chat completions is therefore sufficient.

---

## 2. System overview

### 2.1 Ingestion path

```text
multipart upload
  → authentication (project API key, or master key + explicit project)
  → project RAG settings (embedding model, chunk size/overlap)
  → text extraction (plain text, Markdown, CSV, JSON, XML, YAML, HTML)
  → structural chunking (boundary-aware, project settings; defaults 1000/200)
  → embeddings (local runtime by default, ONNX or remote on request)
  → SQLite: rag_documents row + rag_chunks rows (vectors stored as JSON)
  → 201 Created with the document metadata
```

Ingestion is **synchronous**: the response is returned once the document and
its passages are durable. There is no background queue, worker or spool in the
current version.

### 2.2 Query path

```text
question
  → authentication and project resolution
  → embedding of the question with the same model as the corpus
  → cosine similarity over the project's compatible passages
  → top_k passages (project settings, default 4, maximum 20)
  → system rules + numbered passages + question
  → project's active chat configuration → provider
  → answer, sources and has_context
```

### 2.3 What makes the deployment particular

| Mechanism | Effect |
|---|---|
| Two separate surfaces | `/v1/*` is the OpenAI-compatible gateway; `/v1/rag/*` is project-scoped RAG |
| One SQLite file | configurations, projects, keys, providers, catalog, RAG settings, documents and chunks |
| Reuse of the embedding router | RAG uses the same `local`, `onnx/...` and remote embedding engines as `/v1/embeddings` |
| Model isolation at retrieval | chunks embedded with another model or width are ignored for a query |
| No external vector database | ranking happens in-process on the passages of one project |
| Strict grounding | the model receives numbered passages and a no-context instruction, never a bare question |

---

## 3. Vocabulary and invariants

### 3.1 Vocabulary

| Term | Meaning |
|---|---|
| **Project** | Tenant. Owns configurations, API keys, RAG documents and chunks. |
| **Configuration** (`ChatConfig`) | Provider, model, endpoint, sampling settings and optional system prompt. One is active per project. |
| **API key** | Secret scoped to a project. Only its SHA-256 digest is stored; the plaintext is shown once. |
| **Master key** | Deployment-wide `BIFROST_MASTER_KEY`. On RAG routes it must name a project explicitly. |
| **Document** | One indexed file: project, file name, type, size, chunk count and embedding model. |
| **Chunk / passage** | One embedded slice of a document, with ordinal, text, embedding model and vector. |
| **Retrieval** | Cosine-similarity ranking of a project's passages for a question vector. |
| **Source** | Citation returned with an answer: document ID, file name, chunk index and score. |
| **Embedding model** | Identifier resolved by the existing embedding router: `local-embedding`, `onnx/<name>` or a remote model. |
| **Provider** | Chat engine used to generate the answer: Ollama, ONNX local, Bifrost, etc. |

### 3.2 Invariants

1. **The domain is pure**: `domain/` imports only the standard library and
   other `domain/` packages. No HTTP, SQL, SDK or template type crosses it.
2. **Dependencies point inward**: `handlers/`, `web/` and `infrastructure/`
   depend on `domain/`; `application/` depends on `domain/`; only the
   composition root assembles concrete types.
3. **A document and its chunks are consistent**: the document row is written
   before the chunks; a failed chunk write deletes the document row. Chunk
   replacement is transactional.
4. **A query never mixes embedding spaces**: chunks whose model or vector width
   differs from the query vector are discarded.
5. **A document belongs to exactly one project**: listing, querying and
   deletion are always scoped by the authenticated project.
6. **The model never receives unfetched context**: without a passage, the
   prompt explicitly says that no passage matched.
7. **API keys are never stored in clear**: only `sha256(secret)` is persisted.
8. **Readability caps are enforced**: at most 300 lines per production file,
   40 lines per function and 3 nested control-flow levels (`tools/caps`).

---

## 4. Architecture

### 4.1 Layers

| Layer | Packages | Owns |
|---|---|---|
| Domain | `domain/...` | Entities, value rules, outbound ports, typed errors, chunking, ranking, prompt construction |
| Application | `application/...` | Use-case orchestration, model/port composition, transaction ordering |
| Infrastructure | `infrastructure/...` | SQLite stores, text extraction, ONNX/Bifrost/cache/metrics/clipboard adapters, configuration loading |
| Delivery | `handlers/...`, `web/...` | HTTP/SSE/multipart/JSON translation, HTMX rendering, error envelopes |
| Composition root | `app.go`, `app_server.go`, `wiring.go`, `main.go` | Concrete wiring, startup, shutdown |

### 4.2 Repository layout

```text
domain/
├── chat/            Chat types, enrichment, ONNX chat rules
├── config/          ChatConfig, validation, schemas, tiers
├── embedding/       Embedding API types and curated model choices
├── errs/            Stable error codes and typed errors
├── llm/             LLMProvider and streaming ports
├── monitoring/      Metrics snapshot types
├── project/         Project and API key entities
├── provider/        Provider and catalog model entities
├── rag/             RAG entities, ports, chunking, cosine ranking, prompt
├── routing/         Tier rules, failure classification, cooldowns
└── shared/          Generic repositories, cache, ID generation

application/
├── auth/  catalog/  chat/  config/  embedding/  entity/  project/  provider/
└── rag/             Ingestion, retrieval and grounded-answer use case

infrastructure/
├── *store.go        SQLite repositories
├── rag_*.go         RAG document/chunk stores and text extractor
├── bifrost*.go      Bifrost adapters and conversions
├── onnx*.go         Local ONNX embedding and chat engines
├── routing*.go      Tier ladder and failover
└── config/ metrics/ Configuration loading and monitoring

handlers/
├── chat/  embeddings/  models/  rag/  httpx/
web/                 HTMX controllers, templates and static assets
docs/                Embedded OpenAPI document
tests/               End-to-end delivery tests
tools/caps/          Readability cap checker
```

### 4.3 Process lifecycle

**Construction (`newGatewayApp`)**

1. Load `config.yaml`.
2. Ensure ONNX artifacts exist (network failure is logged, not fatal).
3. Open SQLite and apply the idempotent schema migration.
4. Import legacy JSON stores on first start.
5. Build stores and use cases.
6. Build the LLM provider chain, the embedding router and the RAG use case.
7. Seed built-in providers and local catalog models.

**Serving** — `app_server.go` registers `/v1/*`, `/v1/rag/*`, legacy aliases,
health probes and the HTMX UI, then starts the HTTP server with graceful
shutdown.

**Shutdown** — stop cache sweeps, close the database, close the server.

---

## 5. Domain model

### 5.1 Gateway entities

| Entity | Role |
|---|---|
| `Project` | Tenant identity and description |
| `ChatConfig` | Provider/model/endpoint, sampling, response format, system prompt, tier, active flag |
| `APIKey` | Project-scoped secret metadata and SHA-256 digest |
| `Provider` | Named provider endpoint |
| `CatalogModel` | Model registered from the UI or seeded at startup |
| `ChatRequest` / `ChatMessage` | OpenAI-compatible conversation |
| `EnrichedRequest` | Request after system prompt, default model and response format are applied |
| `LLMResult` / `StreamEvent` | Chat reply or streamed delta |
| `EmbeddingRequest` / `EmbeddingResponse` | OpenAI-compatible embeddings |

### 5.2 RAG entities

**`Document`**

| Field | Meaning |
|---|---|
| `id` | Generated identifier |
| `project_id` | Owning project |
| `file_name` | Base name of the upload (path components removed) |
| `content_type` | Declared MIME type, best effort |
| `size_bytes` | Original byte size |
| `chunk_count` | Number of stored passages |
| `embedding_model` | Model used for every passage of this document |
| `created_at` | UTC creation timestamp |

**`Chunk`**

| Field | Meaning |
|---|---|
| `id` | `<document_id>:<ordinal>` |
| `document_id`, `project_id` | Ownership |
| `file_name` | Denormalized citation label |
| `ordinal` | Position in the document |
| `text` | Passage text |
| `embedding_model` | Model that produced `vector` |
| `vector` | Dense `float32` vector (JSON persistence only) |
| `created_at` | UTC creation timestamp |

**`Settings`**

| Field | Meaning |
|---|---|
| `project_id` | Owning project (one settings row per project) |
| `embedding_model` | Model used by ingestion and retrieval |
| `top_k` | Passages sent to the chat model (1–20) |
| `chunk_size` | Target passage size in runes |
| `chunk_overlap` | Overlap between adjacent passages (0 to chunk size − 1) |
| `system_prompt` | Optional additional RAG instruction (max 8 000 runes) |
| `updated_at` | UTC timestamp of the last save |

A missing settings row means `DefaultSettings(project_id)`: local embedding,
`top_k` 4, chunk size 1 000 and overlap 200.

**`RetrievedChunk`**, **`Source`** and **`Answer`**

- `RetrievedChunk` = chunk + cosine score.
- `Source` = document ID, file name, chunk index and score.
- `Answer` = text, sources, `has_context` and the model that actually replied.

### 5.3 Typed errors

`domain/errs` defines a stable `Code` (`validation_error`, `invalid_request`,
`not_found`, `conflict`, `unauthorized`, `forbidden`, `rate_limited`,
`provider_unavailable`, `timeout`, `canceled`, `internal_error`) and typed
errors (`ValidationError`, `InvalidRequestError`, `Error`). Delivery maps codes
to OpenAI error envelopes; RAG adds the `ErrDocumentNotFound` and
`ErrRAGSettingsNotFound` sentinels.

---

## 6. Ports and adapters

### 6.1 Gateway ports

| Port | Implemented by | Role |
|---|---|---|
| `domain.LLMProvider` | Bifrost adapter, ONNX chat router, cache/metrics/routing decorators | Chat completion |
| `domain.StreamingLLMProvider` | Bifrost and ONNX streaming adapters | Optional SSE streaming |
| `domain.EmbeddingProvider` | Local embedder, ONNX embedder, embedding router, Bifrost | Dense vectors |
| `domain.ChatConfigRepository` | `ChatConfigStore`, optional cache decorator | Configuration persistence |
| `domain.ProjectRepository`, `domain.APIKeyRepository` | SQLite stores, optional cache decorators | Tenants and keys |
| `domain.ProviderRepository`, `domain.ModelCatalogRepository` | SQLite stores | Provider and model catalog |
| `domain.Classifier` | Local model classifier | Tier routing |
| `domain.Cache` | In-memory TTL cache | Repository and LLM response caching |

### 6.2 RAG ports

| Port | Declared in | Implemented by | Methods |
|---|---|---|---|
| `DocumentRepository` | `domain/rag` | `RAGDocumentStore` (SQLite) | list, get, create, update, delete, list by project |
| `ChunkRepository` | `domain/rag` | `RAGChunkStore` (SQLite) | replace for document, list by project, delete by document |
| `TextExtractor` | `domain/rag` | `RAGTextExtractor` | extract plain text from one upload |
| `SettingsRepository` | `domain/rag` | `RAGSettingsStore` (SQLite) | get and upsert per-project settings |

The RAG use case also consumes `domain.EmbeddingProvider`,
`domain.ChatConfigRepository` and `domain.LLMProvider`. It never imports a
concrete adapter.

### 6.3 Domain services

| Function | Responsibility |
|---|---|
| `rag.ChunkText` | Boundary-aware, overlapping windows over runes |
| `rag.CosineSimilarity` | Cosine similarity, safe on empty and zero vectors |
| `rag.RankChunks` | Filter by vector width, score, sort, cap at `top_k` |
| `rag.Sources` | Project retrieved passages into citations |
| `rag.BuildMessages` | System grounding rules, numbered passages, no-context fallback |

---

## 7. RAG use cases

### 7.1 Ingestion — `application/rag.Ingest`

1. Normalize and validate the project, file name and non-empty content.
2. Extract text through `TextExtractor`. Unsupported binary formats are
   rejected with an explicit 400.
3. Split into passages with `rag.ChunkText`.
4. Apply the project RAG settings, then resolve the embedding configuration:
   - the request model when provided;
   - otherwise the project's saved embedding model;
   - otherwise `local-embedding`, which needs no project configuration and
     works offline.
5. Embed all passages in one provider call and check that the number and order
   of returned vectors match.
6. Build the document and deterministic chunk IDs (`document:ordinal`).
7. Persist the document, then replace its chunks in a transaction. If the chunk
   write fails, the document row is deleted.

### 7.2 Retrieval — `application/rag.Search`

1. Validate project and question (non-empty, at most 8 000 runes).
2. Load the project's chunks.
3. Apply the project RAG settings (request overrides win).
4. Choose the embedding model: request override, then project settings, then
   the model of the first stored passage.
5. Embed the question.
6. Keep only chunks with the same embedding model and vector width.
7. Rank by cosine similarity and return at most `top_k` (default 4, max 20).

An empty corpus returns no passages without calling the embedding provider.

### 7.3 Grounded answer — `application/rag.Query`

1. Run retrieval.
2. Resolve the project's active chat configuration.
3. Build messages:
   - a system prompt requiring strict grounding, numbered citations,
     no cross-document mixing, two or three sentences, and an explicit
     admission when the corpus does not cover the topic;
   - one user message with `[n] file (passage k)` plus flattened passage text
     (truncated to 1 500 runes);
   - when nothing matched, an explicit no-context instruction instead of the
     bare question.
4. Apply the project system prompt unless the request overrides it.
5. Call the chat provider.
6. Return `Answer` with the reply, one source per passage, `has_context` and
   the model that actually replied.

The project settings may supply an additional `system_prompt`; a request
`system_prompt` overrides it for that call. It is placed before the built-in
rules and cannot remove them.

### 7.4 Settings — `application/rag.Settings` / `SaveSettings`

- `Settings(project)` returns the stored parameters, or the domain defaults when
  the project never saved any.
- `SaveSettings` normalizes, validates (`top_k` 1–20, chunk size > 0,
  overlap in `[0, chunk_size)`, prompt ≤ 8 000 runes), timestamps and upserts
  the row.
- Ingestion and retrieval apply the stored values as fallbacks; explicit
  request options still win.

### 7.5 Document management

- `Documents(project)` lists the project's documents, newest first.
- `DeleteDocument(project, id)` verifies ownership, deletes chunks, then the
  document. A document from another project is reported as not found.

---

## 8. HTTP API

### 8.1 Conventions

- Entity authentication: `Authorization: Bearer <project key>`.
- Master authentication: `Authorization: Bearer <BIFROST_MASTER_KEY>` plus
  `X-Project-ID: <project>` or `?project_id=<project>` on RAG routes.
- JSON errors use the OpenAI envelope:
  `{"error":{"message":"...","type":"...","param":...,"code":...}}`.
- Uploads use `multipart/form-data` with a `file` field.
- RAG responses are JSON; RAG does not expose SSE.

### 8.2 Gateway and UI routes (existing)

| Method | Route | Effect |
|---|---|---|
| `POST` | `/v1/chat/completions` | Chat completion, streamed when `stream` is true |
| `POST` | `/v1/embeddings` | OpenAI-compatible embeddings |
| `GET` | `/v1/models` | List known models |
| `GET` | `/v1/models/{model}` | Retrieve one model |
| `POST` | `/chat/completions` | Legacy alias |
| `POST` | `/embeddings` | Legacy alias |
| `GET` | `/health/liveness`, `/health/readiness`, `/health/test_connection` | Health probes |
| `GET` | `/docs`, `/openapi.yaml` | Documentation and OpenAPI |
| UI | `/`, `/models`, `/providers`, `/projects`, `/monitoring`, `/rag/settings`, `/rag/test` | HTMX interface |

RAG UI routes:

| Method | Route | Effect |
|---|---|---|
| `GET` | `/rag/settings` | Per-project RAG parameters form (`?project_id=`) |
| `POST` | `/rag/settings` | Validate and save the project settings |
| `GET` | `/rag/test` | Upload, document list, query and search bench (`?project_id=`) |
| `POST` | `/rag/test/upload` | Multipart ingestion from the test page |
| `POST` | `/rag/test/query` | Answer or search-only mode, rendered as an HTMX fragment |
| `POST` | `/rag/test/documents/{id}/delete` | Delete a document from the test page |

### 8.3 RAG routes

| Method | Route | Effect |
|---|---|---|
| `POST` | `/v1/rag/documents` | Upload and index one document (`file`, optional `model`) |
| `GET` | `/v1/rag/documents` | List the project's documents |
| `DELETE` | `/v1/rag/documents/{id}` | Delete a document and its passages |
| `POST` | `/v1/rag/query` | Retrieve and answer: `{question, model?, top_k?, system_prompt?}` |
| `POST` | `/v1/rag/search` | Retrieve only: same body, returns `{chunks, total}` |

Upload response (`201`):

```json
{
  "id": "2e0e…",
  "project_id": "project-…",
  "file_name": "notes.txt",
  "content_type": "text/plain",
  "size_bytes": 42,
  "chunk_count": 1,
  "embedding_model": "local-embedding",
  "created_at": "2026-10-31T12:00:00Z"
}
```

Query response (`200`):

```json
{
  "answer": "Orion is the alpha project [1].",
  "sources": [
    {"document_id": "2e0e…", "file_name": "notes.txt", "chunk_index": 0, "score": 1}
  ],
  "has_context": true,
  "model": "gemma4:e4b"
}
```

Search response (`200`):

```json
{
  "chunks": [
    {"document_id": "2e0e…", "file_name": "notes.txt", "chunk_index": 0,
     "text": "The alpha project is called Orion.", "score": 1}
  ],
  "total": 1
}
```

### 8.4 Error mapping

| Condition | HTTP |
|---|---|
| Missing or invalid API key | `401` |
| Empty project, file, question or validation failure | `400` |
| Unsupported file type, empty document | `400` |
| Document not found / belongs to another project | `404` |
| Upload above 25 MiB | `413` |
| No active configuration for the project | `400` |
| Provider, embedding or persistence failure | `4xx/5xx` from its typed code |
| Unhandled internal error | `500` |

---

## 9. Document formats and limits

| Format | Support |
|---|---|
| `.txt`, `.md`, `.markdown`, `.csv`, `.json`, `.log`, `.yaml`, `.yml`, `.xml` | Decoded as UTF-8 text |
| `.html`, `.htm` | Tags removed and entities decoded |
| PDF, Office, images, archives | Rejected with an explicit unsupported-type error |

| Limit | Value |
|---|---|
| Upload size | 25 MiB |
| Extracted text | 5 000 000 runes (truncated) |
| Chunk size / overlap | 1 000 / 200 runes by default |
| Passage length in prompt | 1 500 runes |
| Question length | 8 000 runes |
| Default / maximum `top_k` | 4 / 20 |

Defaults are constants in `domain/rag`; `top_k`, chunk size and overlap are
configurable per project from `/rag/settings`. Requests may still override
`top_k`, `model` and `system_prompt` per call. The upload limit and extraction
limit remain deployment constants.

---

## 10. Persistence

### 10.1 SQLite

One file, `data/bridge.db` by default (`--db` overrides it). The pure-Go
`modernc.org/sqlite` driver keeps `CGO_ENABLED=0` builds valid. Every table
uses `(id TEXT PRIMARY KEY, payload TEXT NOT NULL)` with JSON payloads, plus
JSON indexes on filtered fields.

| Table | Contents | Indexes |
|---|---|---|
| `projects` | Projects | — |
| `chat_configs` | Chat configurations | project, project+active |
| `api_keys` | Key metadata and digest | project, digest |
| `providers` | Provider endpoints | name |
| `catalog_models` | Registered models | provider |
| `rag_settings` | Per-project RAG parameters | — (primary key: project ID) |
| `rag_documents` | Indexed documents | project |
| `rag_chunks` | Embedded passages and vectors | project, document |

Legacy JSON stores under `data/` are imported idempotently on first start.

### 10.2 RAG chunk replacement

`ReplaceForDocument` opens one transaction, deletes the previous chunks of the
document, inserts the new ones and commits. A reader therefore never sees a
half-replaced document.

### 10.3 Embedding model changes

A chunk records the model that produced its vector. When the corpus must switch
embedding models, save the new model in `/rag/settings`, delete the old
documents and re-upload them: old and new vectors coexist in storage but
retrieval only compares the model it used for the query. The previous document
must be deleted first to avoid duplicate passages with the same ordinal.

---

## 11. Configuration

### 11.1 Files and flags

| Setting | Source |
|---|---|
| Models, cache, routing, classifier | `config.yaml` |
| Listen port | `--port`, else `BRIDGE_PORT`, else `8080` |
| Database | `--db`, else `data/bridge.db` |
| Legacy JSON directory | `--import-json`, else `data` |
| Master key | `BIFROST_MASTER_KEY` |
| ONNX assets | `models/onnx/`, downloaded at first start unless `ONNX_AUTO_DOWNLOAD=0` |
| ONNX runtime library | `ONNX_RUNTIME_LIB` |

The UI stays in French; code, comments and developer documentation are in
English.

### 11.2 Embedding selection for RAG

| Value | Engine |
|---|---|
| empty or `local-embedding` | Pure-Go local embedder (deterministic lexical vectors, 384 dimensions) |
| `onnx/<name>` | Local ONNX model under `models/onnx/<name>/` |
| Another curated or catalog model | Routed by the existing embedding router to its provider |
| Any other identifier | Uses the project's active configuration provider |

The embedding request is built from the project's active configuration only to
reuse its endpoint; local and ONNX models explicitly clear `base_url` and
`api_key`.

### 11.3 Chat selection for RAG

The project's **active** `ChatConfig` supplies provider, model, endpoint,
sampling and response format for `Query`. There is no fallback configuration:
without an active configuration, the request fails with a clear error.

---

## 12. Authentication and isolation

| Situation | Result |
|---|---|
| `/v1/*`, `/v1/rag/*` with a project key | Acts on that project |
| RAG with the master key and `X-Project-ID` / `project_id` | Acts on the named project |
| RAG with the master key without a project | `400` |
| Missing or invalid key | `401` |
| Project key deleted or revoked | `401` |
| Project key of another project | Cannot read or delete its documents |

Isolation rules:

- document listing, querying, deletion and chunk access are always filtered by
  the authenticated project ID;
- a document ID from another project is treated as not found;
- chunk IDs include their document ID and could not collide across documents;
- the model only sees passages from the authenticated project.

---

## 13. Console and monitoring

The HTMX UI covers chat, models, providers, projects, configurations, keys,
monitoring and two RAG pages:

- **RAG — Paramètres** (`/rag/settings`): per-project embedding model,
  `top_k`, chunk size/overlap and extra system instruction.
- **RAG — Test** (`/rag/test`): upload documents, list/delete the corpus, ask
  a grounded question and inspect the retrieved passages in search-only mode.

The API surface remains `/v1/rag/*`. Monitoring covers gateway traffic and LLM
calls, not RAG-specific counters.

---

## 14. Tests and quality

| Item | State |
|---|---|
| Framework | Go `testing`, `httptest`, `go test -race` |
| Domain tests | Chunking, cosine ranking, prompt construction |
| Application tests | Ingestion, retrieval, grounded answers, deletion, validation |
| Adapter tests | SQLite document/chunk round trip, HTML stripping, binary rejection |
| Delivery tests | Multipart upload, listing, query, search, deletion, 401/400 paths |
| Readability | `go run ./tools/caps .` (300 / 40 / 3) |
| Commands | `make fmt`, `make vet`, `make test`, `make test-all`, `make test-race`, `make caps`, `make ci` |

The architecture check (`bash .agents/skills/ddd-hexagonal/scripts/check-architecture.sh`)
must pass for changes touching the layers.

---

## 15. Known limits and out of scope

**Assumed limits**

1. **In-process retrieval** — every passage of a project is loaded from SQLite
   and scored in memory. This is appropriate for local and small/medium
   corpora; a dedicated vector index is not used.
2. **Synchronous ingestion** — a large document keeps the HTTP request open.
3. **Text formats only** — PDF, Office and image OCR are not extracted.
4. **No reranker, BM25 or hybrid fusion** — ranking is dense cosine similarity
   only.
5. **No incremental re-embedding** — changing the embedding model requires
   re-uploading documents.
6. **No RAG-specific monitoring** — ingestion and query latency are not
   reported in the monitoring snapshot.
7. **Single application instance** — SQLite and in-process caches assume one
   writer; horizontal scaling would require shared storage and a vector store.
8. **Vectors are stored as JSON** — simple and dependency-free, but larger and
   slower than binary blobs.

**Out of scope v1**

- Asynchronous ingestion queues, workers and resumable tasks.
- PDF/Office parsing, OCR and vision models.
- Cross-project retrieval (a project can only search its own corpus).
- Streaming RAG answers.
- Chunk-level access control beyond project isolation.
- Evaluation dashboards for retrieval quality.
- Facturation, quotas and audit logs.

---

## 16. Annex — RAG file map

```text
domain/rag/
├── model.go       Document, Chunk, RetrievedChunk, Source, Answer, ports
├── chunking.go    ChunkText, window sizing and boundary helpers
├── settings.go    Settings, defaults, validation and SettingsRepository
├── search.go      CosineSimilarity, RankChunks, Sources
└── prompt.go      BuildMessages, grounding rules, no-context prompt

application/rag/
├── service.go     Service, options, constructor
├── ingest.go      Ingest, Documents, DeleteDocument, persistence
├── query.go       Query, Search, model compatibility
├── settings.go    Settings defaults, save and request fallback
└── embed.go       Embedding config resolution and ordered vectors

infrastructure/
├── rag_document_store.go  DocumentRepository on SQLite
├── rag_chunk_store.go     ChunkRepository on SQLite (transactional replace)
├── rag_settings_store.go  SettingsRepository on SQLite (upsert)
└── rag_extractor.go       Text extraction and HTML stripping

handlers/rag/
├── handler.go     Authentication scope and JSON helper
├── documents.go   Upload, list and delete
└── query.go       Query and search
```
