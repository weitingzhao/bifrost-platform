# bifrost-platform MCP

Model Context Protocol bridge for Cursor / Agent — **same contract as platform-api**.

## mcp-server-platform (P5 — available)

Stdio MCP server that proxies `http://127.0.0.1:8780/api/v1/*` with Bearer token auth.

```bash
cd mcp/platform
npm install
PLATFORM_OPERATOR_TOKEN=platform-operator-dev npm start
```

Cursor config snippet: **Ops Console → Architecture → MCP Contract → Copy Cursor config**

Or:

```json
{
  "mcpServers": {
    "bifrost-platform": {
      "command": "npx",
      "args": ["tsx", "/path/to/bifrost-platform/mcp/platform/src/index.ts"],
      "env": {
        "PLATFORM_API_URL": "http://127.0.0.1:8780",
        "PLATFORM_OPERATOR_TOKEN": "platform-operator-dev"
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
- No `daemon_control` writes
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
