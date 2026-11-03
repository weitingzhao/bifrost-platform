import { authedFetch } from './client'
import { parsePlanExplain, type ExplainRead } from './planExplainSchema'
import { parsePlanSnapshot, type PlanSnapshot } from './planSnapshotSchema'
export { parsePlanSnapshot } from './planSnapshotSchema'
export type { PlanSnapshot, PlanNode, PlanItem, PlanEdge, PlanWork, ExternalWork } from './planSnapshotSchema'
export type { ExplainRead } from './planExplainSchema'

/** A plan view with, when asked for, its explain block as read (R5:5.5-19); absent from a server without one. */
export type PlanSnapshotRead = PlanSnapshot & { explain?: ExplainRead }

export async function fetchPlanSnapshot(root: string, signal?: AbortSignal, cursor = '', explain = false): Promise<PlanSnapshotRead> {
  const query = new URLSearchParams({ root })
  if (cursor) query.set('recent_cursor', cursor)
  if (explain) query.set('explain', '1')
  const response = await authedFetch('Plan view', `/api/v1/agentrt/plan-view?${query}`, { signal })
  return splitPlanExplain(await response.json(), root)
}

/**
 * The base snapshot parser refuses any key it does not know, so the explain block is taken out first and read on its
 * own: a block that cannot be read affects the block alone, never the snapshot (§6 item 6).
 */
export function splitPlanExplain(value: unknown, root: string): PlanSnapshotRead {
  if (value === null || typeof value !== 'object' || Array.isArray(value) || !('explain' in value)) return parsePlanSnapshot(value, root)
  const { explain, ...rest } = value as Record<string, unknown>
  const view = parsePlanSnapshot(rest, root)
  return explain === undefined ? view : { ...view, explain: parsePlanExplain(explain) }
}
