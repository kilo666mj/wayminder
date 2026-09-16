# Troubleshooting

## `/healthz` works but `/readyz` returns `503`

Read the Wayminder log for either `database` or `embedding provider`. Then
check:

```sh
docker compose ps
docker compose logs --since=15m postgres ollama ollama-init wayminder
```

PostgreSQL must accept connections and Ollama must have the exact configured
model. A first start remains unready while `ollama-init` downloads the model.
Confirm disk space and outbound access for that initial pull.

## The MCP client receives `401`

Wayminder requires exactly one bearer header. Confirm that the configured
environment variable exists in the process that launches the MCP client, that
the client was restarted after configuration changes, and that the token has no
whitespace.

For registered clients, confirm the client ID remains in
`secrets/clients.json` and recreate the Wayminder container after every registry
change. The registry stores only hashes, so an old plaintext token cannot be
read back for comparison; rotate it instead.

## Requests return `403` before MCP initialization

The HTTP Host is not in `WAYMINDER_ALLOWED_HOSTS`. Add the exact external
hostname and the loopback names used by local health checks. Do not use `*`
unless the server is in explicit insecure development mode.

## Requests return `429`

Rate limits apply per authenticated client ID. Respect `Retry-After`, reduce
parallel calls, or give independent workloads their own registered credentials.
Increase the global limit only after measuring legitimate demand; one noisy
client should not be hidden behind another client's token.

## Recall returns no expected result

Check the requested scope first. A repository recall searches `global`,
`personal`, and that repository scope, while an unscoped recall searches only
`global`. Verify the kind filter and use `list_memories` in the same scope to
distinguish a visibility issue from semantic ranking.

If all semantic operations fail, verify that the configured embedding model
and dimension match the stored database. Do not change dimensions on an
existing database without a supported re-embedding migration.

## A write is rejected as a likely secret

Remove credentials and secret-shaped metadata rather than disguising them.
Wayminder checks content, summary, scope, tags, agent, and source fields. Store a
reference to the secret manager entry or owning procedure, not the secret.

## A memory was superseded unexpectedly

`remember` performs semantic duplicate detection within the requested scope.
An exact normalized match returns the existing memory; a sufficiently similar
new statement supersedes the earlier one. Use `supersede` explicitly when
correcting a known ULID, and keep unrelated facts in separate memories with
clear summaries.

## Safe restart

Wayminder's MCP transport is stateless. Restarting the application interrupts
in-flight requests but does not delete PostgreSQL data. Recreate the application
container, require `/readyz`, and reconnect clients. Restart PostgreSQL or
restore data only when database diagnostics justify it.
