import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { approvalRef } from './approvalTools.js'
import { consoleApprovalUrl } from './approveHint.js'
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
 *
 * Tier D is decided on Console only (ADR §5, Owner 2026-10-10): a prompt
 * cannot show the full command, and once the out-of-band executor runs the
 * approval is the only gate. The tier is the one the API stored. The API
 * also answers 403 to a chat approval of tier D; refusing here as well keeps
 * the model from raising a prompt for it. The API accepts a tier D reject
 * from any route; this tool still refuses it so tier D is decided on Console.
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
  /** Canonical line produced by the server. Shown and compared exactly. */
  approval_line?: string
  params_hash?: string
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
 * The approval line is the string the server produced. This does not rebuild
 * it, and comparison is strict equality: whitespace is significant.
 */
export function approvalLine(rec: ApprovalRecord): string {
  return rec.approval_line ?? ''
}

function sameLine(a: string, b: string): boolean {
  return a === b
}

type Checked = { ok: true; rec: ApprovalRecord; line: string } | { ok: false; refusal: Record<string, unknown> }

/** The answer for a tier D request: nothing sent, decide on Console. */
function consoleOnly(rec: ApprovalRecord): Record<string, unknown> {
  const ref = rec.number ? `#${rec.number}` : rec.id
  return {
    error: 'tier D is approved on Console only',
    sent: false,
    id: rec.id,
    ...(rec.number ? { number: rec.number } : {}),
    tier: 'D',
    console_url: consoleApprovalUrl(rec.id!),
    hint: `Tell the Owner to approve or reject ${ref} on Console. Chat does not decide tier D (ADR §5).`,
  }
}

/** Read the request and refuse unless id and approval line match it. Sends nothing on refusal. */
async function checkDecision(id: string, approval: string, client: ApproveClient): Promise<Checked> {
  const ref = approvalRef(id)
  const rec = asRecord(await client.get(`/api/v1/approvals/${encodeURIComponent(ref)}`))
  const line = approvalLine(rec)
  const refuse = (error: string, extra: Record<string, unknown> = {}) =>
    ({ ok: false, refusal: { error, sent: false, approval_line: line, ...extra } }) as const
  if (!rec.id) return refuse('not found', { id: ref })
  if (rec.tier === 'D') return { ok: false, refusal: consoleOnly(rec) }
  if (!rec.approval_line || !rec.params_hash) return refuse('approval line is not available from the server')
  if (rec.status !== 'pending') return refuse('not pending', { status: rec.status })
  if (rec.number && ref !== String(rec.number)) {
    return refuse(`pass id "#${rec.number}" so the permission prompt shows the number`)
  }
  if (!sameLine(approval, line)) {
    return refuse('approval line does not match the request; copy approval_line exactly and call again')
  }
  return { ok: true, rec, line }
}

/** Tiers below D only; tier D answers with its Console link and sends nothing. */
export async function approveRequest(id: string, approval: string, client: ApproveClient = liveClient): Promise<unknown> {
  const checked = await checkDecision(id, approval, client)
  if (!checked.ok) return checked.refusal
  const body = asRecord(
    await client.send(`/api/v1/approvals/${encodeURIComponent(checked.rec.id!)}/approve`, {
      channel: 'chat',
      approval_line: checked.line,
      params_hash: checked.rec.params_hash,
    }),
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

/**
 * Pending requests. Below tier D each carries the approval_line that
 * approve_request and reject_request expect; tier D carries console_url
 * instead, so there is no line to call the tools with.
 */
export async function listPending(client: ApproveClient = liveClient): Promise<unknown> {
  const body = await client.get('/api/v1/approvals?status=pending')
  const list = (asRecord(body) as { approvals?: unknown }).approvals
  if (!Array.isArray(list)) return body
  return {
    approvals: list.map((a) => {
      const rec = asRecord(a)
      if (rec.tier === 'D') {
        const rest: ApprovalRecord = { ...rec }
        delete rest.approval_line
        if (rec.id) return { ...rest, console_only: true, console_url: consoleApprovalUrl(rec.id) }
        return rest
      }
      return { approval_line: approvalLine(rec), ...rec }
    }),
  }
}

const ID_DESC = 'The number as "#57" (approval ids appr_… only for requests without a number)'
const LINE_DESC =
  'approval_line of this request, copied exactly from list_pending, e.g. "#57 · tier C · start_pipeline_run · env prod · …". Shown in the permission prompt.'
const TIER_D_NOTE =
  'Tier D is decided on Console only: do not call this tool for it; give the Owner console_url from list_pending. A tier D call sends nothing.'

export function registerApproveBridge(server: McpServer): void {
  server.tool(
    'approve_request',
    `Approve a pending request below tier D when the Owner says 批 #n. Call list_pending first and pass its approval_line; nothing is sent if id or the line do not match the stored request. The permission prompt is the approval. channel is always "chat". ${TIER_D_NOTE} The answer is executed or failed for a platform action, or approved when it waits for an executor or a transient refusal to clear.`,
    { id: z.string().describe(ID_DESC), approval: z.string().describe(LINE_DESC) },
    async ({ id, approval }) => jsonResult(await approveRequest(id, approval)),
  )

  server.tool(
    'reject_request',
    `Reject a pending request below tier D when the Owner says 驳 #n <reason>. Call list_pending first and pass its approval_line; nothing is sent if id or the line do not match. The permission prompt is the rejection. ${TIER_D_NOTE}`,
    {
      id: z.string().describe(ID_DESC),
      approval: z.string().describe(LINE_DESC),
      reason: z.string().describe('Why this request is rejected'),
    },
    async ({ id, approval, reason }) => jsonResult(await rejectRequest(id, approval, reason)),
  )

  server.tool(
    'list_pending',
    'List approval requests with status pending. Below tier D each carries approval_line for approve_request / reject_request; tier D carries console_only and console_url (decided on Console only).',
    {},
    async () => jsonResult(await listPending()),
  )
}
