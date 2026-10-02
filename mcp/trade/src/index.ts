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

/**
 * Eight gateway prefixes, four API processes (TD-55): Traefik strips `/api/<id>` and the
 * Service behind each prefix selects the Deployment named in `process`. `docs` and `ops`
 * answer from api-monitor; `trading`, `strategy` and `portfolio` from api-account. A
 * prefix is an alias, not a boundary — three healthy prefixes may be one healthy process.
 */
const DOMAINS = [
  { id: 'monitor', probe: '/status', process: 'api-monitor' },
  { id: 'docs', probe: '/health', process: 'api-monitor' },
  { id: 'ops', probe: '/health', process: 'api-monitor' },
  { id: 'trading', probe: '/health', process: 'api-account' },
  { id: 'strategy', probe: '/health', process: 'api-account' },
  { id: 'portfolio', probe: '/health', process: 'api-account' },
  { id: 'market', probe: '/health', process: 'api-market' },
  { id: 'research', probe: '/health', process: 'api-research' },
] as const

const PROCESSES = [...new Set(DOMAINS.map((d) => d.process))]

const server = new McpServer({ name: SERVER_NAME, version: SERVER_VERSION })

server.tool('trade_mcp_health', 'MCP server health — read-only mode enforced', {}, async () =>
  jsonResult({
    ok: true,
    server: SERVER_NAME,
    version: SERVER_VERSION,
    mode: 'read_only',
    gateway: process.env.TRADE_API_GATEWAY ?? 'http://127.0.0.1:80',
    gatewayHost: process.env.TRADE_API_GATEWAY_HOST ?? '',
  }),
)

server.tool('trade_mcp_capabilities', 'List read-only Trade domain probe tools', {}, async () =>
  jsonResult({
    server: SERVER_NAME,
    mode: 'read_only',
    domains: DOMAINS,
    forbidden: ['POST', 'PUT', 'DELETE', 'ib:operator:cmd', 'Redis daemon control write'],
  }),
)

server.tool(
  'list_trade_domains',
  'Eight Trade API gateway prefixes with probe paths and the four API processes that answer them',
  {},
  async () => jsonResult({ domains: DOMAINS, count: DOMAINS.length, processes: PROCESSES }),
)

for (const d of DOMAINS) {
  server.tool(
    `get_${d.id}_health`,
    `GET /api/${d.id}${d.probe} — read-only health probe (answered by ${d.process})`,
    {},
    async () => jsonResult(await tradeGet(`/api/${d.id}${d.probe}`)),
  )
}

server.tool(
  'get_trade_api',
  'Generic read-only GET to Trade API (path must start with /api/)',
  { path: z.string().describe('Path e.g. /api/strategy/strategies/allocations or /api/monitor/status') },
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
