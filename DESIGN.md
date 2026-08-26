# Wayminder design and implementation plan

Status: MVP implemented
Project: `wayminder`
Deployment target: a small self-hosted Linux server
Tagline: **Remember the way.**

## 1. Purpose

Wayminder is a self-hosted, local-first memory service for AI agents. It allows
Codex, Claude Code, and other MCP-compatible agents to share durable knowledge
across sessions without sending memory contents to an external embedding API.

The service should remember information that remains useful after a task ends:

- system and deployment facts;
- repository conventions and architectural decisions;
- user preferences;
- proven fixes and operational procedures;
- references an agent is likely to need again.

It should not become a transcript archive. Secrets, credentials, transient logs,
speculation, and short-lived task state do not belong in long-term memory.

## 2. Goals

- Provide an MCP Streamable HTTP endpoint usable by Codex and Claude Code.
- Store memories transactionally in a local PostgreSQL/pgvector database.
- Perform semantic recall using a compact embedding model running locally.
- Isolate memories with explicit scopes such as `global`, `repo:<name>`, and
  `host:<name>`.
- Preserve provenance and version history for every mutation.
- Prefer deterministic behavior in the request path.
- Remain small enough to operate as a Docker Compose stack on a modest server.
- Make retrieval behavior observable and explainable.
- Support backup and human-readable export without making files the primary
  database.

## 3. Non-goals for the first release

- Multi-tenant SaaS operation.
- Cloud embeddings or mandatory external model APIs.
- A general chat interface.
- An LLM call for every memory read or write.
- Multi-human corroboration and promotion workflows.
- A knowledge-graph visualization UI.
- Automatic ingestion of complete conversations.
- Autonomous rewriting of memory without an auditable maintenance action.

## 4. System architecture

```text
Codex / Claude Code / MCP client
              |
              | Streamable HTTP + bearer token
              v
       Wayminder (Go)
        |           |
        |           +--> local embedding provider
        |                (compact embedding model)
        v
PostgreSQL 16 + pgvector
```

The initial Docker Compose stack will contain:

1. `wayminder`: the Go MCP and health/API service;
2. `postgres`: PostgreSQL with the pgvector extension;
3. Ollama running `nomic-embed-text` locally.

PostgreSQL will only be reachable on the private Compose network. Wayminder will
be the sole database client exposed to other hosts.

## 5. Technology choices

### Application

- Go, matching the surrounding personal infrastructure.
- Official Model Context Protocol Go SDK.
- Standard `net/http` server and middleware.
- `pgx` for PostgreSQL access.
- SQL migrations embedded in the binary.
- Structured JSON logging.
- ULIDs for sortable identifiers.

### Database

- PostgreSQL 16.
- pgvector for cosine similarity search.
- PostgreSQL full-text search for exact terms, identifiers, and error strings.
- Hybrid ranking combining semantic and lexical retrieval.

### Embeddings

The default should be a compact local English embedding model. The model and its
vector dimension are deployment-coupled configuration: changing either requires
re-embedding every live memory or creating a new vector column.

The MVP uses `nomic-embed-text` through Ollama (768 dimensions). Ollama owns
tokenization, pooling, normalization, model download, and process lifecycle.
An ONNX adapter remains a possible later optimization if deployment benchmarks show
that removing the sidecar is worth the CGO and native-runtime complexity.

The provider interface will keep the application independent of the initial
runtime:

```go
type Embedder interface {
    Embed(context.Context, []string) ([][]float32, error)
    Dimension() int
    Model() string
}
```

The intended deployment class has enough CPU and memory for a compact embedding
model. The choice will be based on measured warm latency, cold-start
behavior, image size, and operational complexity.

## 6. Data model

### `memories`

| Column | Purpose |
|---|---|
| `id` | ULID primary key |
| `content` | Canonical memory text |
| `summary` | Optional compact human-readable summary |
| `kind` | `fact`, `decision`, `preference`, `procedure`, or `reference` |
| `scope` | Visibility context such as `global` or `repo:wayminder` |
| `tags` | Searchable labels |
| `embedding` | pgvector embedding |
| `embedding_model` | Model identity used to produce the vector |
| `author_agent` | Agent or client that wrote the memory |
| `source` | Repository, session, host, or workflow provenance |
| `supersedes_id` | Previous version, when applicable |
| `created_at` | Creation timestamp |
| `updated_at` | Last update timestamp |
| `deleted_at` | Soft-deletion timestamp |

