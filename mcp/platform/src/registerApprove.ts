import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { approvalRef } from './approvalTools.js'
import { jsonResult, platformGet, platformSend } from './platformClient.js'

/**
 * Chat approval server. Registered only when MCP_BRIDGE_FOCUS=approve.
 * The Claude MCP config pins PLATFORM_TOKEN_ENV_KEY=PLATFORM_ADMIN_TOKEN.
 * channel is always "chat"; the model cannot choose another channel.
 */
export const APPROVE_TOOL_NAMES = ['approve_request', 'reject_request', 'list_pending'] as const

export async function approveRequest(id: string): Promise<unknown> {
  return platformSend('POST', `/api/v1/approvals/${encodeURIComponent(approvalRef(id))}/approve`, {
    channel: 'chat',
  })
}

export async function rejectRequest(id: string, reason: string): Promise<unknown> {
  return platformSend('POST', `/api/v1/approvals/${encodeURIComponent(approvalRef(id))}/reject`, { reason })
}

export async function listPending(): Promise<unknown> {
  return platformGet('/api/v1/approvals?status=pending')
}

export function registerApproveBridge(server: McpServer): void {
  server.tool(
    'approve_request',
    'Approve a pending request. channel is always "chat". Admin token only. The answer is executed or failed for a platform action, or approved when it waits for an executor or a transient refusal to clear.',
    { id: z.string().describe('Approval id (appr_…) or number (57 or #57)') },
    async ({ id }) => jsonResult(await approveRequest(id)),
  )

  server.tool(
    'reject_request',
    'Reject a pending request. Admin token only.',
    {
      id: z.string().describe('Approval id (appr_…) or number (57 or #57)'),
      reason: z.string().describe('Why this request is rejected'),
    },
    async ({ id, reason }) => jsonResult(await rejectRequest(id, reason)),
  )

  server.tool(
    'list_pending',
    'List approval requests with status pending',
    {},
    async () => jsonResult(await listPending()),
  )
}
