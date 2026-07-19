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

/**
 * Path chip for Execution → Now Fix target bar.
 * Manual/observe → Manual|Observe; agent-capable → Semi/Auto (matches Ops loop CTA family).
 */
export function fixPathLabel(
  b: Pick<DailyOpsBlocker, 'fixCapability' | 'fixScope'>,
): string {
  if (b.fixCapability === 'observe') return 'Observe'
  if (blockerRequiresManualPath(b)) return 'Manual'
  return 'Semi/Auto'
}

/** Short next-step copy aligned with Ops loop primary CTA (not a second resolver). */
export function fixTargetNextStep(
  primary: DailyOpsBlocker,
  primaryActionLabel?: string | null,
): string {
  const fromCta = primaryActionLabel?.replace(/\s*→\s*$/, '').trim()
  // In-flight strip CTA — not the underlying fix-target next step.
  if (
    fromCta != null &&
    fromCta !== '' &&
    !/^view agent$/i.test(fromCta)
  ) {
    return fromCta
  }
  if (blockerRequiresManualPath(primary)) return manualPrimaryCtaLabel(primary)
  return `AI Fix · ${shortLabel(primary.label)}`
}

/** Catalog items that share a remediation fixScope (job ↔ checklist link). */
export function checklistItemsByFixScope(fixScope: string | null | undefined): ChecklistItem[] {
  if (fixScope == null || fixScope === '') return []
  const out: ChecklistItem[] = []
  for (const step of DAILY_OPS_CHECKLIST) {
    for (const item of step.items) {
      if (item.fixScope === fixScope) out.push(item)
    }
  }
  return out
}

export type AmbientJobFixTarget = {
  /** Prefer checklist label when job maps to an item; else scope display. */
  label: string
  itemId: string | null
  pathLabel: string
  fixScope: string | null
  /** True when job target is the same checklist item as primary blocker. */
  alignsWithPrimary: boolean
}

/**
 * Resolve ambient remediation job → Fix target for Now bar.
 * Prefer explicit checklist item id; else match fixScope (prefer primary blocker when shared scope).
 */
export function resolveAmbientJobFixTarget(opts: {
  jobScope?: string | null
  checklistItemId?: string | null
  primaryBlocker?: DailyOpsBlocker | null
  scopeFallbackLabel?: string | null
}): AmbientJobFixTarget | null {
  const scope =
    opts.jobScope != null && opts.jobScope !== '' ? opts.jobScope : null
  const itemId =
    opts.checklistItemId != null && opts.checklistItemId !== ''
      ? opts.checklistItemId
      : null

  let item: ChecklistItem | null = itemId != null ? checklistItemById(itemId) : null
  if (item == null && scope != null) {
    const byScope = checklistItemsByFixScope(scope)
    if (opts.primaryBlocker != null && opts.primaryBlocker.fixScope === scope) {
      const hit = byScope.find(i => i.id === opts.primaryBlocker!.itemId)
      if (hit != null) item = hit
    }
    if (item == null && byScope.length === 1) item = byScope[0] ?? null
    if (item == null && byScope.length > 1) {
      // Shared scope (e.g. operator-plane): prefer primary if present, else first.
      item =
        byScope.find(i => i.id === opts.primaryBlocker?.itemId) ?? byScope[0] ?? null
    }
  }

  if (item == null && scope == null && itemId == null) return null

  const label =
    item?.label ??
    (opts.scopeFallbackLabel != null && opts.scopeFallbackLabel !== ''
      ? opts.scopeFallbackLabel
      : (scope ?? 'Agent run'))

  const pathSource: Pick<DailyOpsBlocker, 'fixCapability' | 'fixScope'> =
    item != null
      ? { fixCapability: item.fixCapability, fixScope: item.fixScope }
      : opts.primaryBlocker != null && opts.primaryBlocker.fixScope === scope
        ? opts.primaryBlocker
        : { fixCapability: 'semi_auto', fixScope: scope }

  const alignsWithPrimary =
    opts.primaryBlocker != null &&
    (item?.id === opts.primaryBlocker.itemId ||
      (item == null &&
        scope != null &&
        opts.primaryBlocker.fixScope === scope))

  return {
    label,
    itemId: item?.id ?? null,
    pathLabel: fixPathLabel(pathSource),
    fixScope: item?.fixScope ?? scope,
    alignsWithPrimary,
  }
}

/** Lookup catalog item (tests / UI hints). */
export function checklistItemById(itemId: string): ChecklistItem | null {
  for (const step of DAILY_OPS_CHECKLIST) {
    const item = step.items.find(i => i.id === itemId)
    if (item != null) return item
  }
  return null
}
