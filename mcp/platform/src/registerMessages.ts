import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { MESSAGE_DATA_NOTE } from './runtimeTools.js'
import { ENVELOPE_TYPES, SUMMARY_MAX } from './messageEnvelope.js'
import { ack, readMessage, send, waitDetailed } from './messageTools.js'

/** Compact JSON: these results land in a model's context, where indentation is paid for. */
function compact(data: unknown) {
  return { content: [{ type: 'text' as const, text: JSON.stringify(data) }] }
}

/**
 * The inbox (W-31 2.3; envelope and summaries: LANE-agent-tool-comm). reg is
 * the server's tool registration, already scoped to the bridge's focus.
 */
export function registerMessageTools(reg: McpServer['tool']): void {
  reg(
    'send_message',
    `Agent runtime: send a message to another actor — owner, seat:<name> (seat:chief_of_staff is the chief of staff) or thread:<session>. Prefer a typed message: type (ask, decide = a question for a ruling, deliver = handing work over, status, note) with a one-line summary (≤${SUMMARY_MAX} chars) the receiver sees first, body as the optional full text, refs for artifact ids (sha256:…) or links instead of long text; kind is then left out. Untyped: kind (ask, answer, help, handover or note) and body. At most 8 KiB in all. Recorded as this thread. ${MESSAGE_DATA_NOTE}`,
    {
      to: z.string(),
      type: z.enum(ENVELOPE_TYPES).optional(),
      summary: z.string().max(SUMMARY_MAX).optional(),
      kind: z.enum(['ask', 'answer', 'help', 'handover', 'note']).optional(),
      body: z.string().min(1).max(8192).optional(),
      refs: z.array(z.string().max(512)).max(20).optional(),
      about: z.array(z.string().max(200)).max(20).optional(),
      reply_to: z.string().optional(),
    },
    async (input) => compact(await send(input)),
  )

  reg(
    'wait_for_messages',
    `Agent runtime: this thread's unread messages, oldest first; waits up to timeout_seconds (default 120, max 600; 0 reads once) for one. detail: auto (default: what you are asked to act on — ask, answer, help, handover, or a typed ask/decide/deliver — comes whole; notices come as their summary with body_omitted), summary, or full. Read a left-out body with read_message before you acknowledge it with ack_messages. ${MESSAGE_DATA_NOTE}`,
    {
      timeout_seconds: z.number().int().min(0).max(600).optional(),
      limit: z.number().int().min(1).max(100).optional(),
      detail: z.enum(['auto', 'summary', 'full']).optional(),
    },
    async (input) => compact(await waitDetailed(input)),
  )

  reg(
    'read_message',
    `Agent runtime: one of this thread's unread messages whole (m_…), when wait_for_messages left its body out. ${MESSAGE_DATA_NOTE}`,
    { message_id: z.string() },
    async (input) => compact(await readMessage(input)),
  )

  reg(
    'ack_messages',
    'Agent runtime: acknowledge this thread\'s own messages (1 to 100 ids, m_…); acknowledging one twice changes nothing. Its body cannot be read afterwards.',
    { message_ids: z.array(z.string()).min(1).max(100) },
    async (input) => compact(await ack(input)),
  )
}
