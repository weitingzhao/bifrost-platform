import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { AUDIT_SINCE_ERROR, filterAudit, filterRequests } from './readFilters.js'

// The shapes GET /api/v1/approvals and GET /api/v1/audit answer
// (approvals/types.go Approval, actuation/audit.go AuditRecord).
function approval(n: number, action: string, created_at: string, status = 'executed') {
  return { id: `appr_${n}`, number: n, action, tier: 'C', status, reason: 'r', created_at }
}

const approvals = {
  approvals: [
    approval(5, 'start_pipeline_run', '2026-11-12T10:00:00Z'),
    approval(4, 'gitops_sync_app', '2026-11-11T10:00:00Z'),
    approval(3, 'start_pipeline_run', '2026-11-10T10:00:00Z', 'rejected'),
    approval(2, 'start_pipeline_run_extra', '2026-11-09T10:00:00Z'),
    approval(1, 'start_pipeline_run', '2026-11-08T10:00:00Z'),
  ],
}

describe('list_requests filters', () => {
  it('returns the server body untouched when neither action nor limit is given', () => {
    assert.equal(filterRequests(approvals, {}), approvals)
    assert.equal(filterRequests(approvals, { action: '  ' }), approvals)
  })

  it('keeps only the exact action, newest first, and counts what matched', () => {
    const got = filterRequests(approvals, { action: 'start_pipeline_run' }) as { approvals: Array<{ number: number }>; matched: number; returned: number }
    assert.deepEqual(got.approvals.map((a) => a.number), [5, 3, 1])
    assert.equal(got.matched, 3)
    assert.equal(got.returned, 3)
    const none = filterRequests(approvals, { action: 'drain_node' }) as { approvals: unknown[]; matched: number }
    assert.deepEqual(none.approvals, [])
    assert.equal(none.matched, 0)
  })

  it('limit takes the newest whatever order the list came in, and is capped', () => {
    const shuffled = { approvals: [approvals.approvals[3], approvals.approvals[0], approvals.approvals[4], approvals.approvals[1], approvals.approvals[2]] }
    const two = filterRequests(shuffled, { limit: 2 }) as { approvals: Array<{ number: number }>; matched: number; returned: number }
    assert.deepEqual(two.approvals.map((a) => a.number), [5, 4])
    assert.equal(two.matched, 5)
    assert.equal(two.returned, 2)

    const many = { approvals: Array.from({ length: 250 }, (_, i) => approval(i + 1, 'run_probe_pod', new Date(Date.UTC(2026, 0, 1) + i * 60_000).toISOString())) }
    const byAction = filterRequests(many, { action: 'run_probe_pod' }) as { approvals: Array<{ number: number }>; matched: number; returned: number }
    assert.equal(byAction.returned, 20) // the default once a filter is given
    assert.equal(byAction.matched, 250)
    assert.equal(byAction.approvals[0].number, 250)
    const capped = filterRequests(many, { limit: 5000 }) as { returned: number }
    assert.equal(capped.returned, 200)
  })

  it('passes on a body that is not a list answer', () => {
    const err = { error: 'status must be pending, open, all, or one status' }
    assert.equal(filterRequests(err, { action: 'x' }), err)
  })
})

function record(id: string, at: string, action: string, target: string) {
  return { id, at, actor: 'thread:x', role: 'operator', action, target, status: 'ok', detail: '' }
}

const audit = {
  records: [
    record('6', '2026-11-12T15:50:00Z', 'release_policy.auto_approved', 'start_pipeline_run/bifrost-deliver-platform-prod'),
    record('5', '2026-11-12T15:40:00Z', 'delivery.pipeline.run', 'PipelineRun/cicd/bifrost-deliver-platform-prod-1'),
    record('4', '2026-11-12T15:00:00Z', 'release_policy.refused', 'start_pipeline_run/bifrost-deliver-platform-prod'),
    record('3', '2026-11-12T14:00:00Z', 'release_policy.refused', 'start_pipeline_run/bifrost-deliver-prod'),
    record('2', '2026-11-12T09:00:00Z', 'cluster.rollout_restart', 'Deployment/bifrost-stg/api'),
    record('1', '2026-11-11T09:00:00Z', 'release_policy.signed', 'release-policy@3'),
  ],
}
const NOW = Date.parse('2026-11-12T16:00:00Z')
const ids = (got: unknown) => (got as { records: Array<{ id: string }> }).records.map((r) => r.id)

describe('get_audit_log filters', () => {
  it('returns the server body untouched when no filter is given', () => {
    assert.equal(filterAudit(audit, {}, NOW), audit)
    assert.equal(filterAudit(audit, { action_prefix: '', target_contains: '', since: '' }, NOW), audit)
  })

  it('action_prefix keeps the actions that start with it', () => {
    const got = filterAudit(audit, { action_prefix: 'release_policy.' }, NOW) as { matched: number; returned: number }
    assert.deepEqual(ids(got), ['6', '4', '3', '1'])
    assert.equal(got.matched, 4)
    assert.equal(got.returned, 4)
    assert.deepEqual(ids(filterAudit(audit, { action_prefix: 'policy' }, NOW)), [])
  })

  it('since and since_minutes bound the time; with both the later bound holds', () => {
    assert.deepEqual(ids(filterAudit(audit, { since: '2026-11-12T15:00:00Z' }, NOW)), ['6', '5', '4'])
    assert.deepEqual(ids(filterAudit(audit, { since: '2026-11-12T09:00:00-06:00' }, NOW)), ['6', '5', '4'])
    assert.deepEqual(ids(filterAudit(audit, { since_minutes: 30 }, NOW)), ['6', '5'])
    assert.deepEqual(ids(filterAudit(audit, { since: '2026-11-11T00:00:00Z', since_minutes: 30 }, NOW)), ['6', '5'])
    assert.deepEqual(ids(filterAudit(audit, { since: '2026-11-12T15:45:00Z', since_minutes: 600 }, NOW)), ['6'])
    assert.deepEqual(filterAudit(audit, { since: 'yesterday' }, NOW), { error: AUDIT_SINCE_ERROR })
  })

  it('target_contains ignores case; every given filter must pass', () => {
    assert.deepEqual(ids(filterAudit(audit, { target_contains: 'PLATFORM-PROD' }, NOW)), ['6', '5', '4'])
    assert.deepEqual(
      ids(filterAudit(audit, { action_prefix: 'release_policy.', target_contains: 'platform-prod', since_minutes: 120 }, NOW)),
      ['6', '4'],
    )
  })

  it('limit takes the newest of what matched, whatever order the records came in', () => {
    const shuffled = { records: [...audit.records].reverse() }
    const got = filterAudit(shuffled, { action_prefix: 'release_policy.', limit: 2 }, NOW) as { matched: number; returned: number }
    assert.deepEqual(ids(got), ['6', '4'])
    assert.equal(got.matched, 4)
    assert.equal(got.returned, 2)
    assert.deepEqual(ids(filterAudit(audit, { limit: 1 }, NOW)), ['6'])
    assert.equal((filterAudit(audit, { limit: 9999 }, NOW) as { returned: number }).returned, 6)
  })
})
