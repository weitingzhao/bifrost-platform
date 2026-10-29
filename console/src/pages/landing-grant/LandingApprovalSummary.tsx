import { useQuery } from '@tanstack/react-query'
import type { ApprovalItem } from '@/api/approvals'
import { fetchLandingMaterial, STG_LANDING_NOTICE } from '@/api/landingGrant'

/** The list shows the current soft set only when it still matches the request's digest. */
export function LandingApprovalSummary({ item }: { item: ApprovalItem }) {
  const op = typeof item.params?.op === 'string' ? item.params.op : ''
  const stg = item.params?.stg_release !== false
  const q = useQuery({ queryKey: ['landing-material', op, stg], queryFn: ({ signal }) => fetchLandingMaterial(op, stg, signal), enabled: op !== '', retry: false, refetchInterval: 15_000 })
  const data = q.isSuccess && !q.isPaused ? q.data : undefined
  const matches = data?.material.material_digest === item.params?.material_digest
  return <span className="block text-sm">
    <span className="block">{data && matches ? `Soft checks: ${data.material.soft_at_request.join(', ') || 'None'}` : 'Soft checks unavailable for this request; open its material to review.'}</span>
    {data?.main_known && !data.main_is_M0 && <span className="block">Request is stale; waiting for the coordinator to refresh.</span>}
    {item.params?.stg_release === true && <span className="block">{STG_LANDING_NOTICE}</span>}
  </span>
}
