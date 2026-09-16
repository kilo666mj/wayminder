# Operations and recovery

## Routine checks

Use the HTTP endpoints from the same path as the reverse proxy:

```sh
curl -fsS https://wayminder.example.com/healthz
curl -fsS https://wayminder.example.com/readyz
```

`/healthz` proves that the process can answer HTTP. `/readyz` returns `503`
unless both PostgreSQL and Ollama are reachable. The MCP `status` tool adds the
embedding model, dimension, live count, deleted count, superseded count, and
counts by scope and kind.

For the Compose deployment:

```sh
docker compose ps
docker compose logs --since=30m wayminder postgres ollama
```

Keep the reverse proxy, container runtime, disk use, database connections, and
Ollama model availability in normal host monitoring. Do not expose PostgreSQL,
Ollama, or the application's backend port to untrusted networks.

## Upgrade

1. Read the release notes and preserve the current checkout revision.
2. Back up PostgreSQL, `.env`, and `secrets/clients.json`.
3. Run `make test`, `make test-race`, `make vet`, and `make config` on the new
   source.
4. Rebuild and recreate the application:

   ```sh
   docker compose build wayminder
   docker compose up -d --force-recreate wayminder
   ```

5. Require `/readyz` to return `200` and call the MCP `status` tool from a real
   client.

Wayminder applies ordered SQL migrations at startup. Do not manually mark a
migration complete. Keep `WAYMINDER_EMBED_MODEL` and
`WAYMINDER_EMBED_DIMENSION` unchanged across a routine upgrade; changing the
vector model or dimension requires an explicit re-embedding migration that the
current service does not provide.

## Back up

PostgreSQL is the durable record for memories, supersession history, and the
mutation audit. Create a logical backup with a database superuser:

```sh
docker compose exec -T postgres \
  pg_dump -U postgres -d wayminder -Fc > wayminder.dump
```

Protect the dump as sensitive operational data. Back up `.env` and the hash-
only client registry separately with their restrictive permissions. A registry
backup cannot recover plaintext client tokens; those remain only on clients.

The `ollama_data` volume is a model cache and can be recreated by pulling the
configured model. It is not a substitute for the PostgreSQL backup.

## Restore

Restore into a compatible PostgreSQL/pgvector deployment using the same
embedding model and dimension:

```sh
docker compose stop wayminder
docker compose exec -T postgres \
  pg_restore --clean --if-exists -U postgres -d wayminder < wayminder.dump
docker compose up -d wayminder
```

Then require `/readyz`, inspect container logs for migration errors, and compare
the MCP `status` counts with the backup record. Test one scoped recall and one
write using a disposable, non-secret fact before returning the service to
clients.

Coordinate the client registry with the restored database snapshot. Restoring
an older registry can re-enable a credential that had since been revoked;
restoring a newer registry can invalidate clients that still hold older
tokens. Rotate affected clients after recovery.

## Credential loss

If one client loses its token, rotate that client and recreate the container.
If all client-side tokens are lost but the registry remains, add a new client
with the registry tool, install its one-time token, verify access, and then
revoke or rotate the unreachable entries. If the registry itself is lost,
create a new registry with a new client and treat every former token as
revoked.
