# Authentication and authorization

Wayminder authenticates the calling MCP client. It does not implement separate
per-user memory permissions: every authenticated client can use the same memory
set and the same scope behavior. Deploy a separate Wayminder instance when
callers must not see one another's durable knowledge.

## Preferred production model

Production deployments use `WAYMINDER_CLIENTS_FILE` with one independently
generated credential per client. The registry stores only this shape:

```json
{"clients":[{"id":"codex-workstation","token_sha256":"<64 lowercase hex characters>"}]}
```

Use the registry tool instead of writing that file by hand:

```sh
./scripts/wayminder-client-registry rotate codex-workstation
```

The command prints a 256-bit token exactly once, writes only its SHA-256 hash,
uses an atomic replacement, and restricts the file mode. Install the printed
token immediately in the named client. If the command runs as root, the new
registry is owned by the unprivileged container identity (`65532:65532`).

Set:

```dotenv
WAYMINDER_AUTH_TOKEN=
WAYMINDER_CLIENTS_FILE=/run/secrets/wayminder-clients.json
```

The Compose stack mounts the local `secrets/` directory read-only at
`/run/secrets`. Wayminder reads the registry during startup, so force-recreate
the application container after changing the file.

## Credential behavior

- The client ID associated with the bearer token is the authoritative
  `author_agent` for new mutations.
- `X-Wayminder-Agent` is considered only for legacy or explicitly insecure
  clients that have no registered identity. A registered caller cannot forge a
  different agent with that header.
- `X-Wayminder-Source` is an optional provenance label. It is stored after
  normalization but grants no permissions.
- Rate limiting is keyed by authenticated client ID, so one client's traffic
  does not consume another client's bucket.
- The server requires exactly one `Authorization: Bearer <token>` header and
  compares token hashes in constant time.

Scopes such as `personal` and `repo:wayminder` select recall context; they are
not ACLs. A recall for a repository scope intentionally searches `global`,
`personal`, and that requested scope.

## Shared and insecure modes

`WAYMINDER_AUTH_TOKEN` provides one shared identity named `legacy`. Keep it for
local development or a controlled migration only; it cannot distinguish
clients for provenance, revocation, or rate limiting.

`WAYMINDER_ALLOW_INSECURE=true` permits an unauthenticated server only when no
credentials are configured. It also permits a wildcard host allowlist. Do not
use this mode on a shared host, behind an externally reachable proxy, or in
production.

## Host and network boundary

`WAYMINDER_ALLOWED_HOSTS` rejects requests whose HTTP Host is not explicitly
listed. Include the public hostname and any loopback hostname used by health
checks. This protects routing mistakes but does not provide TLS or caller
authentication.

Bind the container port to loopback when an HTTPS reverse proxy runs on the
same host. Terminate TLS at that trusted proxy, restrict direct origin access,
and apply suitable connection and request limits. PostgreSQL and Ollama remain
on the internal Compose network and must not be published.

## Rotate and revoke

Rotate a client after suspected exposure or as part of a scheduled credential
change:

```sh
cd /opt/wayminder
./scripts/wayminder-client-registry rotate codex-workstation
docker compose up -d --force-recreate wayminder
```

Install the newly printed value in the client before removing its old runtime
configuration. The registry replacement makes the previous value invalid after
the container restart.

Revoke a retired client without replacing it:

```sh
./scripts/wayminder-client-registry revoke codex-workstation
docker compose up -d --force-recreate wayminder
```

The tool refuses to remove the final registered client. No plaintext token can
be recovered from the registry; rotate or create a client when its token is
lost.
