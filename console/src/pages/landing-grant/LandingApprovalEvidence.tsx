import type { ApprovalItem } from '@/api/approvals'
import { CORE_LANDING_NOTICE, GITEA_NOTICE, STG_LANDING_NOTICE, giteaCompareHref, landingGrantHref, type LandingMaterialResponse } from '@/api/landingGrant'

export function LandingApprovalEvidence({ item, data, error, checked, onCheck }: {
  item: ApprovalItem; data?: LandingMaterialResponse; error: boolean; checked: boolean; onCheck: (checked: boolean) => void
}) {
  const op = typeof item.params?.op === 'string' ? item.params.op : ''
  const core = item.action === 'approve_core_landing'
  const m = data?.material
  const compare = m && giteaCompareHref(m.compare_url)
  return <section aria-label="Landing approval evidence" className="space-y-2 text-sm [overflow-wrap:anywhere]">
    {core && <p>{CORE_LANDING_NOTICE}</p>}
    {item.params?.stg_release === true && <p>{STG_LANDING_NOTICE}</p>}
    {op && <a className="underline" href={landingGrantHref(op, item.params?.stg_release !== false)}>Open landing authorization material</a>}
    {error ? <p role="alert">Landing material unavailable or no longer matches this request. Refresh the material before approving.</p>
      : !m ? <p>Reading landing approval material…</p> : <>
        <p>Soft checks: {m.soft_at_request.join(', ') || 'None'}</p>
        <p>Main still M0? {data?.main_known ? data.main_is_M0 ? 'Yes' : 'No — ask the coordinator to refresh' : 'Unknown — approval unavailable'}</p>
        {m.problems.length > 0 && <p role="alert">Approval unavailable: {m.problems.join(', ')}</p>}
        <p>Patch SHA-256: <code>{m.candidate.patch_sha256}</code></p>
        {compare && <a className="underline" href={compare} target="_blank" rel="noopener noreferrer">Compare M0...R in Gitea</a>}
        <p>{m.candidate.decision_chain ? <strong>{GITEA_NOTICE}</strong> : GITEA_NOTICE}</p>
        {core && m.candidate.decision_chain && <>
          <label className="flex items-center gap-2"><input type="checkbox" checked={checked} onChange={e => onCheck(e.target.checked)} />I checked this in Gitea</label>
          <p>This checkbox is a UI constraint only; it is not a mechanical guarantee of independent review.</p>
        </>}
      </>}
  </section>
}
