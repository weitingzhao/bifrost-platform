import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { jsonResult, platformGet } from './platformClient.js'

const SERVER_NAME = 'mcp-server-prometheus'
const SERVER_VERSION = '0.1.0'

/** Register Prometheus MCP tools (L0 read-only via platform-api /telemetry/*). */
export function registerPrometheusBridge(server: McpServer): void {
  server.tool('platform_mcp_health', 'MCP server health + version', {}, async () =>
    jsonResult({
      ok: true,
      server: SERVER_NAME,
      version: SERVER_VERSION,
      focus: 'prometheus',
      platform_api_url: process.env.PLATFORM_API_URL ?? 'http://127.0.0.1:8780',
    }),
  )

  server.tool(
    'query_prometheus',
    'Run instant PromQL query via platform-api telemetry proxy',
    {
      promql: z.string().describe('Prometheus expression (instant vector query)'),
      namespace: z.string().optional().describe('Optional namespace hint (metadata only)'),
    },
    async ({ promql, namespace }) => {
      const params = new URLSearchParams({ q: promql })
      if (namespace != null && namespace !== '') params.set('ns', namespace)
      return jsonResult(await platformGet(`/api/v1/telemetry/promql?${params.toString()}`))
    },
  )

  server.tool(
    'list_alerts',
    'List firing and pending Prometheus alerts',
    {},
    async () => jsonResult(await platformGet('/api/v1/telemetry/alerts')),
  )

  server.tool(
    'list_targets',
    'List Prometheus scrape target health',
    {
      state: z
        .enum(['any', 'active', 'dropped'])
        .optional()
        .describe('Target state filter (default: any)'),
    },
    async ({ state }) => {
      const qs =
        state != null && state !== 'any' ? `?state=${encodeURIComponent(state)}` : ''
      return jsonResult(await platformGet(`/api/v1/telemetry/targets${qs}`))
    },
  )
}
