import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { jsonResult } from './platformClient.js'
import {
  MESSAGE_DATA_NOTE,
  RUNTIME_DATA_NOTE,
  ackMessages,
  getAttemptActions,
  sendMessage,
  waitForMessages,
} from './runtimeTools.js'

/**
 * The inbox and an attempt's actions (W-31 2.3). reg is the server's tool
 * registration, already scoped to the bridge's focus.
 */
export function registerMessageTools(reg: McpServer['tool']): void {
  reg(
    'send_message',
    `Agent runtime: send a message to another actor — owner, seat:<name> (seat:chief_of_staff is the chief of staff) or thread:<session>. kind: ask, answer, help, handover or note; body: text up to 8 KiB. Recorded as this thread. ${MESSAGE_DATA_NOTE}`,
    { to: z.string(), kind: z.enum(['ask', 'answer', 'help', 'handover', 'note']), body: z.string().min(1).max(8192) },
    async (input) => jsonResult(await sendMessage(input)),
  )

  reg(
    'wait_for_messages',
    `Agent runtime: this thread's unread messages, oldest first; waits up to timeout_seconds (default 120, max 600; 0 reads once) for one. Acknowledge what you handled with ack_messages. ${MESSAGE_DATA_NOTE}`,
    {
      timeout_seconds: z.number().int().min(0).max(600).optional(),
      limit: z.number().int().min(1).max(100).optional(),
    },
    async (input) => jsonResult(await waitForMessages(input)),
  )

  reg(
    'ack_messages',
    'Agent runtime: acknowledge this thread\'s own messages (1 to 100 ids, m_…); acknowledging one twice changes nothing.',
    { message_ids: z.array(z.string()).min(1).max(100) },
    async (input) => jsonResult(await ackMessages(input)),
  )

  reg(
    'get_attempt_actions',
    `Agent runtime: an attempt's actions (a_…), kept 24 hours — files read and edited, commands run and their exit codes; after continues from a previous answer's next, last answers only the newest. ${RUNTIME_DATA_NOTE}`,
    { attempt_id: z.string(), after: z.string().optional(), last: z.boolean().optional() },
    async (input) => jsonResult(await getAttemptActions(input)),
  )
}
