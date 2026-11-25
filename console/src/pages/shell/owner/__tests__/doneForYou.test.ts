import { describe, expect, it } from 'vitest'
import type { ActionRecord } from '@/api/actionRecords'
import { doneForYou, doneSummary, undoText } from '../doneForYou'

// 2026-11-19, LAW-6: what can be undone runs without a card and is recorded; Command shows it as
// Done for you. Rows copied from PROD's list_records the same day (ids 815-823): every one then had
// regret_state undeclared and an empty check_result, which the column must say rather than hide.
const NOW = Date.parse('2026-11-19T14:40:00Z')
const rec = (o: Partial<ActionRecord>): ActionRecord => ({
  id: 1, at: '2026-11-19T14:14:22Z', seat: 'local_7de640c4-4b19-4a71-a0ac-ced2ce5fc7ff', action: 'apply_manifest',
  target: 'k8s/cicd/tekton/task-ci-cache-trim.yaml · 1 objects', reason: '(the caller gave no reason)', regret: '',
  regret_state: 'undeclared', law: 'signed release policy', outcome: 'done', check_result: '', not_verified: '', params_hash: 'x', ...o,
})

describe('doneForYou', () => {
  it('shows only calls that went through, in the last 24 hours, newest first', () => {
    const d = doneForYou([
      rec({ id: 1, at: '2026-11-19T13:00:00Z', target: 'a' }),
      rec({ id: 2, at: '2026-11-19T14:00:00Z', target: 'b' }),
      rec({ id: 3, outcome: 'failed', target: 'c' }),
      rec({ id: 4, outcome: 'refused', target: 'd' }),
      rec({ id: 5, at: '2026-11-18T10:00:00Z', target: 'e' }),
    ], NOW)
    expect(d.rows.map(r => r.id)).toEqual(['rec-2', 'rec-1'])
    expect(d.done).toBe(2)
  })

  it('folds repeats of one action on one target', () => {
    const d = doneForYou([rec({ id: 1, action: 'plan_manifest', target: 'plan_manifest' }), rec({ id: 2, action: 'plan_manifest', target: 'plan_manifest', at: '2026-11-19T14:20:00Z' })], NOW)
    expect(d.rows).toHaveLength(1)
    expect(d.rows[0].title).toBe('plan_manifest · plan_manifest ×2')
    expect(d.done).toBe(2)
  })

  it('says when no undo was written and nothing was checked after (as on PROD that day)', () => {
    const d = doneForYou([rec({})], NOW)
    expect(d.rows[0].undo).toBe('No undo written')
    expect(d.rows[0].check).toBe('Not checked after')
    expect(d.rows[0].tone).toBe('warn')
    expect(doneSummary(d)).toBe('1 action in the last 24 h · undo written for 0 · checked after for 0')
  })

  it('tells checked, written and missing undo apart', () => {
    expect(undoText({ regret_state: 'ready', regret: 'gitops_rollback_app platform-stg' })).toEqual({ text: 'Undo checked: gitops_rollback_app platform-stg', tone: 'ok' })
    expect(undoText({ regret_state: 'declared', regret: 'apply the previous file' }).tone).toBe('info')
    expect(undoText({ regret_state: 'undeclared', regret: '' }).text).toBe('No undo written')
  })

  it('a reason given is shown; the placeholder for none is said plainly', () => {
    expect(doneForYou([rec({ reason: 'hold' })], NOW).rows[0].detail).toContain('hold')
    expect(doneForYou([rec({})], NOW).rows[0].detail).toContain('no reason given')
  })

  it('a record with more pages makes the count a floor', () => {
    expect(doneSummary(doneForYou([rec({})], NOW, true))).toMatch(/^At least 1 action/)
  })
})
