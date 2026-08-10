/**
 * Checklist → Fleet Board virtual standard injection (union coverage).
 *
 * For each ChecklistItem with boardProjection: if the target cell has zero
 * matching probes for that item, inject a source:'checklist' standard.
 * Never double-inject when a probe already matches.
 */
import {
  DAILY_OPS_CHECKLIST,
  matchStandardToChecklistItem,
  type ChecklistItem,
  type DailyOpsChecklistStep,
} from '@/lib/control-room/dailyOpsChecklistCatalog'
import {
  cellKey,
  resolveCellGate,
  resolveFleetVerdict,
  signalFromStandards,
  type FleetCell,
  type FleetSnapshot,
  type FleetStandard,
} from '@/lib/control-room/fleetSnapshot'

/**
 * First-match-wins ownership (same as matchStandardToChecklistItem).
 * Shared placeholders like `massive-ib` are claimed by the Massive item, so IB
 * still gets a virtual chip — ensuring Checklist→Board union visibility.
 */
function itemMatchesStandard(
  step: DailyOpsChecklistStep,
  item: ChecklistItem,
  cell: FleetCell,
  standard: FleetStandard,
): boolean {
  const hit = matchStandardToChecklistItem(standard.id, standard.group, {
    role: cell.role,
    env: cell.env,
  })
  return hit?.step.id === step.id && hit.item.id === item.id
}

function cellHasProbeForItem(
  step: DailyOpsChecklistStep,
  item: ChecklistItem,
  cell: FleetCell,
): boolean {
  return cell.standards.some(
    s => (s.source ?? 'probe') === 'probe' && itemMatchesStandard(step, item, cell, s),
  )
}

export type ChecklistSignalPaint = {
  item_id: string
  signal: string
  detail?: string
}

function paintSignal(raw: string | undefined): FleetStandard['signal'] {
  switch ((raw ?? '').toLowerCase()) {
    case 'ok':
      return 'ok'
    case 'degraded':
      return 'degraded'
    case 'fail':
      return 'fail'
    default:
      return 'unknown'
  }
}

function buildVirtualStandard(
  item: ChecklistItem,
  painted?: ChecklistSignalPaint,
): FleetStandard {
  const proj = item.boardProjection!
  // Explicit required:true wins (e.g. IB observe-but-required-for-Vendor-GO).
  // Explicit required:false stays optional. Otherwise observe → not required.
  const required =
    proj.required === true
      ? true
      : proj.required === false
        ? false
        : item.fixCapability !== 'observe'
  const signal = paintSignal(painted?.signal)
  return {
    id: proj.standardId,
    label: proj.label,
    signal,
    group: proj.group,
    reason:
      painted?.detail?.trim() ||
      proj.reason ||
      `Checklist projection · ${item.label}`,
    required,
    source: 'checklist',
  }
}

/**
 * Inject checklist-only virtual standards into cells (mutates copies).
 */
export function injectChecklistVirtualStandards(
  cells: FleetCell[],
  checklistSignals?: ChecklistSignalPaint[],
): FleetCell[] {
  const byKey = new Map(cells.map(c => [c.key, { ...c, standards: [...c.standards] }]))

  for (const step of DAILY_OPS_CHECKLIST) {
    for (const item of step.items) {
      const proj = item.boardProjection
      if (proj == null) continue

      const key = cellKey(proj.cell.role, proj.cell.env)
      const cell = byKey.get(key)
      if (cell == null) continue

      if (cellHasProbeForItem(step, item, cell)) continue

      // Already injected (idempotent)
      if (cell.standards.some(s => s.id === proj.standardId && s.source === 'checklist')) {
        continue
      }

      const painted = checklistSignals?.find(s => s.item_id === item.id)
      cell.standards.push(buildVirtualStandard(item, painted))
      cell.signal = signalFromStandards(cell.standards)
      // Keep value/detail from probes; append note only in detail when virtual present
      const virtualNotes = cell.standards
        .filter(s => s.source === 'checklist')
        .map(s => s.reason)
      if (virtualNotes.length > 0) {
        const base = cell.detail.trim()
        cell.detail = base
          ? `${base} · ${virtualNotes.join(' · ')}`
          : virtualNotes.join(' · ')
      }
      byKey.set(key, cell)
    }
  }

  return cells.map(c => byKey.get(c.key) ?? c)
}

/**
 * Apply Checklist↔Board union: inject virtuals and recompute verdict.
 */
export function applyChecklistFleetUnion(
  fleet: FleetSnapshot,
  checklistSignals?: ChecklistSignalPaint[],
): FleetSnapshot {
  const cells = injectChecklistVirtualStandards(fleet.cells, checklistSignals).map(c => ({
    ...c,
    countsTowardVerdict: c.countsTowardVerdict,
  }))
  const verdict = resolveFleetVerdict(cells)
  const scoredGates = cells.filter(c => {
    const gate = resolveCellGate(c)
    return gate === 'GO' || gate === 'NO-GO'
  })
  const fleetNominal =
    scoredGates.length > 0 && scoredGates.every(c => resolveCellGate(c) === 'GO')

  return {
    ...fleet,
    cells,
    verdict,
    fleetNominal,
    fleetClear: fleetNominal,
  }
}

export type ChecklistFleetUnionAudit = {
  boardGaps: Array<{ cellKey: string; standardId: string; group: string }>
  checklistNeedsProjection: Array<{ stepId: string; itemId: string; label: string }>
  virtualCount: number
  boardGapCount: number
}

/**
 * Audit bidirectional coverage for tests / dry-run diagnostics.
 */
export function auditChecklistFleetUnion(fleet: FleetSnapshot): ChecklistFleetUnionAudit {
  const boardGaps: ChecklistFleetUnionAudit['boardGaps'] = []
  for (const cell of fleet.cells) {
    for (const s of cell.standards) {
      if (s.group === 'path') continue
      const hit = matchStandardToChecklistItem(s.id, s.group, {
        role: cell.role,
        env: cell.env,
      })
      if (hit == null) {
        boardGaps.push({ cellKey: cell.key, standardId: s.id, group: s.group })
      }
    }
  }

  const checklistNeedsProjection: ChecklistFleetUnionAudit['checklistNeedsProjection'] = []
  for (const step of DAILY_OPS_CHECKLIST) {
    for (const item of step.items) {
      let matched = 0
      for (const cell of fleet.cells) {
        for (const s of cell.standards) {
          if (itemMatchesStandard(step, item, cell, s)) matched += 1
        }
      }
      if (matched === 0) {
        checklistNeedsProjection.push({
          stepId: step.id,
          itemId: item.id,
          label: item.label,
        })
      }
    }
  }

  const virtualCount = fleet.cells.reduce(
    (n, c) => n + c.standards.filter(s => s.source === 'checklist').length,
    0,
  )

  return {
    boardGaps,
    checklistNeedsProjection,
    virtualCount,
    boardGapCount: boardGaps.length,
  }
}
