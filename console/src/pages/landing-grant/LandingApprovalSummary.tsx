import type { ApprovalItem } from '@/api/approvals'
import { STG_LANDING_NOTICE } from '@/api/landingGrant'
import { terminalApproval, useLandingMaterial } from './useLandingMaterial'

/** The list shows the current soft set only when it still matches the request's digest. */
export function LandingApprovalSummary({ item }: { item: ApprovalItem }) {
  const op = typeof item.params?.op === 'string' ? item.params.op : ''
  const stg = item.params?.stg_release !== false
  const q = useLandingMaterial(op, stg, terminalApproval(item.status) || item.resolution != null)
  const data = q.confirmed ? q.data : undefined
  const matches = data?.material.material_digest === item.params?.material_digest
  return <span className="block text-sm">
    <span className="block">{data && matches ? `Soft checks: ${data.material.soft_at_request.join(', ') || 'None'}` : 'Soft checks unavailable for this request; open its material to review.'}</span>
    {q.waiting && <span className="block" role="status">{q.waitingMessage}</span>}
    {q.retryStopped && <button type="button" className="underline" disabled={q.isFetching} onClick={() => void q.refetch()}>Refresh material</button>}
    {data?.main_known && !data.main_is_M0 && <span className="block">Request is stale; waiting for the coordinator to refresh.</span>}
    {item.params?.stg_release === true && <span className="block">{STG_LANDING_NOTICE}</span>}
  </span>
}
