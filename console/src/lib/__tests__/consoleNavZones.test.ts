import { describe, expect, it } from 'vitest'
import { getAllNavItems } from '@bifrost/ui'
import {
  buildPartnerNavSections,
  buildSeatNavItems,
  CONSOLE_NAV_GROUPS,
  ENGINEER_LAUNCH_ITEMS,
  ENGINEER_LIFECYCLE_ITEMS,
  ENGINEER_PROFILE_ITEMS,
  ENGINEER_WORKSPACE_ITEMS,
  MISSION_CONTROL_ITEMS,
} from '@/lib/consoleNavConfig'
import { resolveAllowedTabIds } from '@/lib/task-mode/navLens'
import { resolveTaskModeId } from '@/lib/task-mode/taskModeCatalog'

describe('Seat / Partner zone builders', () => {
  it('system seat shows all Mission Control items without TCC', () => {
    const items = buildSeatNavItems(null, false)
    expect(items.map(i => i.id)).toEqual(MISSION_CONTROL_ITEMS.map(i => i.id))
    expect(buildPartnerNavSections(null)?.lifecycle.map(i => i.id)).toEqual(
      ENGINEER_LIFECYCLE_ITEMS.map(i => i.id),
    )
    expect(buildPartnerNavSections(null)?.launch.map(i => i.id)).toEqual(
      ENGINEER_LAUNCH_ITEMS.map(i => i.id),
    )
  })

  it('ops seat is TCC + control-room + observability + defects; partner is Launch + Ops Desk', () => {
    const allowed = resolveAllowedTabIds('ops')
    const items = buildSeatNavItems(allowed, true)
    expect(items.map(i => i.id)).toEqual([
      'task-cc',
      'control-room',
      'observability',
      'defects',
    ])
    const partner = buildPartnerNavSections(allowed)
    expect(partner?.lifecycle).toEqual([])
    expect(partner?.launch.map(i => i.id)).toEqual([
      'platform-release',
      'trade-release',
      'plugin-release',
    ])
    expect(partner?.workspace.map(i => i.id)).toEqual([
      'queue',
      'autonomous-skills',
      'execution-log',
      'operator-plane',
      'agent-governance',
      'agent-capability',
    ])
    expect(partner?.profile).toEqual([])
  })

  it('build seat is TCC + control-room; partner keeps Build Desk + Queue', () => {
    const allowed = resolveAllowedTabIds('build')
    expect(buildSeatNavItems(allowed, true).map(i => i.id)).toEqual([
      'task-cc',
      'control-room',
    ])
    const partner = buildPartnerNavSections(allowed)
    expect(partner?.lifecycle.map(i => i.id)).toEqual([
      'briefing',
      'active-session',
      'delivery-board',
      'dev-sessions',
    ])
    expect(partner?.launch).toEqual([])
    expect(partner?.workspace.map(i => i.id)).toEqual(['queue'])
    expect(ENGINEER_LIFECYCLE_ITEMS.find(i => i.id === 'dev-sessions')?.label).toBe('Dev Sessions')
    expect(partner?.profile).toEqual([])
  })

  it('Build Desk sidebar labels are Briefing → In Flight → Delivery → Dev Sessions', () => {
    expect(ENGINEER_LIFECYCLE_ITEMS.map(i => i.label)).toEqual([
      'Briefing',
      'In Flight',
      'Delivery',
      'Dev Sessions',
    ])
  })

  it('Launch Desk sidebar labels are Rocket → Satellite → Plugin', () => {
    expect(ENGINEER_LAUNCH_ITEMS.map(i => i.label)).toEqual([
      'Rocket',
      'Satellite',
      'Plugin',
    ])
  })

  it('puts queue on Ops Desk (workspace) Partner section', () => {
    expect(ENGINEER_WORKSPACE_ITEMS.map(i => i.id)).toEqual([
      'queue',
      'autonomous-skills',
      'execution-log',
      'operator-plane',
      'agent-governance',
      'agent-capability',
    ])
    expect(ENGINEER_LIFECYCLE_ITEMS.some(i => i.id === 'queue')).toBe(false)
    expect(ENGINEER_PROFILE_ITEMS.some(i => i.id === 'queue')).toBe(false)
    const ops = buildPartnerNavSections(resolveAllowedTabIds('ops'))
    expect(ops?.workspace.some(i => i.id === 'queue')).toBe(true)
  })

  it('Analysis Desk has workspace + insight log + hermes status', () => {
    expect(ENGINEER_PROFILE_ITEMS.map(i => i.id)).toEqual([
      'analysis-workspace',
      'insight-log',
      'hermes-status',
    ])
    const analysis = buildPartnerNavSections(resolveAllowedTabIds('analysis'))
    expect(analysis?.lifecycle).toEqual([])
    expect(analysis?.launch).toEqual([])
    expect(analysis?.workspace).toEqual([])
    expect(analysis?.profile.map(i => i.id)).toEqual([
      'analysis-workspace',
      'insight-log',
      'hermes-status',
    ])
  })

  it('legacy task mode ids resolve to ops', () => {
    expect(resolveTaskModeId('daily-ops')).toBe('ops')
    expect(resolveTaskModeId('mission-launch')).toBe('ops')
    expect(resolveTaskModeId('patrol')).toBe('ops')
  })

  it('remaining nav groups are Satellite → Rocket → Plugin (Network under Plugin)', () => {
    expect(CONSOLE_NAV_GROUPS.map(g => g.label)).toEqual([
      'Satellite',
      'Rocket',
      'Plugin',
    ])
    const missionIds = CONSOLE_NAV_GROUPS.flatMap(g => getAllNavItems(g).map(i => i.id))
    expect(missionIds).not.toContain('platform-release')
    expect(missionIds).not.toContain('trade-release')
    expect(missionIds).not.toContain('plugin-release')
    expect(missionIds).toContain('network')
    expect(missionIds).toContain('plugin-gallery')
    const pluginGroup = CONSOLE_NAV_GROUPS.find(g => g.label === 'Plugin')
    expect(pluginGroup?.subGroups?.map(sg => sg.label)).toEqual(['', 'Infra'])
    expect(pluginGroup?.subGroups?.[1]?.items.map(i => i.id)).toEqual(['network'])
    expect(CONSOLE_NAV_GROUPS[0].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[1].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[2].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[2].emphasis).toBeUndefined()
    expect(CONSOLE_NAV_GROUPS[2].dividerBefore).toBeUndefined()
  })
})
