#!/usr/bin/env node
/**
 * Bifrost Trade MCP server (Vision V4) — read-only GET proxy to Trade API gateway.
 */
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { z } from 'zod'
import { jsonResult, tradeGet } from './tradeClient.js'

const SERVER_NAME = 'mcp-server-trade'
const SERVER_VERSION = '0.1.0'

const DOMAINS = [
  { id: 'monitor', probe: '/status' },
  { id: 'massive', probe: '/health' },
  { id: 'docs', probe: '/health' },
  { id: 'ops', probe: '/health' },
  { id: 'trading', probe: '/health' },
  { id: 'strategy', probe: '/health' },
  { id: 'portfolio', probe: '/health' },
  { id: 'market', probe: '/health' },
  { id: 'research', probe: '/health' },
] as const

const server = new McpServer({ name: SERVER_NAME, version: SERVER_VERSION })

server.tool('trade_mcp_health', 'MCP server health — read-only mode enforced', {}, async () =>
  jsonResult({
    ok: true,
    server: SERVER_NAME,
    version: SERVER_VERSION,
    mode: 'read_only',
    gateway: process.env.TRADE_API_GATEWAY ?? 'http://127.0.0.1:30880',
  }),
)

server.tool('trade_mcp_capabilities', 'List read-only Trade domain probe tools', {}, async () =>
  jsonResult({
    server: SERVER_NAME,
    mode: 'read_only',
    domains: DOMAINS,
    forbidden: ['POST', 'PUT', 'DELETE', 'ib:operator:cmd', 'daemon_control write'],
  }),
)

server.tool('list_trade_domains', 'Nine Trade API domains with probe paths', {}, async () =>
  jsonResult({ domains: DOMAINS, count: DOMAINS.length }),
)

for (const d of DOMAINS) {
  server.tool(
    `get_${d.id}_health`,
    `GET /api/${d.id}${d.probe} — read-only health probe`,
    {},
    async () => jsonResult(await tradeGet(`/api/${d.id}${d.probe}`)),
  )
}

server.tool(
  'get_trade_api',
  'Generic read-only GET to Trade API (path must start with /api/)',
  { path: z.string().describe('Path e.g. /api/trading/positions') },
  async ({ path }) => {
    if (!path.startsWith('/api/')) {
      throw new Error('path must start with /api/')
    }
    if (path.includes('/control/')) {
      throw new Error('forbidden: control write paths')
    }
    return jsonResult(await tradeGet(path))
  },
)

async function main() {
  const transport = new StdioServerTransport()
  await server.connect(transport)
}

main().catch(err => {
  console.error(err)
  process.exit(1)
})
