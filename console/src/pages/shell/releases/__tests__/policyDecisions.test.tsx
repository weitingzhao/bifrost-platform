import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { AuditRecord } from '@/api/auditTypes'
import { ReleasePolicyDecisionsSection } from '@/pages/shell/releases/ReleasePolicyDecisionsSection'
import { policyDecision, policyDecisionsHeadline, policyDecisionsView } from '@/pages/shell/releases/policyDecisions'

const PLATFORM = 'f7bc5bfa4eb6ff53be59a92f401fb388da3b492e'
const INFRA = '820592ad8f3dd2531fc042da0aa283736bb2edfd'
const UI = 'c83f40b40e65e52f8592ec6b73c5f6987c116e93'

function record(partial: Partial<AuditRecord> & Pick<AuditRecord, 'id' | 'at' | 'action'>): AuditRecord {
  return { actor: 'operator', role: 'operator', target: '', status: 'ok', detail: '', ...partial }
}

/** Two records as PROD's GET /api/v1/audit returned them on 2026-11-13 (requester shortened). */
const TRUST_ANCHOR = record({
  id: '1794531538477845142',
  at: '2026-11-13T00:58:58.477846555Z',
  action: 'release_policy.check',
  target: 'start_pipeline_run bifrost-deliver-platform',
  status: 'not covered',
  detail: `policy_id=rp-20261112-2104 requester=local_7a2e681e sha=bifrost-platform=${PLATFORM},bifrost-trade-infra=${INFRA},bifrost-ui=${UI} clauses=signature,unexpired,not_frozen,allow:bifrost-deliver-platform,window_held_by_requester reasons=bifrost-platform 9f739bcf6775..f7bc5bfa4eb6 hits no_trust_anchor_change: api/internal/delivery/release_policy_facts.go, api/internal/releasepolicy/engine.go`,
})
const DIFF_404 = record({
  id: '1794519114624945270',
  at: '2026-11-12T21:31:54.624946944Z',
  action: 'release_policy.check',
  target: 'start_pipeline_run bifrost-deliver-platform',
  status: 'not covered',
  detail: `policy_id=rp-20261112-2104 requester=local_553986d3 sha=bifrost-platform=1c886ac929867974df9811141af26f21716a5340,bifrost-trade-infra=${INFRA},bifrost-ui=${UI} clauses=signature,unexpired,not_frozen,allow:bifrost-deliver-platform,window_held_by_requester reasons=bifrost-platform: cannot read the diff 87aab7b72d8f..1c886ac92986: status 404: 404 page not found\n; bifrost-trade-infra: cannot read the diff 33f7c4f1e9a8..820592ad8f3d: status 404: 404 page not found\n`,
})
/** The shapes release_policy_wire.go writes for a covered check and for an auto-approval. */
const COVERED_CHECK = record({
  id: '30',
  at: '2026-11-13T02:00:00Z',
  action: 'release_policy.check',
  target: 'start_pipeline_run bifrost-deliver-platform',
  status: 'covered',
  detail: `policy_id=rp-20261112-2104 requester=local_2fdc455a sha=bifrost-platform=${PLATFORM},bifrost-ui=${UI} clauses=signature,unexpired,not_frozen,allow:bifrost-deliver-platform,window_held_by_requester,ci_succeeded,no_ddl,no_d10_paths,no_trust_anchor_change`,
})
const AUTO_APPROVED = record({
  id: '31',
  at: '2026-11-13T02:10:00Z',
  action: 'release_policy.auto_approve',
  target: 'start_pipeline_run bifrost-deliver-platform-prod',
  status: 'approved',
  detail: `auto-approved policy_id=rp-20261112-2104 requester=local_2fdc455a sha=bifrost-platform=${PLATFORM},bifrost-ui=${UI} clauses=signature,unexpired,not_frozen,allow:bifrost-deliver-platform-prod,window_held_by_requester`,
})
const OTHER = [
  record({ id: '9', at: '2026-11-12T21:04:46Z', action: 'release_policy.install', target: 'rp-20261112-2104' }),
  record({ id: '8', at: '2026-11-11T10:00:00Z', action: 'rollout_restart_deployment', target: 'bifrost-stg/api' }),
]

describe('policyDecision', () => {
  it('reads pipeline, policy, requester, commits and the reasons as written', () => {
    const d = policyDecision(TRUST_ANCHOR)
    expect(d.pipeline).toBe('bifrost-deliver-platform')
    expect(d.outcome).toBe('not_covered')
    expect(d.policyId).toBe('rp-20261112-2104')
    expect(d.requester).toBe('local_7a2e681e')
    expect(d.commits).toEqual([
      { repo: 'bifrost-platform', sha: PLATFORM },
      { repo: 'bifrost-trade-infra', sha: INFRA },
      { repo: 'bifrost-ui', sha: UI },
    ])
    expect(d.reasons).toBe(
      'bifrost-platform 9f739bcf6775..f7bc5bfa4eb6 hits no_trust_anchor_change: api/internal/delivery/release_policy_facts.go, api/internal/releasepolicy/engine.go',
    )
  })

  it('keeps several reasons whole, inner line breaks included', () => {
    expect(policyDecision(DIFF_404).reasons).toBe(
      'bifrost-platform: cannot read the diff 87aab7b72d8f..1c886ac92986: status 404: 404 page not found\n; bifrost-trade-infra: cannot read the diff 33f7c4f1e9a8..820592ad8f3d: status 404: 404 page not found',
    )
  })

  it('reads an auto-approval, whose detail starts with a word that is not a field', () => {
    const d = policyDecision(AUTO_APPROVED)
    expect(d.outcome).toBe('auto_approved')
    expect(d.pipeline).toBe('bifrost-deliver-platform-prod')
    expect(d.policyId).toBe('rp-20261112-2104')
    expect(d.reasons).toBe('')
  })

  it('does not call a status it does not know covered or not covered', () => {
    expect(policyDecision({ ...COVERED_CHECK, status: 'skipped' }).outcome).toBe('unknown')
    expect(policyDecision({ ...AUTO_APPROVED, status: 'refused' }).outcome).toBe('unknown')
  })

  it('reads a record with an empty detail', () => {
    const d = policyDecision({ ...COVERED_CHECK, detail: '', target: 'start_pipeline_run' })
    expect(d).toMatchObject({ pipeline: 'start_pipeline_run', policyId: '', commits: [], reasons: '' })
  })
})

