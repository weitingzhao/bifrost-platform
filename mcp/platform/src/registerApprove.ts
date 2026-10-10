import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { approvalRef } from './approvalTools.js'
import { jsonResult, platformGet, platformSend } from './platformClient.js'

/**
 * Chat approval server. Registered only when MCP_BRIDGE_FOCUS=approve.
 * The Claude MCP config pins PLATFORM_TOKEN_ENV_KEY=PLATFORM_ADMIN_TOKEN.
 * channel is always "chat"; the model cannot choose another channel.
 *
 * The Claude permission prompt shows tool arguments, not server state. So
 * approve_request and reject_request take the approval line as an argument
 * and send nothing unless it equals the line built from the stored request:
 * what the Owner clicks "allow" on is what gets decided.
 */
export const APPROVE_TOOL_NAMES = ['approve_request', 'reject_request', 'list_pending'] as const

export interface ApprovalRecord {
  id?: string
  number?: number
  action?: string
  tier?: string
  status?: string
  env?: string
  summary?: string
  key_params?: Record<string, string>
}

export interface ApproveClient {
  get: (path: string) => Promise<unknown>
  send: (path: string, body: unknown) => Promise<unknown>
}

const liveClient: ApproveClient = {
  get: (path) => platformGet(path),
  send: (path, body) => platformSend('POST', path, body),
}

function asRecord(value: unknown): ApprovalRecord {
  if (value != null && typeof value === 'object' && !Array.isArray(value)) return value as ApprovalRecord
  return {}
}

/**
 * One line naming the request: number, tier, action, env, summary, key params.
 * Example: "#57 · tier D · owner_run_command · env host · Delete ConfigMap x · name=x"
 */
export function approvalLine(rec: ApprovalRecord): string {
  const parts = [
    rec.number ? `#${rec.number}` : (rec.id ?? ''),
    `tier ${rec.tier ?? '?'}`,
    rec.action ?? '',
  ]
  if (rec.env) parts.push(`env ${rec.env}`)
  if (rec.summary) parts.push(rec.summary)
  const keys = Object.keys(rec.key_params ?? {}).sort()
  if (keys.length > 0) parts.push(keys.map((k) => `${k}=${rec.key_params![k]}`).join(', '))
  return parts.filter((p) => p !== '').join(' · ')
}

function sameLine(a: string, b: string): boolean {
  const norm = (s: string) => s.replace(/\s+/g, ' ').trim()
  return norm(a) === norm(b)
}

type Checked = { ok: true; rec: ApprovalRecord; line: string } | { ok: false; refusal: Record<string, unknown> }

/** Read the request and refuse unless id and approval line match it. Sends nothing on refusal. */
async function checkDecision(id: string, approval: string, client: ApproveClient): Promise<Checked> {
  const ref = approvalRef(id)
  const rec = asRecord(await client.get(`/api/v1/approvals/${encodeURIComponent(ref)}`))
  const line = approvalLine(rec)
  const refuse = (error: string, extra: Record<string, unknown> = {}) =>
    ({ ok: false, refusal: { error, sent: false, approval_line: line, ...extra } }) as const
  if (!rec.id) return refuse('not found', { id: ref })
  if (rec.status !== 'pending') return refuse('not pending', { status: rec.status })
  if (rec.number && ref !== String(rec.number)) {
    return refuse(`pass id "#${rec.number}" so the permission prompt shows the number`)
  }
  if (!sameLine(approval, line)) {
    return refuse('approval line does not match the request; copy approval_line exactly and call again')
  }
  return { ok: true, rec, line }
}

/**
 * Tier D from chat sends no confirm_number: the API exempts channel chat,
 * because the permission prompt showing "#n · tier D" is the second step.
 * The tool never fills confirm_number on the Owner's behalf.
 */
export async function approveRequest(id: string, approval: string, client: ApproveClient = liveClient): Promise<unknown> {
  const checked = await checkDecision(id, approval, client)
  if (!checked.ok) return checked.refusal
  const body = asRecord(
    await client.send(`/api/v1/approvals/${encodeURIComponent(checked.rec.id!)}/approve`, { channel: 'chat' }),
  )
  return { ...body, approval_line: checked.line }
}

export async function rejectRequest(
  id: string,
  approval: string,
  reason: string,
  client: ApproveClient = liveClient,
): Promise<unknown> {
  const checked = await checkDecision(id, approval, client)
  if (!checked.ok) return checked.refusal
  const body = asRecord(
    await client.send(`/api/v1/approvals/${encodeURIComponent(checked.rec.id!)}/reject`, { reason }),
  )
  return { ...body, approval_line: checked.line }
}

/** Pending requests, each with the approval_line that approve_request and reject_request expect. */
export async function listPending(client: ApproveClient = liveClient): Promise<unknown> {
  const body = await client.get('/api/v1/approvals?status=pending')
  const list = (asRecord(body) as { approvals?: unknown }).approvals
  if (!Array.isArray(list)) return body
  return {
    approvals: list.map((a) => ({ approval_line: approvalLine(asRecord(a)), ...asRecord(a) })),
  }
}

const ID_DESC = 'The number as "#57" (approval ids appr_… only for requests without a number)'
const LINE_DESC =
  'approval_line of this request, copied exactly from list_pending, e.g. "#57 · tier D · owner_run_command · env host · …". Shown in the permission prompt.'

export function registerApproveBridge(server: McpServer): void {
  server.tool(
    'approve_request',
    'Approve a pending request when the Owner says 批 #n. Call list_pending first and pass its approval_line; nothing is sent if id or the line do not match the stored request. The permission prompt is the approval. channel is always "chat"; tier D needs no confirm_number from chat. The answer is executed or failed for a platform action, or approved when it waits for an executor or a transient refusal to clear.',
    { id: z.string().describe(ID_DESC), approval: z.string().describe(LINE_DESC) },
    async ({ id, approval }) => jsonResult(await approveRequest(id, approval)),
  )

  server.tool(
    'reject_request',
    'Reject a pending request when the Owner says 驳 #n <reason>. Call list_pending first and pass its approval_line; nothing is sent if id or the line do not match. The permission prompt is the rejection.',
    {
      id: z.string().describe(ID_DESC),
      approval: z.string().describe(LINE_DESC),
      reason: z.string().describe('Why this request is rejected'),
    },
    async ({ id, approval, reason }) => jsonResult(await rejectRequest(id, approval, reason)),
  )

  server.tool(
    'list_pending',
    'List approval requests with status pending. Each carries approval_line for approve_request / reject_request.',
    {},
    async () => jsonResult(await listPending()),
  )
}
