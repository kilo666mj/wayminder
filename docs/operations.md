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

## Maintenance report

`wayminder maintain` reads the database and prints a JSON report of
consolidation candidates. It changes nothing.

```sh
make maintain > maintain.json
make maintain ARGS="-threshold 0.9 -min-revisions 4 -content" > maintain.json
```

The report contains:

- `duplicate_clusters`: groups of live memories whose embeddings are at least
  `-threshold` similar (default `0.85`) and that share a scope, or where one
  side is in `global` or `personal`;
- `supersede_chains`: live memories with at least `-min-revisions` earlier
  versions (default `3`);
- `embedding_model_mismatches`: live memory counts for any model other than
  `WAYMINDER_EMBED_MODEL`.

Resolve a finding with the MCP `supersede` or `forget` tool so that the change
is audited. `pairs_truncated: true` means more pairs exist than `-max-pairs`;
raise the threshold or the limit. The duplicate comparison is quadratic in the
number of live memories, so run it off-peak on a large store.

By default the report identifies memories only by ID, scope, kind, author, and
creation time. `-content` adds summaries and content: treat that output like a
database dump, and do not send it to container logs or a shared channel.

## Recall evaluation

`wayminder probe` samples live memories, asks a generative model for questions
each one should answer, and measures whether recall returns the memory.

The generative model is separate from the embedding model and is not pulled by
the stack. Pull one into the Ollama service once, then name it:

```sh
docker compose exec ollama ollama pull <generative-model>
make probe ARGS="-model <generative-model>" > probe.json
```

Set `WAYMINDER_PROBE_MODEL` in `.env` to avoid repeating the flag. To use a
different Ollama host for generation, set `WAYMINDER_PROBE_OLLAMA_URL` or pass
`-ollama-url`; memory content is sent to that host, so use only one you would
trust with a database dump. Embeddings always use the stack's own Ollama.

Useful flags: `-memories` (sample size, default `50`), `-questions` (per
memory, default `3`), `-k` (recall limit, default `5`, the limit agents get
when they pass none), `-scope` (one exact scope), `-seed` (the same seed
samples the same memories), and `-timeout` (per memory, default `2m`).

Read the report as a comparison, not an absolute score: generated questions
are easier than real ones. Keep the seed and model fixed and compare `overall`
before and after a ranking or content change. `weak` lists memories that at
least one of their own questions failed to return, worst first; they are
candidates for a clearer summary or a rewrite. `generation_failures` counts
memories the model produced no usable questions for. `-content` adds summaries
and the generated questions, with the same handling caution as above.

## Credential loss

If one client loses its token, rotate that client and recreate the container.
If all client-side tokens are lost but the registry remains, add a new client
with the registry tool, install its one-time token, verify access, and then
revoke or rotate the unreachable entries. If the registry itself is lost,
create a new registry with a new client and treat every former token as
revoked.
