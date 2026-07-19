/**
 * Derive the Daily Ops primary blocker from Fleet standards × Checklist catalog.
 * Type-driven via FixCapability — not hardcoded to Mac seat (seat group is one case).
 */
import {
  DAILY_OPS_CHECKLIST,
  matchStandardToChecklistItem,
  type ChecklistItem,
  type FixCapability,
} from '@/lib/control-room/dailyOpsChecklistCatalog'
import type {
  FleetCell,
  FleetCellSignal,
  FleetSnapshot,
  FleetStandardGroup,
} from '@/lib/control-room/fleetSnapshot'

export type DailyOpsBlocker = {
  itemId: string
  stepId: string
  stepOrder: number
  label: string
  group: FleetStandardGroup
  signal: 'fail' | 'degraded'
  fixCapability: FixCapability
  fixScope: string | null
  manualAction?: string
  critical: boolean
  cellKey: string
  standardId: string
  reason: string
}

const SIGNAL_RANK: Record<'fail' | 'degraded', number> = {
  fail: 2,
  degraded: 1,
}

/** AI can attempt a fix when capability is auto/semi and a scope exists. */
export function blockerAllowsAiFix(b: Pick<DailyOpsBlocker, 'fixCapability' | 'fixScope'>): boolean {
  if (b.fixScope == null || b.fixScope === '') return false
  return b.fixCapability === 'full_auto' || b.fixCapability === 'semi_auto'
}

/** Physical / notify / observe — Agent Fix must not be the primary CTA. */
export function blockerRequiresManualPath(
  b: Pick<DailyOpsBlocker, 'fixCapability' | 'fixScope'>,
): boolean {
  if (b.fixCapability === 'manual' || b.fixCapability === 'observe') return true
  return !blockerAllowsAiFix(b)
}

function shortLabel(label: string, max = 28): string {
  const t = label.trim()
  if (t.length <= max) return t
  return `${t.slice(0, max - 1)}…`
}

/**
 * Concise English primary CTA for a manual/observe blocker.
 * Group `seat` → physical Mac workstation copy; otherwise catalog-driven.
 */
export function manualPrimaryCtaLabel(b: DailyOpsBlocker): string {
  if (b.group === 'seat') {
    return 'Mac seat: verify power & bridge'
  }
  if (b.fixCapability === 'observe') {
    return `Observe · ${shortLabel(b.label)}`
  }
  if (b.manualAction != null && b.manualAction.trim() !== '') {
    const first = b.manualAction.split(/[.;]/)[0]?.trim() ?? ''
    if (first.length > 0 && first.length <= 52) return first
  }
  return `${shortLabel(b.label)}: manual next step`
}

export function secondaryAiCtaLabel(b: DailyOpsBlocker): string {
  const hint =
    b.group === 'automation' && /git/i.test(b.label)
      ? 'git dirty'
      : shortLabel(b.label, 18)
  return `Also: AI Fix (${hint})`
}

function unhealthySignal(s: FleetCellSignal): s is 'fail' | 'degraded' {
  return s === 'fail' || s === 'degraded'
}

/**
 * Collect non-ok required standards that map to a Checklist item.
 * Prefer the worst cell when provided; otherwise scan the whole fleet.
 */
export function collectDailyOpsBlockers(
  fleet: FleetSnapshot,
  opts?: { cell?: FleetCell | null },
): DailyOpsBlocker[] {
  const cells = opts?.cell != null ? [opts.cell] : fleet.cells
  const out: DailyOpsBlocker[] = []
  const seen = new Set<string>()

  for (const cell of cells) {
    for (const s of cell.standards) {
      if (s.required === false) continue
      if (!unhealthySignal(s.signal)) continue
      const hit = matchStandardToChecklistItem(s.id, s.group, {
        role: cell.role,
        env: cell.env,
      })
      if (hit == null) continue
      const key = `${hit.item.id}:${cell.key}`
      if (seen.has(key)) continue
      seen.add(key)
      out.push({
        itemId: hit.item.id,
        stepId: hit.step.id,
        stepOrder: hit.step.order,
        label: hit.item.label,
        group: hit.item.group,
        signal: s.signal,
        fixCapability: hit.item.fixCapability,
        fixScope: hit.item.fixScope,
        manualAction: hit.item.manualAction,
        critical: hit.item.critical === true,
        cellKey: cell.key,
        standardId: s.id,
        reason: s.reason,
      })
    }
  }

  return out
}

/** Highest-priority blocker: fail > degraded, then manual/observe over AI-fixable, then critical, then catalog order. */
export function pickPrimaryBlocker(blockers: DailyOpsBlocker[]): DailyOpsBlocker | null {
  if (blockers.length === 0) return null
  const ranked = [...blockers].sort((a, b) => {
    const sig = SIGNAL_RANK[b.signal] - SIGNAL_RANK[a.signal]
    if (sig !== 0) return sig
    // Same severity: physical / observe first so primary CTA is not a misleading AI Fix
    const aManual = blockerRequiresManualPath(a) ? 1 : 0
    const bManual = blockerRequiresManualPath(b) ? 1 : 0
    if (aManual !== bManual) return bManual - aManual
    if (a.critical !== b.critical) return a.critical ? -1 : 1
    if (a.stepOrder !== b.stepOrder) return a.stepOrder - b.stepOrder
    return a.itemId.localeCompare(b.itemId)
  })
  return ranked[0] ?? null
}

export function pickSecondaryAiBlocker(
  blockers: DailyOpsBlocker[],
  primary: DailyOpsBlocker,
): DailyOpsBlocker | null {
  const candidates = blockers.filter(
    b => b.itemId !== primary.itemId && blockerAllowsAiFix(b),
  )
  return pickPrimaryBlocker(candidates)
}

export function nextStepBanner(primary: DailyOpsBlocker): string {
  if (blockerRequiresManualPath(primary)) {
    return `Next: ${manualPrimaryCtaLabel(primary)}`
  }
  return `Next: AI Fix · ${shortLabel(primary.label)}`
}

/** Lookup catalog item (tests / UI hints). */
export function checklistItemById(itemId: string): ChecklistItem | null {
  for (const step of DAILY_OPS_CHECKLIST) {
    const item = step.items.find(i => i.id === itemId)
    if (item != null) return item
  }
  return null
}