describe('policyDecisionsView', () => {
  it('counts only decisions, newest first, whatever order the records came in', () => {
    const view = policyDecisionsView([DIFF_404, ...OTHER, AUTO_APPROVED, TRUST_ANCHOR, COVERED_CHECK])
    expect(view.decisions.map(d => d.id)).toEqual(['31', '30', TRUST_ANCHOR.id, DIFF_404.id])
    expect(view).toMatchObject({ covered: 2, autoApproved: 1, notCovered: 2, recordsRead: 6, oldestRecordAt: '2026-11-11T10:00:00Z' })
    expect(view.lastAutoApproved?.id).toBe('31')
    expect(policyDecisionsHeadline(view)).toBe('2 of the last 4 decisions covered')
  })

  it('sums the listed decisions only, and still finds an older auto-approval', () => {
    const view = policyDecisionsView([TRUST_ANCHOR, DIFF_404, { ...AUTO_APPROVED, at: '2026-11-10T00:00:00Z' }], 2)
    expect(view.decisions).toHaveLength(2)
    expect(view).toMatchObject({ covered: 0, autoApproved: 0, notCovered: 2 })
    expect(view.lastAutoApproved?.id).toBe('31')
    expect(policyDecisionsHeadline(view)).toBe('0 of the last 2 decisions covered')
  })

  it('says so when the records hold no decision', () => {
    const view = policyDecisionsView(OTHER)
    expect(view.decisions).toEqual([])
    expect(policyDecisionsHeadline(view)).toBe('No release policy decision in the audit records')
  })
})

describe('ReleasePolicyDecisionsSection', () => {
  it('lists each decision with its pipeline, outcome and reasons, under a line that sums them', () => {
    render(<ReleasePolicyDecisionsSection records={[TRUST_ANCHOR, DIFF_404, COVERED_CHECK, AUTO_APPROVED, ...OTHER]} isLoading={false} error={null} />)
    const summary = screen.getByTestId('release-policy-decisions-summary')
    expect(summary.textContent).toContain('2 of the last 4 decisions covered')
    expect(summary.textContent).toContain('1 started a run without approval')
    expect(summary.textContent).toContain('Last run started on the policy alone: 2026-11-13 02:10Z bifrost-deliver-platform-prod')
    expect(summary.textContent).toContain('Read from the newest 6 audit records, back to 2026-11-11 10:00Z')
    const rows = screen.getAllByRole('row').slice(1)
    expect(rows).toHaveLength(4)
    expect(within(rows[0]).getByText('bifrost-deliver-platform-prod')).toBeTruthy()
    expect(within(rows[0]).getByText('covered · run started without approval')).toBeTruthy()
    expect(within(rows[1]).getByText('covered · check only')).toBeTruthy()
    expect(within(rows[2]).getByText('not covered')).toBeTruthy()
    expect(rows[2].textContent).toContain('2026-11-13 00:58Z')
    expect(rows[2].textContent).toContain('hits no_trust_anchor_change: api/internal/delivery/release_policy_facts.go')
    expect(rows[2].textContent).toContain('bifrost-platform f7bc5bf, bifrost-trade-infra 820592a, bifrost-ui c83f40b')
    expect(rows[3].textContent).toContain('status 404: 404 page not found')
  })

  it('says that no run started on the policy alone when every decision was a refusal', () => {
    render(<ReleasePolicyDecisionsSection records={[TRUST_ANCHOR, DIFF_404]} isLoading={false} error={null} />)
    const summary = screen.getByTestId('release-policy-decisions-summary')
    expect(summary.textContent).toContain('0 of the last 2 decisions covered')
    expect(summary.textContent).toContain('none started a run without approval')
    expect(summary.textContent).toContain('No run started on the policy alone in these records')
  })

  it('shows an unknown status as written', () => {
    render(<ReleasePolicyDecisionsSection records={[{ ...COVERED_CHECK, status: 'skipped' }]} isLoading={false} error={null} />)
    expect(screen.getByText('skipped')).toBeTruthy()
    expect(screen.getByTestId('release-policy-decisions-summary').textContent).toContain('0 of the last 1 decision covered')
  })

  it('says the audit could not be read instead of showing no decisions', () => {
    render(<ReleasePolicyDecisionsSection records={undefined} isLoading={false} error="audit: HTTP 503" />)
    expect(screen.getByRole('alert').textContent).toContain('Audit records could not be read: audit: HTTP 503. What the policy decided is not known.')
    expect(screen.queryByTestId('release-policy-decisions-summary')).toBeNull()
  })

  it('shows loading while the audit is unread', () => {
    render(<ReleasePolicyDecisionsSection records={undefined} isLoading error={null} />)
    expect(screen.getByText('Loading…')).toBeTruthy()
  })
})
