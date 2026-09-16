# MCP client integration

Wayminder exposes stateless Streamable HTTP MCP at `/mcp`. Clients send a
bearer token on every request. `/healthz` reports process liveness and version;
`/readyz` checks both PostgreSQL and the embedding provider.

## Codex

Place the token in the environment that launches Codex:

```sh
export WAYMINDER_TOKEN='<one-time token from the client registry>'
export WAYMINDER_SOURCE='workstation'
```

Add this to `~/.codex/config.toml`:

```toml
[mcp_servers.wayminder]
url = "https://wayminder.example.com/mcp"
bearer_token_env_var = "WAYMINDER_TOKEN"
env_http_headers = { "X-Wayminder-Source" = "WAYMINDER_SOURCE" }
default_tools_approval_mode = "writes"
```

Restart the Codex host after changing MCP configuration. `codex mcp list` and
the `/mcp` command show whether the server initialized. The `writes` approval
mode preserves a client confirmation boundary for tools that are not marked
read-only. See the [official Codex MCP
documentation](https://developers.openai.com/codex/mcp/) for current client
options.

Do not put the token in `http_headers`, a project repository, or shell history.
`bearer_token_env_var` names the environment variable; it does not contain the
secret itself.

## Generic Streamable HTTP clients

Configure the endpoint as `https://wayminder.example.com/mcp` and send:

```text
Authorization: Bearer <registered client token>
X-Wayminder-Source: <optional non-secret provenance label>
```

The client should preserve the bearer header across MCP initialization and tool
calls, reject unexpected redirects, and reconnect after credential rotation or
server restart. The MCP client name is used as fallback provenance only when no
registered client identity exists.

## Tools

| Tool | Effect | Use |
| --- | --- | --- |
| `recall` | Read-only | Hybrid semantic and lexical search across visible scopes |
| `list_memories` | Read-only | Page through recent live memories |
| `status` | Read-only | Report model information and live memory counts |
| `remember` | Mutating | Store verified durable knowledge or deduplicate it |
| `supersede` | Destructive | Replace stale knowledge while preserving history |
| `forget` | Destructive | Soft-delete a memory from normal recall |

Recall before relying on an infrastructure or repository assumption. Remember
only facts, decisions, preferences, procedures, and references that will remain
useful across sessions. Do not store credentials, transient logs, task status,
or speculation.

## Through Switchboard

Switchboard can federate Wayminder as a remote MCP capability. Configure a
dedicated registered Wayminder client for the gateway, reference its token from
the Switchboard environment, and keep Wayminder on a private route reachable
only from the gateway and operators.

Switchboard prefixes discovered tool names with the capability name when
needed, applies its profile and exact-tool policy, and preserves Wayminder's
tool schemas and annotations. Wayminder still owns memory validation,
deduplication, scope behavior, storage, and audit history.

The Wayminder audit identifies the dedicated gateway credential, not the human
behind a Switchboard OAuth session. `X-Wayminder-Source` may add a non-
authoritative source label, but it is not delegated identity. Use direct,
separate Wayminder instances when per-user memory isolation or provenance is a
hard requirement.
