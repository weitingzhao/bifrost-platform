import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { jsonResult } from './platformClient.js'
import { NOTE_KEY_RULE, PROPOSAL_KEY_RULE, postPlanNote, proposePlanImport } from './runtimeTools.js'

/**
 * A plan's first import (W-31 round 2, CUT-plan-import-2026-10-19.md §4 A),
 * and notes on the architecture table's rows (W-31 2.5 G2 A). reg is the
 * server's tool registration, already scoped to the bridge's focus.
 */
export function registerPlanImportTools(reg: McpServer['tool']): void {
  reg(
    'propose_plan_import',
    `Agent runtime: propose a plan's first import, or a recorder log's import (POST /plan/imports). request_file is the JSON file agentrt-plan-import generate (or recorder) wrote, read only from the request folder (AGENTRT_PLAN_IMPORT_DIR, default ~/agent-work/plan-import). The platform tries the import whole and rolls it back, keeps the request, and puts it to the Owner on one decision card; on Import the controller lands it. The same request again answers with the waiting card, or, once imported, says so; another request for an existing plan is refused. ${PROPOSAL_KEY_RULE}`,
    {
      request_file: z.string().min(1),
      reason: z.string().min(1),
      idempotency_key: z.string().min(1).describe('One key per proposal; see the description'),
    },
    async (input) => jsonResult(await proposePlanImport(input)),
  )

  reg(
    'post_plan_note',
    `Agent runtime: write what an architecture row (ARCH:…, a row of the architecture table) actually is now, in words: a note appended to the row (POST /plan/{id}/notes). Progress shows the latest note beside the row's tick; the tick is the row's gate tests against what is deployed now, never the note. Only an architecture row takes one; text up to 8 KiB, no local paths. Recorded as this thread. ${NOTE_KEY_RULE}`,
    {
      plan_node_id: z.string().min(1).describe('The row, ARCH:<label>'),
      text: z.string().min(1).max(8192),
      idempotency_key: z.string().min(1).describe('One key per note; see the description'),
    },
    async (input) => jsonResult(await postPlanNote(input)),
  )
}
