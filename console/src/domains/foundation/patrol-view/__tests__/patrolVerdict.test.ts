import { describe, expect, it } from 'vitest'
import { effectiveNextRun, overdueOf, patrolVerdict, scheduleIntervalMs } from '../patrolVerdict'
import type { PatrolSkill } from '../readings'

// Origin (2026-11-23, Console line's design-system inventory, confirmed by 箭10): Records → Patrol drew a fixed
// ok lamp (StatusLamp value="ok") whatever the skills said, an "OK for a thing never checked". The head line now
// comes from the skills' last runs.
const skill = (id: string, over: Partial<PatrolSkill> = {}): PatrolSkill => ({
  id, name: id, description: '', schedule: '*/15 * * * *', prompt_template: '', mcp_tools: [], trust_level: 'L0' as PatrolSkill['trust_level'],
  scope: '', timeout_seconds: 60, enabled: true, ...over,
})

describe('patrolVerdict', () => {
  it('is never ok before anything was read', () => {
    expect(patrolVerdict(undefined, true, null).word).toBe('pending')
    expect(patrolVerdict(undefined, false, 'HTTP 503')).toEqual({ word: 'unknown', sentence: 'Skills could not be read: HTTP 503' })
  })

  it('is the worst enabled last result; a skill that never ran cannot be judged', () => {
    expect(patrolVerdict([skill('a', { last_result: 'success' }), skill('b', { last_result: 'success' })], false, null).word).toBe('ok')
    expect(patrolVerdict([skill('a', { last_result: 'success' }), skill('b')], false, null).word).toBe('unknown')
    expect(patrolVerdict([skill('a', { last_result: 'escalated' }), skill('b', { last_result: 'failure' })], false, null).word).toBe('fail')
    expect(patrolVerdict([skill('a', { last_result: 'failure', enabled: false }), skill('b', { last_result: 'success' })], false, null).word).toBe('ok')
  })

  it('says so when no skill is enabled', () => {
    expect(patrolVerdict([skill('a', { enabled: false })], false, null).word).toBe('boundary')
  })
})

// 2026-11-23 (箭10's rule, after the screenshot showed two skills "overdue 8m" under an ok lamp): the grace is the
// skill's own schedule interval: late by less than one interval is warn, by more is fail (older than its declared
// freshness), no schedule declared is unknown.
describe('overdue', () => {
  const NOW = Date.parse('2026-11-23T20:00:00Z')
  const every15 = (late: number, over: Partial<PatrolSkill> = {}) =>
    skill('autopilot', { schedule: '*/15 * * * *', last_result: 'success', next_run_at: new Date(NOW - late).toISOString(), ...over })

  it('reads the interval from the cron', () => {
    expect(scheduleIntervalMs('*/15 * * * *')).toBe(15 * 60_000)
    expect(scheduleIntervalMs('5 * * * *')).toBe(3_600_000)
    expect(scheduleIntervalMs('0 6 * * 1')).toBe(7 * 86_400_000)
    expect(scheduleIntervalMs('not a cron')).toBeNull()
  })

  it('before the next run says nothing; late under one interval is warn, past it is fail', () => {
    expect(overdueOf(every15(-60_000), NOW)).toBeNull()
    expect(overdueOf(every15(8 * 60_000), NOW)).toEqual({ word: 'warn', note: 'autopilot overdue 8m (schedule every 15m)' })
    expect(overdueOf(every15(16 * 60_000), NOW)?.word).toBe('fail')
    expect(overdueOf(every15(8 * 60_000, { schedule: '' }), NOW)).toEqual({ word: 'unknown', note: 'autopilot: no schedule declared' })
  })

  // PROD 2026-11-23 14:21: Ops Autopilot last ran 14:15 but its reported next run was still 14:00; it is on time.
  it('a reported next run older than the last run is not trusted: due one interval after the last run', () => {
    const lastRun = new Date(NOW - 6 * 60_000).toISOString()
    expect(overdueOf(every15(21 * 60_000, { last_run_at: lastRun }), NOW)).toBeNull()
    const longAgo = new Date(NOW - 40 * 60_000).toISOString()
    expect(overdueOf(every15(41 * 60_000, { last_run_at: longAgo }), NOW)?.word).toBe('fail')
  })

  it('the Next run column shows the corrected time and says it was estimated', () => {
    const s = every15(21 * 60_000, { last_run_at: new Date(NOW - 6 * 60_000).toISOString() })
    expect(effectiveNextRun(s)).toEqual({ at: new Date(NOW + 9 * 60_000).toISOString(), stale: true })
    expect(effectiveNextRun(every15(-60_000))?.stale).toBe(false)
  })

  it('counts in the lamp: all success but one late is warn, with the reason in the sentence', () => {
    const v = patrolVerdict([skill('a', { last_result: 'success' }), every15(8 * 60_000)], false, null, NOW)
    expect(v.word).toBe('warn')
    expect(v.sentence).toContain('overdue 8m (schedule every 15m)')
  })
})
