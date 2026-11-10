# bifrost-platform MCP

Model Context Protocol bridge for Cursor / Agent — **same contract as platform-api**.

## mcp-server-platform (P5 — available)

Stdio MCP server that proxies platform-api with Bearer token auth.
The shipped MCP config points platform servers at PROD `http://192.168.10.100:30876`.
`MCP_WRITES=off` (the default) returns `writes not cut over` and does not call write
routes. `MCP_WRITES=on` calls B-tier routes directly and turns C/D routes into
`POST /api/v1/approvals` (the response id is the approval). `MCP_BRIDGE_FOCUS=local`
is the laptop bdev/git-bridge server. `MCP_BRIDGE_FOCUS=approve` is the chat
approval server (`channel` is always `chat`).

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
        "PLATFORM_API_URL": "http://192.168.10.100:30876",
        "MCP_WRITES": "off"
      }
    }
  }
}
```

## Waves (M-368)

The coordinator's wave tools in `mcp/platform` batch work for one repository.
Only `bifrost-platform` supports waves for now.

- `open_wave` takes `repo` and `plan_root` (for example, `R6`). It opens a wave
  on the repository's main head and returns its `id`, `n`, and `base`.
- In `propose_tasks`, set each task's optional `wave` field to that wave id.
  These tasks start from main as usual. Once accepted, they are held out of
  landing while the wave is open.
- `list_waves` takes an optional `repo` filter; omit it to list all waves.
  It returns `id`, `repo`, `plan_root`, `n`, `base`, and `state`.
- `close_wave` takes `id` and `action`, either `close` or `abandon`.
  With `close`, the held members are released together and the batch landing
  lands them once: one landing CI, one card, one push to main, one STG
  release, and one PROD release. Close is refused while any member is not yet
  accepted. If the landing CI is red, the blamed member goes back for rework
  and the others stay held until it is accepted again.
  With `abandon`, nothing lands.
- Tasks in one wave cannot build on each other's unlanded work. Put dependent
  work in the next wave.

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

`mcp-server-kubernetes`, `mcp-server-redis`, etc.

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