### `memory_links`

Optional explicit relationships between memories:

- `related_to`;
- `depends_on`;
- `contradicts`;
- `derived_from`.

Relationships are useful for explainability and maintenance, but semantic recall
does not depend on agents creating them correctly.

### `recall_traces`

Bounded diagnostic records containing the query, scope, selected memory IDs,
individual rank signals, total duration, and caller. Trace retention will be
configurable and tracing failures must never fail a recall request.

## 7. MCP interface

### `remember`

Store durable knowledge.

Inputs:

- `content` (required);
- `kind`;
- `scope`;
- `tags`;
- `source`.

Before insertion, Wayminder searches within the same effective scope for a
near-duplicate. It may return the existing memory, supersede it, or require an
explicit update depending on similarity and conflicting content.

### `recall`

Return ranked live memories for a natural-language query.

Inputs:

- `query` (required);
- `scope`;
- `kind`;
- `limit`.

Results include content, provenance, scope, similarity/rank information, and the
memory ID so the agent can cite or amend the source.

### `list_memories`

Browse recent live memories with filters and cursor pagination.

### `supersede`

Replace a memory while retaining the previous version for audit and rollback.

### `forget`

Soft-delete a memory. Hard deletion is reserved for retention maintenance or an
explicit administrative operation.

### `status`

Report database connectivity, embedding provider/model/dimension, memory counts,
scope counts, stale entries, and pending maintenance signals.

## 8. Scope semantics

Scopes are namespaced strings rather than a fixed enum. Initial conventions:

- `global`: useful to every connected agent;
- `repo:<repository>`: repository-specific knowledge;
- `host:<hostname>`: infrastructure knowledge for one machine;
- `project:<name>`: knowledge spanning several repositories;
- `personal`: user preferences that are not repository-specific.

A scoped recall should search `global`, `personal`, and the requested scope. An
unscoped recall should not silently search every scope; callers must opt into a
broad administrative search. This reduces accidental cross-project leakage.

## 9. Retrieval and ranking

Recall will use hybrid retrieval:

1. embed the query;
2. select live memories in the effective scopes;
3. retrieve semantic candidates with pgvector cosine distance;
4. retrieve lexical candidates with PostgreSQL full-text search;
5. combine ranks using reciprocal-rank fusion or a small deterministic weighted
   score;
6. apply kind, recency, and exact-tag signals without hiding older durable facts;
7. return the top results with rank evidence.

Exact lexical retrieval matters for hostnames, commands, identifiers, and error
messages that semantic models may represent poorly.

## 10. Agent activation and seed memory

Connecting an MCP server does not guarantee an agent will remember to use it.
Wayminder will provide a compact, dynamically generated overview through:

- the MCP server `instructions` field;
- the `recall` tool description as a compatibility fallback.

The overview should contain scope names, memory kinds, counts, and a small recent
activity summary—not complete memory content. Repository-level `AGENTS.md` and
`CLAUDE.md` guidance should tell agents:

- recall at the beginning of relevant work;
- store only durable, verified knowledge;
- use the narrowest correct scope;
- never store credentials or secrets;
- supersede stale facts instead of creating contradictions.

## 11. Security

- Require a generated bearer token for MCP and administrative API requests.
- Store secrets in an uncommitted environment file or Docker secret.
- Do not publish the PostgreSQL port.
- Bind Wayminder to the trusted LAN/VPN interface or place it behind the existing
  private reverse proxy.
- Configure an explicit host allowlist for DNS-rebinding protection.
- Use TLS whenever requests can cross an untrusted network.
- Cap request body size, memory length, recall limit, and request duration.
- Treat all recalled content as untrusted data, not executable instructions.
- Exclude likely secrets at write time using deterministic checks and document
  that these checks are defense-in-depth rather than a guarantee.

## 12. Operations

### Deployment

