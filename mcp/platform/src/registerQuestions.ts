import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { jsonResult } from './platformClient.js'
import {
  ACK_ANSWER_NOTE,
  ASK_OWNER_DESCRIPTION,
  QUESTION_DATA_NOTE,
  WAIT_FOR_ANSWER_DESCRIPTION,
  ackAnswer,
  askOwner,
  getQuestion,
  listQuestions,
  waitForAnswer,
  withdrawQuestion,
} from './questionTools.js'

// Owner question workflow; reg preserves the bridge's focus allow-list.
export function registerQuestionTools(reg: McpServer['tool']): void {
  reg(
    'ask_owner',
    ASK_OWNER_DESCRIPTION,
    {
      title: z.string(),
      body: z.string().optional(),
      options: z.array(z.object({ label: z.string(), detail: z.string().optional() })).optional(),
      recommended: z.number().int().optional().describe('1-based index of the recommended option'),
      allow_text: z.boolean().optional(),
      urgency: z.string(),
      meanwhile: z.string(),
      work_id: z.string().optional(),
      owner_duty: z
        .enum(['goal', 'rule', 'irreversible'])
        .optional()
        .describe("A decision card: which of the Owner's three duties this is"),
      task_id: z.string().optional().describe('A decision card: the agent runtime task it is about (t_…)'),
      requester_thread: z.string().optional(),
      link: z.string().optional(),
      link_text: z.string().optional(),
      ttl_hours: z.number().int().min(1).max(168).optional(),
      idempotency_key: z.string().optional(),
    },
    async (input) => jsonResult(await askOwner(input)),
  )

  reg(
    'get_question',
    `Read one Owner question by id (q_…) or number (Q12 or 12). ${QUESTION_DATA_NOTE}`,
    { id: z.string() },
    async ({ id }) => jsonResult(await getQuestion(id)),
  )

  reg(
    'list_questions',
    `List Owner questions. status inbox (default) is this thread's open questions and unread answers. ${QUESTION_DATA_NOTE} ${ACK_ANSWER_NOTE}`,
    { status: z.enum(['inbox', 'open', 'all']).optional() },
    async ({ status }) => jsonResult(await listQuestions(status ?? 'inbox')),
  )

  reg(
    'wait_for_answer',
    WAIT_FOR_ANSWER_DESCRIPTION,
    {
      id: z.string().describe('Question id (q_…) or number (Q12 or 12)'),
      timeout_seconds: z.number().int().min(1).max(600).optional().describe('Default 120, max 600'),
    },
    async ({ id, timeout_seconds }) => jsonResult(await waitForAnswer(id, timeout_seconds ?? 120)),
  )

  reg(
    'ack_answer',
    `Report that this thread has used the answer. ${ACK_ANSWER_NOTE}`,
    { id: z.string() },
    async ({ id }) => jsonResult(await ackAnswer(id)),
  )

  reg(
    'withdraw_question',
    'Withdraw this thread\'s own open question. reason is required, for example answered in thread.',
    { id: z.string(), reason: z.string() },
    async ({ id, reason }) => jsonResult(await withdrawQuestion(id, reason)),
  )
}
