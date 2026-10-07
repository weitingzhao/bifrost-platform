# bifrost-platform MCP

Model Context Protocol bridge for Cursor / Agent — **same contract as platform-api**.

## mcp-server-platform (P5 — available)

Stdio MCP server that proxies `http://127.0.0.1:8780/api/v1/*` with Bearer token auth.

```bash
cd mcp/platform
npm install
npm start
```

Token: set `PLATFORM_OPERATOR_TOKEN` in the environment, or leave it unset and — when
`PLATFORM_API_URL` is loopback — the server reads it from `bifrost-platform/.env`
(`PLATFORM_TOKEN_ENV_KEY` picks another key, e.g. `PLATFORM_VIEWER_TOKEN` for read-only bridges).
When `PLATFORM_TOKEN_ENV_KEY` is set, only that key is read — from the environment, then `.env` —
so an exported `PLATFORM_OPERATOR_TOKEN` cannot widen a read-only bridge.
There are no default tokens; the old `platform-*-dev` values no longer authenticate.

`MCP_BRIDGE_FOCUS` must be empty (full server) or one of `kubernetes`, `redis`, `postgres`,
`prometheus`; any other value makes the server exit 1 instead of registering every tool.

Cursor config snippet: **Ops Console → Architecture → MCP Contract → Copy Cursor config**

Or:

```json
{
  "mcpServers": {
    "bifrost-platform": {
      "command": "npx",
      "args": ["tsx", "/path/to/bifrost-platform/mcp/platform/src/index.ts"],
      "env": {
        "PLATFORM_API_URL": "http://127.0.0.1:8780"
      }
    }
  }
}
```

## API catalog (Console + Agent parity)

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/mcp/tools` | Tool list with permission levels |
| `GET /api/v1/mcp/status` | Server path + Cursor hints |

## Forbidden (deny-list)

See Ops Console → Architecture → MCP Contract.

- No `ib:operator:cmd` writes
- No Redis daemon control stream writes (`bifrost:daemon:*:control`)
- No Monitor `POST /api/monitor/control/*`
- No direct trade order placement

## Future servers

`mcp-server-kubernetes`, `mcp-server-redis`, etc. — see `console/src/lib/standards/mcpContractCatalog.ts`

## mcp-server-trade (V4 — available)

Read-only Trade API proxy — 9 domains via nginx gateway.

```bash
cd mcp/trade
npm install
TRADE_API_GATEWAY=http://192.168.10.73:30880 npm start
```

Cursor config: `config/cursor-mcp-trade.json`

| Endpoint | Purpose |
|----------|---------|
| `GET /api/v1/trade-agent/domains` | Nine Trade API domains |
| `GET /api/v1/trade-agent/catalog` | Read-only MCP tool catalog |
