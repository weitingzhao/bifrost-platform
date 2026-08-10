import { describe, expect, it } from 'vitest'
import type { PatrolRun, PatrolSkill } from '@/api/patrol'
import {
  formatPatrolRelativeTime,
  latestPatrolRun,
  patrolHasFailure,
  patrolPosture,
  patrolRailSignal,
  patrolSkillsOkCount,
} from '@/lib/patrol/patrolStatus'

const SKILLS: PatrolSkill[] = [
  {
    id: 'fleet-drift-scan',
    name: 'Fleet drift scan',
    description: 'd',
    schedule: '0 3 * * *',
    prompt_template: '',
    mcp_tools: [],
    trust_level: 'L0',
    scope: 'cluster',
    timeout_seconds: 120,
    enabled: true,
    last_result: 'success',
    last_run_at: '2026-08-09T13:00:00.000Z',
    next_run_at: '2026-08-10T03:00:00.000Z',
  },
  {
    id: 'cert-expiry-check',
    name: 'Cert expiry check',
    description: 'd',
    schedule: '0 6 * * 1',
    prompt_template: '',
    mcp_tools: [],
    trust_level: 'L0',
    scope: 'platform',
    timeout_seconds: 90,
    enabled: true,
    last_result: 'success',
    last_run_at: '2026-08-08T14:00:00.000Z',
    next_run_at: '2026-08-15T06:00:00.000Z',
  },
  {
    id: 'stale-pod-cleanup',
    name: 'Stale pod cleanup',
    description: 'd',
    schedule: '0 4 * * *',
    prompt_template: '',
    mcp_tools: [],
    trust_level: 'L1',
    scope: 'cluster',
    timeout_seconds: 180,
    enabled: true,
    last_result: 'success',
    last_run_at: '2026-08-09T04:00:00.000Z',
    next_run_at: '2026-08-10T04:00:00.000Z',
  },
]

const RUNS: PatrolRun[] = [
  {
    id: 'run-fleet',
    skill_id: 'fleet-drift-scan',
    skill_name: 'Fleet drift scan',
    trigger: 'cron',
    started_at: '2026-08-09T12:59:00.000Z',
    finished_at: '2026-08-09T13:00:00.000Z',
    duration_ms: 42_100,
    result: 'success',
  },
  {
    id: 'run-cert',
    skill_id: 'cert-expiry-check',
    skill_name: 'Cert expiry check',
    trigger: 'cron',
    started_at: '2026-08-08T13:59:00.000Z',
    finished_at: '2026-08-08T14:00:00.000Z',
    duration_ms: 18_400,
    result: 'success',
  },
]

describe('patrolStatus', () => {
  it('counts success as OK for N/N skills', () => {
    expect(patrolSkillsOkCount(SKILLS)).toEqual({ ok: 3, total: 3 })
  })

  it('empty skills+runs is Idle / unknown (no fake green)', () => {
    const posture = patrolPosture([], [])
    expect(posture.label).toBe('Idle')
    expect(posture.lamp).toBe('unknown')
    expect(patrolRailSignal([])).toBeNull()
    expect(patrolHasFailure([])).toBe(false)
  })

  it('success runs yield All OK', () => {
    const posture = patrolPosture(SKILLS, RUNS)
    expect(posture.label).toBe('All OK')
    expect(posture.lamp).toBe('ok')
  })

  it('posture is WARN when a skill skipped (no failure)', () => {
    const skipped = SKILLS.map(s =>
      s.id === 'stale-pod-cleanup' ? { ...s, last_result: 'skipped' as const } : s,
    )
    const runs = RUNS.map(r =>
      r.skill_id === 'stale-pod-cleanup' ? { ...r, result: 'skipped' as const } : r,
    )
    const posture = patrolPosture(skipped, runs)
    expect(posture.label).toBe('WARN')
    expect(posture.lamp).toBe('degraded')
  })

  it('failure in last runs lights FAIL', () => {
    const failed: PatrolRun = {
      ...RUNS[0],
      id: 'run-fail',
      result: 'failure',
      error: 'probe timeout',
    }
    const posture = patrolPosture(SKILLS, [failed, ...RUNS])
    expect(patrolHasFailure([failed])).toBe(true)
    expect(posture.label).toBe('FAIL')
    expect(posture.lamp).toBe('fail')
  })

  it('running skill is WARN not All OK', () => {
    const running = SKILLS.map(s =>
      s.id === 'fleet-drift-scan' ? { ...s, last_result: 'running' as const } : s,
    )
    const runs: PatrolRun[] = [{ ...RUNS[0], result: 'running', finished_at: undefined }]
    const posture = patrolPosture(running, runs)
    expect(posture.label).toBe('WARN')
    expect(posture.lamp).toBe('degraded')
    expect(patrolRailSignal(runs)).toBe('warn')
  })

  it('rail signal: failure beats skipped/escalated', () => {
    expect(patrolRailSignal(RUNS)).toBeNull()
    expect(
      patrolRailSignal([{ ...RUNS[0], result: 'skipped' }, { ...RUNS[1], result: 'escalated' }]),
    ).toBe('warn')
    expect(
      patrolRailSignal([
        { ...RUNS[0], result: 'skipped' },
        { ...RUNS[1], id: 'f', result: 'failure' },
      ]),
    ).toBe('error')
  })

  it('formats relative hours and picks latest run', () => {
    const now = Date.parse('2026-08-09T18:00:00.000Z')
    expect(formatPatrolRelativeTime('2026-08-09T13:00:00.000Z', now)).toBe('5h ago')
    expect(latestPatrolRun(RUNS)?.skill_id).toBe('fleet-drift-scan')
  })
})
