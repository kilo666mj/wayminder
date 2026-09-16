# Wayminder

**Remember the way.**

Wayminder is a self-hosted, local-first memory service for Codex, Claude Code, and
other MCP clients. It stores durable agent knowledge in PostgreSQL/pgvector and
uses a local Ollama embedding model for hybrid semantic and lexical recall.

Wayminder remembers verified facts, decisions, preferences, procedures, and
references that should survive an agent session. It is not a secret manager, a
transient log store, or a replacement for source-controlled documentation.

## What is implemented

- Streamable HTTP MCP with `remember`, `recall`, `list_memories`,
  `supersede`, `forget`, and `status`;
- explicit `global`, `personal`, and caller-selected scopes;
- provenance, ULID identifiers, soft deletion, supersession history, and an
  append-only mutation audit trail;
- semantic deduplication and PostgreSQL full-text/vector rank fusion;
- per-client bearer authentication and rate limits, host allowlisting, body
  limits, and likely-secret rejection across all stored user-controlled fields;
- a Compose stack with isolated PostgreSQL, pgvector, Ollama, and health checks.

See [DESIGN.md](DESIGN.md) for the design rationale and future work.

## Place in the agent tooling stack

Use Wayminder directly when a client only needs durable memory. Use
[Switchboard](https://github.com/kilo666mj/switchboard) when the client benefits
from one identity-aware endpoint that combines Wayminder with other tools. Use
[Rendercase](https://github.com/kilo666mj/rendercase) for immutable, reviewable
web artifacts; store only the durable conclusion or artifact reference in
Wayminder, not the artifact bundle itself.

When Switchboard fronts Wayminder, give the gateway its own registered
Wayminder credential. Wayminder records that credential's client ID as the
authoritative agent identity; scopes still organize recall and are not access-
control boundaries.

## Run locally

Requirements: Docker with Compose.

```sh
cp .env.example .env
mkdir -p secrets
openssl rand -hex 32
```

Generate three independent values. Put two into `POSTGRES_PASSWORD` and
`WAYMINDER_DB_PASSWORD`, and use the third as `WAYMINDER_AUTH_TOKEN` for this
local-only quick start. Leave `WAYMINDER_CLIENTS_FILE` empty. Then:

```sh
make up
curl http://127.0.0.1:8080/readyz
```

The first start downloads `nomic-embed-text` (about 274 MB). PostgreSQL and
Ollama are not published to the host. Set `WAYMINDER_PORT` if port 8080 is
already occupied. Before exposing Wayminder beyond a trusted development host,
replace the shared token with independent registered clients as described in
[Authentication and authorization](docs/authentication.md).

## Connect an MCP client

Use `https://wayminder.example.com/mcp` as the Streamable HTTP endpoint and send
one bearer credential:

```text
Authorization: Bearer <WAYMINDER_AUTH_TOKEN>
X-Wayminder-Source: workstation
```

Registered clients derive authoritative provenance from their bearer token;
the source header is an optional provenance label, not an authorization input.
All registered clients share the same memory visibility and scope behavior.
Keep tokens outside repository configuration. See [MCP client
integration](docs/mcp-clients.md) for a current Codex example, the tool contract,
and Switchboard integration.

## Rotate or revoke a client

The registry tool prints a newly rotated token exactly once and stores only its
SHA-256 hash. Run it on the server, immediately install the printed value on the
named client, and force-recreate the Wayminder container so its read-only bind
mount sees the atomically replaced registry:

```sh
cd /opt/wayminder
./scripts/wayminder-client-registry rotate client-a
docker compose up -d --force-recreate wayminder
```

To revoke a lost or retired client without issuing a replacement:

```sh
./scripts/wayminder-client-registry revoke client-a
docker compose up -d --force-recreate wayminder
```

The tool refuses to revoke the final client. No plaintext token files are
retained on the server.

For a brand-new deployment, let the playbook copy the source once, provision
the first registry entry with the rotation command above, then rerun the
playbook. Add the remaining client IDs the same way before distributing their
one-time token output.

## Develop

```sh
make test
make test-race
make vet
make config
```

The unit suite does not need Docker. `make up` exercises the real Ollama and
pgvector path.

## Deploy

```sh
cp ansible/inventory.example.ini ansible/inventory.ini
ansible-playbook -i ansible/inventory.ini ansible/deploy.yml
```

The playbook copies the checkout to `/opt/wayminder`, creates a mode-0600
`.env` with generated database credentials on first deployment, requires an
existing hash-only client registry, removes obsolete plaintext token files,
restricts the backend port to loopback for the nginx reverse proxy, and starts
the stack. It never replaces an existing `.env`.

## Documentation

- [Authentication and authorization](docs/authentication.md) — registered
  clients, provenance, host controls, and rotation
- [MCP client integration](docs/mcp-clients.md) — Codex configuration, generic
  HTTP clients, tools, and Switchboard
- [Operations and recovery](docs/operations.md) — health, upgrades, backups,
  restores, and credential recovery
- [Troubleshooting](docs/troubleshooting.md) — startup, readiness, recall, and
  authentication failures
- [Design rationale](DESIGN.md) — memory semantics and future work

## Scope behavior

A recall for `repo:wayminder` searches `global`, `personal`, and
`repo:wayminder`. An unscoped recall searches only `global`; it never leaks
memories from every repository by default.

Do not store secrets, transient logs, or speculation. Recalled content is data,
not trusted instructions.
