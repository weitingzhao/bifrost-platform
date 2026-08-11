/** Static snapshot of Ops Console UI implementation — S11: nav-derived + Owner overrides. */

import { getAllNavItems, type ShellNavItem } from '@bifrost/ui'
import {
  CONSOLE_NAV_GROUPS,
  CONSOLE_NAV_PLANE_BY_TAB,
  ENGINEER_LAUNCH_ITEMS,
  ENGINEER_LIFECYCLE_ITEMS,
  ENGINEER_PROFILE_ITEMS,
  ENGINEER_WORKSPACE_ITEMS,
  MISSION_CONTROL_ITEMS,
  TASK_CC_NAV_ITEM,
} from '@/lib/consoleNavConfig'
import { UI_PROGRESS_OVERRIDES } from '@/lib/briefing/uiProgressOverrides'

export type UiItemStatus = 'done' | 'partial' | 'planned'

export interface UiProgressItem {
  area: string
  item: string
  status: UiItemStatus
  notes: string
}

function pushNavProgress(
  items: UiProgressItem[],
  seen: Set<string>,
  navItems: readonly ShellNavItem[],
  areaFallback: string,
) {
  for (const navItem of navItems) {
    if (seen.has(navItem.id)) continue
    seen.add(navItem.id)
    const override = UI_PROGRESS_OVERRIDES[navItem.id]
    items.push({
      area: CONSOLE_NAV_PLANE_BY_TAB[navItem.id] ?? areaFallback,
      item: navItem.label,
      status: override?.status ?? 'planned',
      notes: override?.notes ?? `Console tab: ${navItem.id}`,
    })
  }
}

/** S11: derive UI progress rows from sidebar nav registry + Owner overrides. */
export function deriveConsoleUiProgress(): UiProgressItem[] {
  const items: UiProgressItem[] = []
  const seen = new Set<string>()

  pushNavProgress(items, seen, [TASK_CC_NAV_ITEM, ...MISSION_CONTROL_ITEMS], 'Mission Control')
  pushNavProgress(items, seen, ENGINEER_LIFECYCLE_ITEMS, 'Engineer')
  pushNavProgress(items, seen, ENGINEER_LAUNCH_ITEMS, 'Engineer')
  pushNavProgress(items, seen, ENGINEER_WORKSPACE_ITEMS, 'Engineer')
  pushNavProgress(items, seen, ENGINEER_PROFILE_ITEMS, 'Engineer')

  for (const group of CONSOLE_NAV_GROUPS) {
    pushNavProgress(items, seen, getAllNavItems(group), group.label)
  }

  items.push(
    {
      area: 'Platform API',
      item: 'L0 probes',
      status: 'done',
      notes: 'matrix, topology, context, cluster, gitops, delivery, audit, briefing/session-pack',
    },
    {
      area: 'Platform API',
      item: 'L1 cluster actuation',
      status: 'done',
      notes: 'ensure namespaces, rollout restart, scale, cordon/drain/join — audited via GET /api/v1/audit',
    },
    {
      area: 'Platform API',
      item: 'P5 MCP catalog',
      status: 'done',
      notes: 'GET /mcp/tools incl. get_session_briefing + close_briefing_session',
    },
  )

  return items
}

export const CONSOLE_UI_PROGRESS: UiProgressItem[] = deriveConsoleUiProgress()

export function formatUiProgressSection(): string {
  const lines = ['## Ops Console UI progress (snapshot)', '']
  let lastArea = ''
  for (const row of CONSOLE_UI_PROGRESS) {
    if (row.area !== lastArea) {
      lines.push(`### ${row.area}`)
      lastArea = row.area
    }
    lines.push(`- [${row.status.toUpperCase()}] ${row.item} — ${row.notes}`)
  }
  return lines.join('\n')
}

/** Optional live placement violations — append to briefing when placement API is available. */
export function formatPlacementViolationsSummary(
  violations: { severity: string; message: string }[] | undefined,
): string {
  if (violations == null || violations.length === 0) {
    return '## Placement violations\n\n0 critical (GET /api/v1/cluster/placement)'
  }
  const lines = ['## Placement violations', '']
  for (const v of violations) {
    lines.push(`- [${v.severity}] ${v.message}`)
  }
  return lines.join('\n')
}
