# bifrost-platform MCP (Phase P2)

Model Context Protocol tools for Cursor / Ollama agents.

## Planned tools

| Tool | Backend | Level |
|------|---------|-------|
| `get_connectivity_matrix` | `GET /api/v1/matrix` | L0 |
| `list_environments` | `GET /api/v1/environments` | L0 |
| `get_platform_health` | `GET /health` | L0 |

## Forbidden tools (by policy)

- Any direct `daemon_control` write
- Any `ib:operator:cmd` RPC
- Unauthenticated trade stack mutations

## Phase 0

Not implemented. Use HTTP against platform-api (`:8780`) manually or via scripts.

Implementation target: TypeScript or Go MCP server in this directory.
