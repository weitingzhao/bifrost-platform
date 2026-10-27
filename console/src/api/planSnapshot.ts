import { authedFetch } from './client'
import { parsePlanSnapshot } from './planSnapshotSchema'
export { parsePlanSnapshot } from './planSnapshotSchema'
export type { PlanSnapshot, PlanNode, PlanItem, PlanEdge, PlanWork, ExternalWork } from './planSnapshotSchema'

export async function fetchPlanSnapshot(root: string, signal?: AbortSignal, cursor = '') {
  const query = new URLSearchParams({ root })
  if (cursor) query.set('recent_cursor', cursor)
  const response = await authedFetch('Plan view', `/api/v1/agentrt/plan-view?${query}`, { signal })
  return parsePlanSnapshot(await response.json(), root)
}
