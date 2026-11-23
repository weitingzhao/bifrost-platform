import { describe, expect, it } from 'vitest'
import { patrolVerdict } from '../patrolVerdict'
import type { PatrolSkill } from '../readings'

// Origin (2026-11-23, Console line's design-system inventory, confirmed by 箭10): Records → Patrol drew a fixed
// ok lamp (StatusLamp value="ok") whatever the skills said, an "OK for a thing never checked". The head line now
// comes from the skills' last runs.
const skill = (id: string, over: Partial<PatrolSkill> = {}): PatrolSkill => ({
  id, name: id, description: '', schedule: '', prompt_template: '', mcp_tools: [], trust_level: 'L0' as PatrolSkill['trust_level'],
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
