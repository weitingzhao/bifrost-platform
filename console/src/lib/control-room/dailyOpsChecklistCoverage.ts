/**
 * Daily Ops Checklist ↔ Fleet Board coverage index + touch timestamps.
 *
 * Touch kinds:
 * - dry-run: catalog coverage pass against live Fleet probes (automatic each poll)
 * - run: real remediation / checklist execution (Agent Fix, Operator Plan AI Fix, …)
 *
 * Display prefers last **run** when present; otherwise last dry-run.
 * `path` group is excluded (structural unavailable, not a health check).
 */
import {
  matchStandardToChecklistItem,
  type ChecklistItem,
  type DailyOpsChecklistStep,
} from '@/lib/control-room/dailyOpsChecklistCatalog'
import type {
  FleetCell,
  FleetSnapshot,
  FleetStandard,
  FleetStandardGroup,
} from '@/lib/control-room/fleetSnapshot'

export type ChecklistTouchKind = 'dry-run' | 'run'

export type ChecklistTouchRecord = {
  dryRunAt?: string
  runAt?: string
}

export type ChecklistCoverageHit = {
  stepId: string
  stepOrder: number
  stepLabel: string
  itemId: string
  itemLabel: string
  fixCapability: ChecklistItem['fixCapability']
  /** Display touch kind (run wins over dry-run when both exist) */
  touchKind: ChecklistTouchKind
  /** Timestamp for the displayed touch */
  touchedAt: string
  dryRunAt?: string
  runAt?: string
}

export type ChecklistCoverageEntry = {
  cellKey: string
  standardId: string
  group: FleetStandardGroup
  /** null when not in checklist (gap) or excluded (path) */
  hit: ChecklistCoverageHit | null
  excluded: boolean
}

export type ChecklistCoverageIndex = {
  /** `${cellKey}::${standardId}` → entry */
  byKey: Map<string, ChecklistCoverageEntry>
  coveredCount: number
  uncoveredCount: number
  excludedCount: number
  runTouchedCount: number
  /** Standards with source:'checklist' (virtual projections) */
  virtualCount: number
  /** Wall-clock of this dry-run pass */
  dryRunAt: string
}

const TOUCH_STORAGE_KEY = 'bifrost:daily-ops-checklist-touches-v2'

let touchStoreEpoch = 0
const touchListeners = new Set<() => void>()

function bumpTouchEpoch(): void {
  touchStoreEpoch += 1
  for (const l of touchListeners) l()
}

/** For React useSyncExternalStore — re-render when run touches are recorded. */
export function subscribeChecklistTouchStore(onStoreChange: () => void): () => void {
  touchListeners.add(onStoreChange)
  return () => {
    touchListeners.delete(onStoreChange)
  }
}

export function getChecklistTouchStoreEpoch(): number {
  return touchStoreEpoch
}

export function coverageKey(cellKey: string, standardId: string): string {
  return `${cellKey}::${standardId}`
}

function parseTouchRecord(raw: unknown): ChecklistTouchRecord | null {
  if (raw == null) return null
  if (typeof raw === 'string') {
    // v1 migration: bare ISO → dry-run only
    return { dryRunAt: raw }
  }
  if (typeof raw !== 'object') return null
  const o = raw as Record<string, unknown>
  const dryRunAt = typeof o.dryRunAt === 'string' ? o.dryRunAt : undefined
  const runAt = typeof o.runAt === 'string' ? o.runAt : undefined
  if (dryRunAt == null && runAt == null) return null
  return { dryRunAt, runAt }
}

function readTouchStore(): Record<string, ChecklistTouchRecord> {
  if (typeof sessionStorage === 'undefined') return {}
  try {
    const raw = sessionStorage.getItem(TOUCH_STORAGE_KEY)
    if (raw == null || raw === '') return {}
    const parsed = JSON.parse(raw) as unknown
    if (parsed == null || typeof parsed !== 'object') return {}
    const out: Record<string, ChecklistTouchRecord> = {}
    for (const [k, v] of Object.entries(parsed as Record<string, unknown>)) {
      const rec = parseTouchRecord(v)
      if (rec != null) out[k] = rec
    }
    return out
  } catch {
    return {}
  }
}