- Multi-stage Docker build producing a minimal runtime image.
- Docker Compose deployment on a self-hosted Linux server.
- Named Postgres volume stored on local disk.
- Health checks for PostgreSQL, the embedding provider, and Wayminder.
- `restart: unless-stopped` for long-running services.

### Backup

- Scheduled compressed `pg_dump` to a host directory covered by normal backups.
- Periodic restore test into a temporary database.
- Optional Markdown/YAML export for human inspection and Git-based archival.

### Maintenance

A scheduled deterministic maintenance command will:

- identify probable duplicates;
- report contradictory or repeatedly superseded memories;
- prune expired recall traces;
- hard-delete soft-deleted rows after the configured retention period;
- report embedding-model mismatches;
- optionally re-embed records during a controlled model migration.

Automated semantic rewriting is deferred. A future agent-assisted maintenance
mode may propose changes, but every mutation must remain auditable.

## 13. Observability

- Structured request logs without bearer tokens or full memory content.
- Request counts and latency by MCP tool.
- Embedding and database latency measured separately.
- Recall result counts and rank-source diagnostics.
- `/healthz` for liveness and `/readyz` for dependency readiness.
- Optional Prometheus metrics after the core service is stable.

## 14. Implementation phases

### Phase 0 — benchmark and skeleton

- Initialize the Go module and repository conventions.
- Add configuration parsing, logging, health endpoints, and graceful shutdown.
- Benchmark candidate local embedding providers on representative short and long
  texts on the deployment host.
- Lock the initial model and vector dimension.

Exit condition: a documented embedding decision and a containerized Go service
with passing unit tests.

### Phase 1 — durable memory core

- Add PostgreSQL/pgvector Compose service and migrations.
- Implement the embedder interface and selected provider.
- Implement memory storage, soft deletion, supersession, and scope filtering.
- Add semantic and lexical retrieval with deterministic hybrid ranking.
- Add service-level integration tests against live pgvector PostgreSQL.

Exit condition: memories survive restarts and can be accurately recalled through
the Go service.

### Phase 2 — MCP and clients

- Add Streamable HTTP MCP transport.
- Implement `remember`, `recall`, `list_memories`, `supersede`, `forget`, and
  `status`.
- Add bearer authentication and provenance headers.
- Add seed-memory instructions.
- Register and smoke-test Wayminder with Codex and Claude Code.

Exit condition: both clients can safely share scoped memories.

### Phase 3 — deployment hardening

- Add production Compose configuration for the deployment host.
- Remove all default credentials and host-published database ports.
- Configure private networking, host allowlists, backups, and scheduled
  maintenance.
- Add resource limits and failure/recovery tests.

Exit condition: unattended operation on the deployment host with a tested backup and restore
procedure.

### Phase 4 — explainability and portability

- Add bounded recall traces.
- Add explicit memory links where useful.
- Add deterministic health reporting.
- Add Markdown/YAML export and import.
- Evaluate a small read-only browser only if operational use justifies it.

Exit condition: retrieval can be inspected, audited, and exported without direct
database access.

## 15. Initial acceptance criteria

- Codex and Claude Code can connect over authenticated Streamable HTTP.
- A memory written by one client is recallable by the other.
- `repo:x` recall includes `global`, `personal`, and `repo:x`, but excludes
  unrelated repository scopes.
- Semantic paraphrases retrieve the expected memory.
- Exact hostnames and error strings are retrievable through lexical ranking.
- Duplicate writes do not create an uncontrolled pile of records.
- Superseded and forgotten memories do not appear in ordinary recall.
- PostgreSQL is not exposed outside the Compose network.
- No memory content leaves the local environment for embedding.
- Backup and restore are documented and tested.

## 16. Open decisions

1. Direct ONNX inference versus an Ollama embedding endpoint after benchmarking.
2. Exact hybrid-ranking formula and score presentation.
3. Retention duration for soft-deleted memories and recall traces.
4. Whether explicit memory links belong in the first database migration or a
   later migration.
5. Private reverse proxy/TLS arrangement between agent clients and the service.

These decisions do not block repository setup. Phase 0 resolves the embedding
choice with measurements before the database schema fixes a vector dimension.
