import { describe, expect, it } from 'vitest'
import { parsePlanExplain } from '@/api/planExplainSchema'
import { approvalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { waitWords } from '../waitWords'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { AT, r5Raw } from './moduleDetailFixture'

const base = { ...parsePlanSnapshot(r5Raw(), 'R5').tasks[0], state: 'ready', on_main: undefined }
const now = Date.parse(AT) + 120_000

describe('shared wait words', () => {
  it.each(['owner', 'checks', 'landing', 'stop', 'host', 'queued', 'dependencies', 'rework', 'blocked', 'working'] as const)('words %s with and without a since', on => {
    const task = { ...base, waiting: { on, reason: 'recorded reason', question_id: 'q_1', checks: [{ task_id: 't_123456', role: 'review', state: 'ready' }] } }
    const expected = { owner: 'Waits on the Owner · question q_1', checks: 'Waits on checks: review t_…123456 ready', landing: 'Waits on landing:', stop: 'Stopped by Unknown: recorded reason', host: 'Waits for an executor slot · recorded reason', queued: 'Waits for an executor slot · recorded reason', dependencies: 'Waits on dependencies · recorded reason', rework: 'recorded reason', blocked: 'recorded reason', working: 'recorded reason' }[on]
    expect(waitWords(task, undefined, now).text).toContain(expected)
    expect(waitWords(task, undefined, now).text).toContain('since Unknown')
    const timed = waitWords({ ...task, waiting: { ...task.waiting, since: AT } }, undefined, now)
    expect(timed.age).toBe(2)
    expect(timed.text).toContain(`since ${new Date(AT).toTimeString().slice(0, 5)} · 2 min`)
    if (on === 'owner') expect(timed.href).toBe(questionHref('q_1'))
  })

  it.each(['landing_pending', 'landing_main_unsettled'])('links a matching %s grant and includes the CI run', state => {
    const task = { ...base, waiting: { on: 'landing' as const, reason: 'pending' }, landing: { stream: '', operation: 'op', phase: 'checking', outcome: '', reason: '', attempt: '', result: '', main_commit: '', run: '123', observed_at: AT, basis: '' } }
    const blocker = { atom: 'step_landed', state, task_id: task.task_id, op: 'op', phase: 'checking', last_record_at: AT, grant: { approval_id: 'appr_1', number: 1, last_status: 'approved', recorded_at: AT }, cards: [] }
    const explain = parsePlanExplain({ status: 'ok', read_at: AT, steps: { step: { node_version: 1, kind: 'explained', rule: 'false', blockers: [blocker] } } })
    const words = waitWords(task, explain, now)
    expect(words.text).toContain('checking · CI run 123 · Request #1 · approved')
    expect(words.href).toBe(approvalHref('appr_1'))
    expect(waitWords({ ...task, task_id: 'other' }, explain, now).href).toBe('')
  })
})

it('uses existing dependency, slot and executor fields without changing the API', () => {
  expect(waitWords({ ...base, waiting: { on: 'dependencies', reason: '', depends_on: [{ task_id: 't_123456', title: 'Prepare inputs', state: 'running' }] } }, undefined, now).text).toContain('Prepare inputs (t_…123456, running)')
  expect(waitWords({ ...base, waiting: { on: 'landing', reason: '', slot_holder: { operation: 'op', task_id: 't_123456', title: 'Current batch', phase: 'main_intent' } } }, undefined, now).text).toContain('held by Current batch (main_intent)')
  expect(waitWords({ ...base, waiting: { on: 'host', reason: '', executor: { pool: 'pool-a', executor: 'codex', host_id: 'host-a' } } }, undefined, now).text).toContain('pool-a · codex · host-a')
})
it('does not show stale waiting data on finished tasks', () => {
  const waiting = { on: 'owner' as const, reason: 'old wait' }
  for (const state of ['accepted', 'rejected', 'cancelled']) expect(waitWords({ ...base, state, waiting }).text).toBe('')
  expect(waitWords({ ...base, waiting, on_main: { commit: 'a'.repeat(40), via: 'landing' } }).text).toBe('')
})
