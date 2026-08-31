import { describe, expect, it } from 'vitest'
import { getAllNavItems } from '@bifrost/ui'
import {
  buildPartnerNavSections,
  buildSeatNavItems,
  buildSeatRecordsItems,
  CONSOLE_NAV_GROUPS,
  ENGINEER_LAUNCH_ITEMS,
  ENGINEER_LIFECYCLE_ITEMS,
  ENGINEER_PROFILE_ITEMS,
  ENGINEER_WORKSPACE_ITEMS,
  ENGINEER_WORKSPACE_SUBGROUPS,
  MISSION_CONTROL_ITEMS,
  MISSION_CONTROL_RECORDS_ITEMS,
  MISSION_CONTROL_RECORDS_LABEL,
} from '@/lib/consoleNavConfig'
import { resolveAllowedTabIds } from '@/lib/task-mode/navLens'
import { resolveTaskModeId } from '@/lib/task-mode/taskModeCatalog'

describe('Seat / Partner zone builders', () => {
  it('system seat shows pinned Mission Control without Defects/Audit', () => {
    const items = buildSeatNavItems(null, false)
    expect(items.map(i => i.id)).toEqual(MISSION_CONTROL_ITEMS.map(i => i.id))
    expect(items.map(i => i.id)).toEqual(['control-room', 'observability', 'code-health'])
    expect(buildSeatRecordsItems(null).map(i => i.id)).toEqual(
      MISSION_CONTROL_RECORDS_ITEMS.map(i => i.id),
    )
    expect(MISSION_CONTROL_RECORDS_LABEL).toBe('Defects & Audit')
    expect(buildPartnerNavSections(null)?.lifecycle.map(i => i.id)).toEqual(
      ENGINEER_LIFECYCLE_ITEMS.map(i => i.id),
    )
    expect(buildPartnerNavSections(null)?.launch.map(i => i.id)).toEqual([
      'platform-release',
      'satellite-launch',
      'plugin-release',
      'agent-release',
    ])
    expect(
      buildPartnerNavSections(null)?.launch.find(i => i.id === 'satellite-launch')?.children?.map(
        c => c.id,
      ),
    ).toEqual(['trade-release', 'research-release'])
  })

  it('ops seat is TCC + control-room + observability; records keep defects', () => {
    const allowed = resolveAllowedTabIds('ops')
    const items = buildSeatNavItems(allowed, true)
    expect(items.map(i => i.id)).toEqual([
      'task-cc',
      'control-room',
      'observability',
    ])
    expect(buildSeatRecordsItems(allowed).map(i => i.id)).toEqual(['defects'])
    const partner = buildPartnerNavSections(allowed)
    expect(partner?.lifecycle).toEqual([])
    expect(partner?.launch.map(i => i.id)).toEqual([
      'platform-release',
      'satellite-launch',
      'plugin-release',
      'agent-release',
    ])
    expect(partner?.launch.find(i => i.id === 'satellite-launch')?.children?.map(c => c.id)).toEqual(
      ['trade-release', 'research-release'],
    )
    expect(partner?.workspace.map(i => i.id)).toEqual([
      'queue',
      'autonomous-skills',
      'execution-log',
      'operator-plane',
      'agent-governance',
      'agent-capability',
    ])
    expect(partner?.workspaceGroups.map(g => g.label)).toEqual(['Operate', 'Patrol', ''])
    expect(partner?.workspaceGroups[0]?.items.map(i => i.id)).toEqual(['queue'])
    expect(partner?.workspaceGroups[1]?.items.map(i => i.id)).toEqual([
      'autonomous-skills',
      'execution-log',
    ])
    expect(partner?.profile).toEqual([])
  })

  it('build seat is TCC + control-room; partner keeps Build Desk + Queue', () => {
    const allowed = resolveAllowedTabIds('build')
    expect(buildSeatNavItems(allowed, true).map(i => i.id)).toEqual([
      'task-cc',
      'control-room',
    ])
    expect(buildSeatRecordsItems(allowed)).toEqual([])
    const partner = buildPartnerNavSections(allowed)
    expect(partner?.lifecycle.map(i => i.id)).toEqual([
      'briefing',
      'active-session',
      'delivery-board',
    ])
    expect(partner?.launch).toEqual([])
    expect(partner?.workspace.map(i => i.id)).toEqual(['queue'])
    expect(partner?.workspaceGroups.map(g => g.label)).toEqual(['Operate'])
    expect(ENGINEER_LIFECYCLE_ITEMS.some(i => i.id === 'dev-sessions')).toBe(false)
    expect(partner?.profile).toEqual([])
  })

  it('Build Desk sidebar labels are Briefing → In Flight → Delivery', () => {
    expect(ENGINEER_LIFECYCLE_ITEMS.map(i => i.label)).toEqual([
      'Briefing',
      'In Flight',
      'Delivery',
    ])
  })

  it('Launch Desk sidebar labels are Rocket → Satellite(Trade, Research) → Plugin → Agent', () => {
    expect(ENGINEER_LAUNCH_ITEMS.map(i => i.label)).toEqual([
      'Rocket',
      'Satellite',
      'Plugin',
      'Agent',
    ])
    expect(ENGINEER_LAUNCH_ITEMS.find(i => i.id === 'satellite-launch')?.children?.map(c => c.label)).toEqual([
      'Trade',
      'Research',
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
    expect(ENGINEER_WORKSPACE_SUBGROUPS.map(g => g.label)).toEqual(['Operate', 'Patrol', ''])
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

  it('remaining nav groups are Satellite → Rocket → Plugin (Research Engine under Satellite)', () => {
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
    expect(missionIds).toContain('research-engine')
    const satelliteGroup = CONSOLE_NAV_GROUPS.find(g => g.label === 'Satellite')
    expect(satelliteGroup?.subGroups?.[0]?.items.map(i => i.id)).toEqual([
      'satellite-bus',
      'satellite-health',
      'research-engine',
    ])
    expect(CONSOLE_NAV_GROUPS.find(g => g.label === 'Research')).toBeUndefined()
    const pluginGroup = CONSOLE_NAV_GROUPS.find(g => g.label === 'Plugin')
    expect(pluginGroup?.subGroups?.map(sg => sg.label)).toEqual(['', 'Infra'])
    expect(pluginGroup?.subGroups?.[0]?.items.map(i => i.id)).not.toContain('research-engine')
    expect(pluginGroup?.subGroups?.[1]?.items.map(i => i.id)).toEqual(['network'])
    expect(CONSOLE_NAV_GROUPS[0].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[1].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[2].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[2].emphasis).toBeUndefined()
    expect(CONSOLE_NAV_GROUPS[2].dividerBefore).toBeUndefined()
  })
})
