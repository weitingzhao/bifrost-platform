import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import { jsonResult } from './platformClient.js'
import { PROPOSAL_KEY_RULE, proposePlanImport } from './runtimeTools.js'

/**
 * A plan's first import (W-31 round 2, CUT-plan-import-2026-10-19.md §4 A).
 * reg is the server's tool registration, already scoped to the bridge's
 * focus.
 */
export function registerPlanImportTools(reg: McpServer['tool']): void {
  reg(
    'propose_plan_import',
    `Agent runtime: propose a plan's first import (POST /plan/imports). request_file is the JSON file agentrt-plan-import generate wrote, read only from the request folder (AGENTRT_PLAN_IMPORT_DIR, default ~/agent-work/plan-import). The platform tries the import whole and rolls it back, keeps the request, and puts it to the Owner on one decision card; on Import the controller lands it. The same request again answers with the waiting card, or, once imported, says so; another request for an existing plan is refused. ${PROPOSAL_KEY_RULE}`,
    {
      request_file: z.string().min(1),
      reason: z.string().min(1),
      idempotency_key: z.string().min(1).describe('One key per proposal; see the description'),
    },
    async (input) => jsonResult(await proposePlanImport(input)),
  )
}
