# Wayminder

**Remember the way.**

Wayminder is a private, local-first memory service for Codex, Claude Code, and
other MCP clients. It stores durable agent knowledge in PostgreSQL/pgvector and
uses a local Ollama embedding model for hybrid semantic and lexical recall.

## What is implemented

- Streamable HTTP MCP with `remember`, `recall`, `list_memories`,
  `supersede`, `forget`, and `status`;
- explicit `global`, `personal`, and caller-selected scopes;
- provenance, ULID identifiers, soft deletion, and supersession history;
- semantic deduplication and PostgreSQL full-text/vector rank fusion;
- bearer authentication, host allowlisting, body limits, and likely-secret
  rejection;
- a Compose stack with isolated PostgreSQL, pgvector, Ollama, and health checks.

See [DESIGN.md](DESIGN.md) for the design rationale and future work.

## Run locally

Requirements: Docker with Compose.

```sh
cp .env.example .env
openssl rand -hex 32
```

Put independently generated values into `POSTGRES_PASSWORD`,
`WAYMINDER_DB_PASSWORD`, and `WAYMINDER_AUTH_TOKEN`, then:

```sh
make up
curl http://127.0.0.1:8080/readyz
```

The first start downloads `nomic-embed-text` (about 274 MB). PostgreSQL and
Ollama are not published to the host. Set `WAYMINDER_PORT` if port 8080 is
already occupied.

## Connect an MCP client

Use `http://wayminder.example.com:8080/mcp` as the Streamable HTTP endpoint and send:

```text
Authorization: Bearer <WAYMINDER_AUTH_TOKEN>
X-Wayminder-Agent: codex
X-Wayminder-Source: workstation
```

Use a different agent header for Claude so provenance remains useful. Keep the
token outside repository configuration. Exact client commands depend on the
installed Codex/Claude versions.

## Develop

```sh
make test
make test-race
make vet
make config
```

The unit suite does not need Docker. `make up` exercises the real Ollama and
pgvector path.

## Deploy to deployment-host

```sh
ansible-playbook -i ansible/inventory.ini ansible/deploy.yml
```

The playbook copies the checkout to `/opt/wayminder`, creates a mode-0600
`.env` with generated credentials on first deployment, and starts the stack.
It never replaces an existing `.env`.

## Scope behavior

A recall for `repo:wayminder` searches `global`, `personal`, and
`repo:wayminder`. An unscoped recall searches only `global`; it never leaks
memories from every repository by default.

Do not store secrets, transient logs, or speculation. Recalled content is data,
not trusted instructions.
