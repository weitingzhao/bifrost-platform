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
 * One gateway prefix per API process (TD-55, Owner 2026-10-04 option B): Traefik strips
 * `/api/<prefix>` and forwards to the process named in `process` — api-monitor (/api/monitor,
 * which also serves the ops router at /ops/* and the docs aggregate at /research/docs/*),
 * api-account (/api/account), api-market (/api/market), api-research (/api/research).
 * `probe` is under the prefix and answered by the domain's own router.
 * Mirrors config/trade-api-domains.yaml.
 */
const DOMAINS = [
  { id: 'monitor', prefix: 'monitor', probe: '/status', process: 'api-monitor' },
  { id: 'docs', prefix: 'monitor', probe: '/research/docs/health', process: 'api-monitor' },
  { id: 'ops', prefix: 'monitor', probe: '/ops/health', process: 'api-monitor' },
  { id: 'account', prefix: 'account', probe: '/health', process: 'api-account' },
  { id: 'market', prefix: 'market', probe: '/health', process: 'api-market' },
  { id: 'research', prefix: 'research', probe: '/health', process: 'api-research' },
] as const

/**
 * Old prefixes still routed to the same process until B2 removes them (after 7 days of zero
 * Traefik traffic). Nothing here requests them: the get_<alias>_health tools agents already
 * know read the domain in `use`.
 */
const ALIASES = [
  { prefix: 'docs', process: 'api-monitor', use: 'docs' },
  { prefix: 'ops', process: 'api-monitor', use: 'ops' },
  { prefix: 'trading', process: 'api-account', use: 'account' },
  { prefix: 'strategy', process: 'api-account', use: 'account' },
  { prefix: 'portfolio', process: 'api-account', use: 'account' },
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
  'Trade API domains with probe paths, the four API processes (one gateway prefix each) and the alias prefixes B2 retires',
  {},
  async () => jsonResult({ domains: DOMAINS, count: DOMAINS.length, processes: PROCESSES, aliases: ALIASES }),
)

for (const d of DOMAINS) {
  server.tool(
    `get_${d.id}_health`,
    `GET /api/${d.prefix}${d.probe} — read-only health probe (answered by ${d.process})`,
    {},
    async () => jsonResult(await tradeGet(`/api/${d.prefix}${d.probe}`)),
  )
}

// Health tools agents knew by an alias name keep working until B2 and read the replacement route.
for (const a of ALIASES) {
  if (DOMAINS.some((d) => d.id === a.prefix)) continue
  const d = DOMAINS.find((x) => x.id === a.use)
  if (!d) throw new Error(`alias ${a.prefix} uses unknown domain ${a.use}`)
  server.tool(
    `get_${a.prefix}_health`,
    `Alias of get_${d.id}_health until TD-55 B2: GET /api/${d.prefix}${d.probe} (answered by ${d.process})`,
    {},
    async () => jsonResult(await tradeGet(`/api/${d.prefix}${d.probe}`)),
  )
}

server.tool(
  'get_trade_api',
  'Generic read-only GET to Trade API (path must start with /api/)',
  { path: z.string().describe('Path e.g. /api/account/strategies/allocations or /api/monitor/status') },
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
