# AGENTS.md - Wayminder

Run `make test`, `make test-race`, and `make vet` after Go changes.

Keep memory behavior in `internal/memory` independent of MCP and PostgreSQL.
All recall paths must enforce effective scopes. All writes must remain
versioned or soft-deleted, and no credential may be committed or logged.

Use the official MCP Go SDK and pgx/pgvector adapters. Keep the MCP server
stateless and compatible with Streamable HTTP clients.