function writeTouchStore(store: Record<string, ChecklistTouchRecord>): void {
  if (typeof sessionStorage === 'undefined') return
  try {
    sessionStorage.setItem(TOUCH_STORAGE_KEY, JSON.stringify(store))
  } catch {
    /* quota / private mode — ignore */
  }
}

function resolveDisplayTouch(rec: ChecklistTouchRecord | undefined, dryRunFallback: string): {
  touchKind: ChecklistTouchKind
  touchedAt: string
  dryRunAt?: string
  runAt?: string
} {
  const dryRunAt = rec?.dryRunAt ?? dryRunFallback
  const runAt = rec?.runAt
  if (runAt != null && runAt !== '') {
    return { touchKind: 'run', touchedAt: runAt, dryRunAt, runAt }
  }
  return { touchKind: 'dry-run', touchedAt: dryRunAt, dryRunAt, runAt }
}

/** Relative label for dense UI — with "ago". */
export function formatChecklistTouchAge(
  iso: string | null | undefined,
  nowMs: number = Date.now(),
): string {
  const compact = formatChecklistTouchAgeCompact(iso, nowMs)
  if (compact === 'never' || compact === 'now') {
    return compact === 'now' ? 'just now' : 'never'
  }
  return `${compact} ago`
}

/** Compact age for chips: `now` · `3s` · `2m` · `1h` · `3d`. */
export function formatChecklistTouchAgeCompact(
  iso: string | null | undefined,
  nowMs: number = Date.now(),
): string {
  if (iso == null || iso === '') return 'never'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return 'never'
  const sec = Math.max(0, Math.floor((nowMs - t) / 1000))
  if (sec < 5) return 'now'
  if (sec < 60) return `${sec}s`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m`
  const hr = Math.floor(min / 60)
  if (hr < 48) return `${hr}h`
  return `${Math.floor(hr / 24)}d`
}

export function touchKindShortLabel(kind: ChecklistTouchKind): 'd' | 'r' {
  return kind === 'run' ? 'r' : 'd'
}

export function touchKindLabel(kind: ChecklistTouchKind): string {
  return kind === 'run' ? 'run' : 'dry-run'
}

export function describeCoverageEntry(entry: ChecklistCoverageEntry | undefined): string {
  if (entry == null) return 'No coverage data'
  if (entry.excluded) return 'Excluded from Daily Ops Checklist (structural path)'
  if (entry.hit == null) return 'Not touched by Daily Ops Checklist'
  const age = formatChecklistTouchAge(entry.hit.touchedAt)
  const kind = touchKindLabel(entry.hit.touchKind)
  return `Checklist ${entry.hit.stepOrder}. ${entry.hit.stepLabel} · ${entry.hit.itemLabel} · ${kind} · ${age}`
}

/**
 * Resolve live Fleet Board standards against the checklist catalog and
 * refresh dry-run touch timestamps for every covered standard.
 */
export function buildChecklistCoverageIndex(
  fleet: FleetSnapshot,
  options?: { dryRunAt?: string; persistTouches?: boolean },
): ChecklistCoverageIndex {
  const dryRunAt = options?.dryRunAt ?? new Date().toISOString()
  const persist = options?.persistTouches !== false
  const prev = readTouchStore()
  const nextStore: Record<string, ChecklistTouchRecord> = { ...prev }
  const byKey = new Map<string, ChecklistCoverageEntry>()
  let coveredCount = 0
  let uncoveredCount = 0
  let excludedCount = 0
  let runTouchedCount = 0
  let virtualCount = 0

  for (const cell of fleet.cells) {
    for (const standard of cell.standards) {
      const key = coverageKey(cell.key, standard.id)
      if (standard.source === 'checklist') virtualCount += 1
      if (standard.group === 'path') {
        excludedCount += 1
        byKey.set(key, {
          cellKey: cell.key,
          standardId: standard.id,
          group: standard.group,
          hit: null,
          excluded: true,
        })
        continue
      }

      const matched = matchStandardToChecklistItem(standard.id, standard.group, {
        role: cell.role,
        env: cell.env,
      })

      if (matched == null) {
        uncoveredCount += 1
        byKey.set(key, {
          cellKey: cell.key,
          standardId: standard.id,
          group: standard.group,
          hit: null,
          excluded: false,
        })
        continue
      }

      coveredCount += 1
      const existing = nextStore[key] ?? {}
      nextStore[key] = { ...existing, dryRunAt }
      const display = resolveDisplayTouch(nextStore[key], dryRunAt)
      if (display.touchKind === 'run') runTouchedCount += 1

      byKey.set(key, {
        cellKey: cell.key,
        standardId: standard.id,
        group: standard.group,
        excluded: false,
        hit: {
          stepId: matched.step.id,
          stepOrder: matched.step.order,
          stepLabel: matched.step.label,
          itemId: matched.item.id,
          itemLabel: matched.item.label,
          fixCapability: matched.item.fixCapability,
          touchKind: display.touchKind,
          touchedAt: display.touchedAt,
          dryRunAt: display.dryRunAt,
          runAt: display.runAt,
        },
      })
    }
  }

  if (persist) writeTouchStore(nextStore)

  return {
    byKey,
    coveredCount,
    uncoveredCount,
    excludedCount,
    runTouchedCount,
    virtualCount,
    dryRunAt,
  }
}

/**
 * Record a real checklist / Agent Fix run against all scored standards in a cell.
 * Does not clear dry-run history; display will prefer this run timestamp.
 */
export function recordChecklistRunTouch(
  cell: Pick<FleetCell, 'key' | 'standards'>,
  options?: { at?: string; standardIds?: string[] },
): void {
  const at = options?.at ?? new Date().toISOString()
  const store = readTouchStore()
  const ids =
    options?.standardIds ??
    cell.standards.filter(s => s.group !== 'path').map(s => s.id)

  for (const id of ids) {
    const key = coverageKey(cell.key, id)
    const prev = store[key] ?? {}
    store[key] = { ...prev, runAt: at }
  }
  writeTouchStore(store)
  bumpTouchEpoch()
}

export function lookupCoverage(
  index: ChecklistCoverageIndex | null | undefined,
  cell: Pick<FleetCell, 'key'>,
  standard: Pick<FleetStandard, 'id'>,
): ChecklistCoverageEntry | undefined {
  if (index == null) return undefined
  return index.byKey.get(coverageKey(cell.key, standard.id))
}

/**
 * Coverage keys (`cellKey::standardId`) owned by a checklist step.
 * Used to flash Fleet Board chips when a Checklist section is clicked.
 */
export function coverageKeysForChecklistStep(
  index: ChecklistCoverageIndex | null | undefined,
  stepId: string,
): string[] {
  if (index == null) return []
  const keys: string[] = []
  for (const [key, entry] of index.byKey) {
    if (entry.hit?.stepId === stepId) keys.push(key)
  }
  return keys
}

/** Test helper — clear session touch store. */
export function clearChecklistTouchStore(): void {
  if (typeof sessionStorage === 'undefined') return
  try {
    sessionStorage.removeItem(TOUCH_STORAGE_KEY)
  } catch {
    /* ignore */
  }
  bumpTouchEpoch()
}

/** Pure match without persistence — for unit tests. */
export function matchCellStandard(
  cell: Pick<FleetCell, 'role' | 'env' | 'key'>,
  standard: Pick<FleetStandard, 'id' | 'group'>,
): { step: DailyOpsChecklistStep; item: ChecklistItem } | null {
  if (standard.group === 'path') return null
  return matchStandardToChecklistItem(standard.id, standard.group, {
    role: cell.role,
    env: cell.env,
  })
}
