# CLAUDE.md - Wayminder

Wayminder is a self-hosted, local-first MCP memory service for Codex and Claude Code.
It is a Go application backed by PostgreSQL/pgvector with local Ollama
embeddings.

## Commands

- `make test` - unit tests
- `make test-race` - race-enabled tests
- `make vet` - static checks
- `make config` - validate Compose after creating `.env`
- `make up` - build and start the complete local stack
- `make maintain` / `make probe` - offline reports against the running stack

## Architecture

- `cmd/wayminder` - executable and lifecycle; `serve` (default), `maintain`,
  and `probe` subcommands
- `internal/memory` - transport-independent validation and memory behavior,
  including the read-only maintenance report
- `internal/probe` - offline recall evaluation with generated questions
- `internal/store` - pgx/pgvector persistence and embedded migrations
- `internal/embed` - embedding provider interface and Ollama adapter
- `internal/server` - MCP tools, authentication, host validation, health
- `compose.yaml` - isolated Postgres, Ollama, model initialization, service

The embedding dimension is fixed by the initial migration. A model or dimension
change requires a migration and complete re-embedding. Do not store or log
memory content, bearer tokens, or database passwords.

`maintain` and `probe` never mutate memories and stay out of the request path.
Their reports omit memory text unless the operator passes `-content`.
