import { describe, expect, it } from 'vitest'
import type { PatrolRun } from '@/api/patrol'
import { patrolRunLogText } from '@/lib/patrol/runLog'

function run(partial: Partial<PatrolRun>): PatrolRun {
  return {
    id: 'r1',
    skill_id: 'fleet-drift-scan',
    skill_name: 'Fleet Drift Scan',
    trigger: 'cron',
    started_at: '2026-08-10T03:00:03Z',
    result: 'failure',
    ...partial,
  }
}

describe('patrolRunLogText', () => {
  it('prefers error then evidence', () => {
    expect(patrolRunLogText(run({ error: 'boom', evidence: 'called MCP' }))).toBe('boom\n\ncalled MCP')
    expect(patrolRunLogText(run({ error: 'boom' }))).toBe('boom')
    expect(patrolRunLogText(run({ evidence: 'ok' }))).toBe('ok')
    expect(patrolRunLogText(run({}))).toBe('')
  })
})
