import { describe, expect, it } from 'vitest'
import { getAllNavItems } from '@bifrost/ui'
import {
  buildPartnerNavSections,
  buildSeatNavItems,
  buildSeatRecordsItems,
  CONSOLE_NAV_GROUPS,
  ENGINEER_LAUNCH_ITEMS,
  MISSION_CONTROL_ITEMS,
  MISSION_CONTROL_RECORDS_ITEMS,
  MISSION_CONTROL_RECORDS_LABEL,
} from '@/lib/consoleNavConfig'
import { resolveTaskModeId } from '@/lib/task-mode/taskModeCatalog'

describe('Seat / Partner zone builders', () => {
  it('system seat shows pinned Mission Control without Defects/Audit', () => {
    const items = buildSeatNavItems(null, false)
    expect(items.map(i => i.id)).toEqual(MISSION_CONTROL_ITEMS.map(i => i.id))
    expect(items.map(i => i.id)).toEqual(['approvals', 'control-room', 'observability', 'code-health'])
    expect(buildSeatRecordsItems(null).map(i => i.id)).toEqual(
      MISSION_CONTROL_RECORDS_ITEMS.map(i => i.id),
    )
    expect(MISSION_CONTROL_RECORDS_LABEL).toBe('Defects & Audit')
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

  it('the retired Build lens opens System: no Build Desk tabs anywhere', () => {
    expect(resolveTaskModeId('build')).toBe('system')
    const partner = buildPartnerNavSections(null)
    const ids = [
      ...(partner?.launch ?? []).flatMap(i => [i.id, ...(i.children ?? []).map(c => c.id)]),
      ...(partner?.workspace ?? []).map(i => i.id),
      ...(partner?.profile ?? []).map(i => i.id),
    ]
    for (const retired of ['briefing', 'active-session', 'delivery-board', 'briefing-reconciliation']) {
      expect(ids).not.toContain(retired)
    }
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
    expect(pluginGroup?.subGroups?.[0]?.items.map(i => i.id)).not.toContain('analytics-pipeline')
    expect(pluginGroup?.subGroups?.[0]?.items.map(i => i.id)).toEqual([
      'plugin-gallery',
      'ib-gateway-manage',
      'market-data-manage',
      'flex-query-manage',
    ])
    expect(pluginGroup?.subGroups?.[1]?.items.map(i => i.id)).toEqual(['network'])
    expect(CONSOLE_NAV_GROUPS[0].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[1].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[2].defaultOpen).toBe(true)
    expect(CONSOLE_NAV_GROUPS[2].emphasis).toBeUndefined()
    expect(CONSOLE_NAV_GROUPS[2].dividerBefore).toBeUndefined()
  })
})
