import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { jsonResult, platformGet, platformSend } from './platformClient.js'

/** Laptop-only. Not registered on the PROD-facing server. */
export const LOCAL_TOOL_NAMES = [
  'platform_mcp_health',
  'list_dev_sessions',
  'get_dev_session_logs',
  'restart_dev_session',
  'get_local_git_bridge',
] as const

const SERVER_NAME = 'mcp-server-platform'

export function registerLocalBridge(server: McpServer): void {
  server.tool('platform_mcp_health', 'Local MCP server health (laptop platform-api)', {}, async () =>
    jsonResult({
      ok: true,
      server: SERVER_NAME,
      focus: 'local',
      platform_api_url: process.env.PLATFORM_API_URL ?? 'http://127.0.0.1:8780',
      tools: [...LOCAL_TOOL_NAMES],
    }),
  )

  server.tool(
    'list_dev_sessions',
    'List laptop bdev sessions (local platform-api only; not PROD)',
    {},
    async () => jsonResult(await platformGet('/api/v1/dev-sessions/')),
  )

  server.tool(
    'get_dev_session_logs',
    'Recent log lines for a laptop bdev session, including git-bridge',
    {
      name: z.string().describe('Session name, e.g. git-bridge or platform-api'),
      lines: z.number().optional().describe('Number of log lines (default 100)'),
    },
    async ({ name, lines }) =>
      jsonResult(
        await platformGet(
          `/api/v1/dev-sessions/${encodeURIComponent(name)}/logs?lines=${lines ?? 100}`,
        ),
      ),
  )

  server.tool(
    'restart_dev_session',
    'Restart a laptop bdev session (direct; not an approval). Includes git-bridge.',
    { name: z.string().describe('Session name from list_dev_sessions') },
    async ({ name }) =>
      jsonResult(
        await platformSend('POST', `/api/v1/dev-sessions/${encodeURIComponent(name)}/control`, {
          action: 'restart',
        }),
      ),
  )

  server.tool(
    'get_local_git_bridge',
    'Laptop git-bridge status from the local platform-api agent-bridge probe',
    {},
    async () => {
      const body = await platformGet('/api/v1/agent/bridge')
      const record = body != null && typeof body === 'object' ? (body as Record<string, unknown>) : {}
      return jsonResult({ git_bridge: record.git_bridge ?? null })
    },
  )
}
