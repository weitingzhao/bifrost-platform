import { describe, expect, it } from 'vitest'
import { CONSOLE_NAV_GROUPS, buildPartnerNavSections } from '@/lib/consoleNavConfig'
import { resolveTaskModeId, taskModeById } from '@/lib/task-mode/taskModeCatalog'
import {
  allNavTabIds,
  buildTaskNavGroups,
  phaseRelevantTabIds,
  resolveAllowedTabIds,
  resolveTaskPhaseStatus,
} from '@/lib/task-mode/navLens'

describe('buildTaskNavGroups command hierarchy', () => {
  it('system mode keeps Mission+Support groups and never injects Mission Control or TCC', () => {
    const groups = buildTaskNavGroups('system', CONSOLE_NAV_GROUPS)
    expect(groups.map(g => g.label)).toEqual([
      'Satellite',
      'Rocket',
      'Plugin',
    ])
    const ids = allNavTabIds(groups)
    expect(ids).not.toContain('task-cc')
    expect(ids).not.toContain('control-room')
    expect(resolveAllowedTabIds('system')).toBeNull()
  })

  it('ops navGroups keep observe tabs; launch tabs live on Engineer Launch Desk', () => {
    const groups = buildTaskNavGroups('ops', CONSOLE_NAV_GROUPS)
    const ids = allNavTabIds(groups)
    expect(ids).not.toContain('plugin-release')
    expect(ids).not.toContain('platform-release')
    expect(ids).not.toContain('trade-release')
    expect(ids).toContain('cluster')
    expect(ids).toContain('satellite-bus')
    expect(ids).not.toContain('task-cc')
    expect(ids).not.toContain('control-room')
    expect(ids).not.toContain('observability')
    expect(ids).not.toContain('defects')
    expect(groups.map(g => g.label)).toEqual(['Satellite', 'Rocket'])
    const partner = buildPartnerNavSections(resolveAllowedTabIds('ops'))
    expect(partner?.launch.map(i => i.id)).toEqual([
      'platform-release',
      'satellite-launch',
      'plugin-release',
      'agent-release',
    ])
    expect(partner?.launch.find(i => i.id === 'satellite-launch')?.children?.map(c => c.id)).toEqual([
      'trade-release',
      'research-release',
    ])
  })
})

describe('nav lens includeTabs', () => {
  it('ops keeps Queue desk + operator-plane + patrol + launch tabs with TCC/CR', () => {
    const allowed = resolveAllowedTabIds('ops')
    expect(allowed?.has('queue')).toBe(true)
    expect(allowed?.has('operator-plane')).toBe(true)
    expect(allowed?.has('autonomous-skills')).toBe(true)
    expect(allowed?.has('execution-log')).toBe(true)
    expect(allowed?.has('agent-governance')).toBe(true)
    expect(allowed?.has('agent-capability')).toBe(true)
    expect(allowed?.has('platform-release')).toBe(true)
    expect(allowed?.has('research-release')).toBe(true)
    expect(allowed?.has('agent-release')).toBe(true)
    expect(allowed?.has('rocket-health')).toBe(true)
    expect(allowed?.has('task-cc')).toBe(true)
    expect(allowed?.has('control-room')).toBe(true)
    expect(allowed?.has('analysis-workspace')).toBe(false)
  })

  it('analysis includeTabs is TCC + Analysis Desk + control-room', () => {
    expect(taskModeById('analysis').loopArchetype).toBe('analysis')
    const allowed = resolveAllowedTabIds('analysis')
    expect(allowed).toEqual(
      new Set([
        'task-cc',
        'analysis-workspace',
        'insight-log',
        'hermes-status',
        'control-room',
      ]),
    )
    expect(phaseRelevantTabIds('analysis', 'review-insights')?.has('analysis-workspace')).toBe(true)
    expect(phaseRelevantTabIds('analysis', 'verify')?.has('insight-log')).toBe(true)
  })
})

describe('legacy task mode aliases', () => {
  it('maps daily-ops / mission-launch / patrol → ops', () => {
    expect(resolveTaskModeId('daily-ops')).toBe('ops')
    expect(resolveTaskModeId('mission-launch')).toBe('ops')
    expect(resolveTaskModeId('patrol')).toBe('ops')
    expect(resolveTaskModeId('ops')).toBe('ops')
  })

  it('maps the retired Build lens ids → system', () => {
    expect(resolveTaskModeId('build')).toBe('system')
    expect(resolveTaskModeId('rocket-build')).toBe('system')
    expect(resolveTaskModeId('plugin-build')).toBe('system')
  })
})

describe('resolveTaskPhaseStatus ops patrol phase', () => {
  it('patrol is active when there are no live runs (Idle)', () => {
    expect(
      resolveTaskPhaseStatus('ops', 'patrol', {
        snapshot: { missionOverall: 'ok' } as never,
        patrolRuns: [],
      }),
    ).toBe('active')
  })

  it('patrol is done after a successful last run when fleet is ok', () => {
    const status = resolveTaskPhaseStatus('ops', 'patrol', {
      snapshot: { missionOverall: 'ok' } as never,
      patrolRuns: [
        {
          id: 'r1',
          skill_id: 'fleet-drift-scan',
          skill_name: 'Fleet drift scan',
          trigger: 'cron',
          started_at: '2026-08-09T12:00:00.000Z',
          finished_at: '2026-08-09T12:01:00.000Z',
          result: 'success',
        },
      ],
    })
    expect(status).toBe('done')
  })
})
