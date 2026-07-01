/** Static snapshot of Ops Console UI implementation — S11: nav-derived + Owner overrides. */

import { getAllNavItems } from '@bifrost/ui'
import { CONSOLE_NAV_GROUPS, CONSOLE_NAV_PLANE_BY_TAB } from '@/lib/consoleNavConfig'
import { UI_PROGRESS_OVERRIDES } from '@/lib/briefing/uiProgressOverrides'

export type UiItemStatus = 'done' | 'partial' | 'planned'

export interface UiProgressItem {
  area: string
  item: string
  status: UiItemStatus
  notes: string
}

/** S11: derive UI progress rows from sidebar nav registry + Owner overrides. */
export function deriveConsoleUiProgress(): UiProgressItem[] {
  const items: UiProgressItem[] = []
  const seen = new Set<string>()

  for (const group of CONSOLE_NAV_GROUPS) {
    for (const navItem of getAllNavItems(group)) {
      if (seen.has(navItem.id)) continue
      seen.add(navItem.id)
      const override = UI_PROGRESS_OVERRIDES[navItem.id]
      items.push({
        area: CONSOLE_NAV_PLANE_BY_TAB[navItem.id] ?? group.label,
        item: navItem.label,
        status: override?.status ?? 'planned',
        notes: override?.notes ?? `Console tab: ${navItem.id}`,
      })
    }
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
      status: 'partial',
      notes: 'ensure namespaces, rollout restart, scale, cordon/drain/join',
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
