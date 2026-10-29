import type { LandingGrant, LandingWait } from '@/api/landingGrant'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { grantState, landingWaitWords } from './landingGrantModel'

export function WaitStatus({ wait }: { wait: LandingWait }) {
  return <div className="space-y-1 text-sm">
    <p>{landingWaitWords(wait)}</p>
    {wait.approval_id && <p><a className="underline" href={approvalHref(wait.approval_id)}>Approval #{wait.approval_number ?? '?'}{wait.approval_status ? ` · ${wait.approval_status}` : ''}</a></p>}
    <p>Main still M0? {wait.main_known ? wait.main_changed ? 'No — refresh required' : 'Yes' : 'Unknown'}</p>
  </div>
}
export function GrantStatus({ grant, opPhase, revoke, disabled }: { grant: LandingGrant; opPhase: string; revoke?: (id: string) => void; disabled?: boolean }) {
  return <section aria-label={`Grant ${grant.grant_id}`} className="space-y-1 rounded border border-[var(--card-border)] p-3 text-sm [overflow-wrap:anywhere]">
    <p><strong>{grantState(grant, Date.now(), opPhase)}</strong> · <a className="underline" href={approvalHref(grant.approval_id)}>Approval #{grant.approval_number}</a></p>
    <p>{grant.action} · {grant.grant_id}</p>
    <p>Approved: {grant.approved_at} · Granted: {grant.granted_at} · Expires: {grant.expires_at}</p>
    <p>Waives: {grant.waives.join(', ') || 'None'} · STG release: {String(grant.stg_release)}</p>
    <p>Candidate digest: <code>{grant.candidate_digest}</code></p>
    <p>Material digest: <code>{grant.material_digest}</code></p>
    {grant.revoked && <p>Revoked at: {grant.revoked_at ?? 'Not reported'}</p>}
    {grant.handed_to_push && <p>Operation {grant.op}, main_intent sequence {grant.main_intent_seq ?? 'not reported'}. Revocation cannot be guaranteed. Reconcile main; use a separate revert if needed.</p>}
    {revoke && !grant.revoked && !grant.handed_to_push && <button type="button" className="underline" disabled={disabled} onClick={() => revoke(grant.grant_id)}>Revoke grant #{grant.approval_number}</button>}
  </section>
}
