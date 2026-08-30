import { describe, expect, it } from 'vitest'
import type { ProgramDetailResponse, ProgramSummary } from '@/api/programsTypes'
import { CONSOLE_NAV_GROUPS, buildPartnerNavSections } from '@/lib/consoleNavConfig'
import { resolveTaskModeId, taskModeById } from '@/lib/task-mode/taskModeCatalog'
import {
  allNavTabIds,
  buildTaskNavGroups,
  phaseRelevantTabIds,
  resolveAllowedTabIds,
  resolveTaskPhaseStatus,
} from '@/lib/task-mode/navLens'

function detail(partial: Partial<ProgramSummary>): ProgramDetailResponse {
  return {
    program: {
      id: partial.id ?? 'p',
      title: 'P',
      description: '',
      status: 'active',
      phase_count: partial.phase_count ?? 9,
      phases_done: partial.phases_done ?? 9,
      all_phases_done: true,
      active: false,
      ...partial,
    },
    phases: [],
    active: false,
  }
}

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
    expect(ids).not.toContain('briefing')
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

  it('build navGroups have no Mission Control or Engineer items', () => {
    const groups = buildTaskNavGroups('build', CONSOLE_NAV_GROUPS)
    const ids = allNavTabIds(groups)
    expect(ids).not.toContain('task-cc')
    expect(ids).not.toContain('control-room')
    expect(ids).not.toContain('briefing')
    expect(groups).toEqual([])
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
    expect(allowed?.has('briefing')).toBe(false)
    expect(allowed?.has('analysis-workspace')).toBe(false)
  })

  it('build keeps Build Desk four + TCC + queue', () => {
    const allowed = resolveAllowedTabIds('build')
    expect(allowed?.has('briefing')).toBe(true)
    expect(allowed?.has('active-session')).toBe(true)
    expect(allowed?.has('delivery-board')).toBe(true)
    expect(allowed?.has('dev-sessions')).toBe(true)
    expect(allowed?.has('queue')).toBe(true)
    expect(allowed?.has('task-cc')).toBe(true)
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
})

describe('build phaseRelevantTabs', () => {
  it('keeps Active Session on implement/pre-push; Briefing on plan; Board on close', () => {
    expect(phaseRelevantTabIds('build', 'implement')?.has('active-session')).toBe(true)
    expect(phaseRelevantTabIds('build', 'pre-push')?.has('active-session')).toBe(true)
    expect(phaseRelevantTabIds('build', 'briefing')?.has('briefing')).toBe(true)
    expect(phaseRelevantTabIds('build', 'deliver-stg')?.has('delivery-board')).toBe(true)
    expect(phaseRelevantTabIds('build', 'sign-off')?.has('delivery-board')).toBe(true)
  })

  it('TCC implement/pre-push deep-link to In Flight', () => {
    const build = taskModeById('build')
    const implement = build.phases?.find(p => p.id === 'implement')
    const prePush = build.phases?.find(p => p.id === 'pre-push')
    expect(implement?.navigateTab).toBe('active-session')
    expect(prePush?.navigateTab).toBe('active-session')
    expect(implement?.actions?.find(a => a.tabId === 'active-session')?.label).toBe('In Flight')
    expect(prePush?.actions?.some(a => a.tabId === 'active-session')).toBe(true)
  })
})

describe('resolveTaskPhaseStatus build playbook', () => {
  it('does not mark deliver-stg done when catalog is not closed (req_pc)', () => {
    const status = resolveTaskPhaseStatus('build', 'deliver-stg', {
      programDetail: detail({
        complete: true,
        signed: 9,
        sign_off_required_count: 9,
        requires_post_completion: true,
      }),
    })
    expect(status).not.toBe('done')
  })

  it('does not mark sign-off done when gates are unsigned', () => {
    const status = resolveTaskPhaseStatus('build', 'sign-off', {
      programDetail: detail({
        complete: false,
        signed: 0,
        sign_off_required_count: 9,
        phases_done: 9,
        requires_post_completion: true,
      }),
      briefingOpened: true,
      devAgentPhaseDone: () => true,
    })
    expect(status).not.toBe('done')
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
