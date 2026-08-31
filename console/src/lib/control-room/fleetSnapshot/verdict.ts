/**
 * Fleet GO|HOLD|NO-GO verdict.
 */
import {
  type FleetCell,
  type FleetCellSignal,
  type FleetVerdict,
} from '@/lib/control-room/fleetSnapshot/types'
import { cellCountsTowardVerdict, resolveCellGate } from '@/lib/control-room/fleetSnapshot/standards'

export function severityRank(s: FleetCellSignal): number {
  switch (s) {
    case 'fail':
      return 4
    case 'degraded':
      return 3
    case 'unavailable':
      return 2
    case 'unknown':
      return 1
    default:
      return 0
  }
}

export function pickWorstCell(cells: FleetCell[]): FleetCell | null {
  let worstCell: FleetCell | null = null
  for (const c of cells) {
    if (worstCell == null || severityRank(c.signal) > severityRank(worstCell.signal)) {
      worstCell = c
    }
  }
  return worstCell
}

/**
 * Verdict rules (unavailable excluded from scoring — display only):
 * - GO: every scored cell gate is GO (all required standards green)
 * - NO-GO: any scored cell has a non-green required standard
 * Engineer fail/degraded → Primary CTA navigates to Operator Plane (not Agent Fix)
 */
export function resolveFleetVerdict(cells: FleetCell[]): FleetVerdict {
  const scored = cells.filter(cellCountsTowardVerdict)
  const worstCell = pickWorstCell(scored)
  if (worstCell == null) {
    return {
      kind: 'NO-GO',
      topReason: scored.length === 0 ? 'No scored fleet cells' : 'No fleet cells',
      primaryCta: { label: 'Open Control Room', tabId: 'control-room', kind: 'navigate' },
      worstCell: null,
    }
  }

  const anyNoGo = scored.some(c => resolveCellGate(c) === 'NO-GO')
  if (!anyNoGo) {
    return {
      kind: 'GO',
      topReason: 'All required standards green',
      primaryCta: { label: 'Fleet clear', kind: 'none' },
      worstCell: null,
    }
  }

  if (worstCell.role === 'engineer' && worstCell.escalateTabId) {
    return {
      kind: 'NO-GO',
      topReason: worstCell.detail,
      primaryCta: {
        label: 'Open Operator Plane',
        tabId: worstCell.escalateTabId,
        cellKey: worstCell.key,
        kind: 'navigate',
      },
      worstCell,
    }
  }
  return {
    kind: 'NO-GO',
    topReason: worstCell.detail,
    primaryCta: {
      label: 'Agent Fix',
      cellKey: worstCell.key,
      kind: worstCell.agentFixEnabled ? 'agent-fix' : 'navigate',
      tabId: worstCell.agentFixEnabled ? undefined : 'control-room',
    },
    worstCell,
  }
}

