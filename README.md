# Wayminder

**Remember the way.**

Wayminder is a private, local-first memory service for AI coding agents. It gives
Codex, Claude Code, and other MCP clients a shared place to record durable facts,
recall related knowledge semantically, and correct or retire stale information.

The service is planned as a small Go application backed by PostgreSQL and
pgvector, with embeddings generated locally. See [DESIGN.md](DESIGN.md) for the
architecture and implementation plan.

