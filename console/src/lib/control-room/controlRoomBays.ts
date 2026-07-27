import type { Signal } from '@/lib/control-room/missionSignals'
import {
  collectMissionDegradationItems,
  missionDegradationSummary,
  type MissionSnapshot,
  worst,
} from '@/lib/control-room/missionSignals'

/** Stable DOM / hash ids for Control Room in-page bay navigation. */
export const CONTROL_ROOM_BAY_IDS = [
  'mission',
  'launch',
  'operate',
  'release',
  'health',
  'governance',
] as const

export type ControlRoomBayId = (typeof CONTROL_ROOM_BAY_IDS)[number]

export const CONTROL_ROOM_BAY_DOM_PREFIX = 'cr-'

export function controlRoomBayDomId(id: ControlRoomBayId): string {
  return `${CONTROL_ROOM_BAY_DOM_PREFIX}${id}`
}

/** Hash fragment `#cr-mission` — not a Console view tab; safe beside `#control-room`. */
export function controlRoomBayHash(id: ControlRoomBayId): string {
  return `#${controlRoomBayDomId(id)}`
}

export function parseControlRoomBayHash(hash: string): ControlRoomBayId | null {
  const raw = hash.replace(/^#/, '').split('?')[0] ?? ''
  if (!raw.startsWith(CONTROL_ROOM_BAY_DOM_PREFIX)) return null
  const id = raw.slice(CONTROL_ROOM_BAY_DOM_PREFIX.length)
  return (CONTROL_ROOM_BAY_IDS as readonly string[]).includes(id)
    ? (id as ControlRoomBayId)
    : null
}

export type ControlRoomBaySignal = {
  id: ControlRoomBayId
  label: string
  signal: Signal
  /** One-line compact reason shown in bay header / nav title. */
  reason: string
}

export type ControlRoomBaySignalInput = {
  snapshot: MissionSnapshot
  /** Open operate-queue handoffs (caution when > 0). */
  operateOpenCount?: number
  /** Pending decision briefs. */
  pendingBriefCount?: number
  /** Active remediation jobs (live Agent loop). */
  activeAgentJobCount?: number
  /** Network live probe reach when Health bay is present. */
  networkProbe?: Signal
  /** Promote / cutover lamp when known. */
  promoteLamp?: Signal
  showHealth?: boolean
}

function signalReason(signal: Signal, ok: string, caution: string, fail: string): string {
  if (signal === 'ok') return ok
  if (signal === 'fail') return fail
  if (signal === 'degraded') return caution
  return 'Probing…'
}

export function buildControlRoomBaySignals(input: ControlRoomBaySignalInput): ControlRoomBaySignal[] {
  const {
    snapshot,
    operateOpenCount = 0,
    pendingBriefCount = 0,
    activeAgentJobCount = 0,
    networkProbe,
    promoteLamp,
    showHealth = true,
  } = input

  const degradation = collectMissionDegradationItems(snapshot)
  const missionReason =
    snapshot.missionOverall === 'ok'
      ? 'Mission probes nominal'
      : missionDegradationSummary(degradation)

  const launchSignal = worst(snapshot.release.signal, snapshot.payloadOverall)
  const launchReason = signalReason(
    launchSignal,
    'Launch pad clear',
    'Release or payload caution',
    'Release or payload blocked',
  )

  const operateParts: Signal[] = [snapshot.agent.signal]
  if (operateOpenCount > 0 || pendingBriefCount > 0) operateParts.push('degraded')
  if (activeAgentJobCount > 0) operateParts.push('degraded')
  const operateSignal = worst(...operateParts)
  const operateBits: string[] = []
  if (activeAgentJobCount > 0) operateBits.push(`${activeAgentJobCount} live job${activeAgentJobCount === 1 ? '' : 's'}`)
  if (operateOpenCount > 0) operateBits.push(`${operateOpenCount} handoff${operateOpenCount === 1 ? '' : 's'}`)
  if (pendingBriefCount > 0) operateBits.push(`${pendingBriefCount} decision${pendingBriefCount === 1 ? '' : 's'}`)
  const operateReason =
    operateBits.length > 0
      ? operateBits.join(' · ')
      : signalReason(snapshot.agent.signal, 'Agent loop idle', 'Agent caution', 'Agent fail')

  // promoteLamp 'unknown' = Prod cutover narrative-ready (non-live GO), not "still probing".
  // Folding it into worst() would paint the Release bay as PROBING while STG/deliver are clear.
  let releaseSignal: Signal
  let releaseReason: string
  if (promoteLamp == null) {
    releaseSignal = snapshot.release.signal
    releaseReason = signalReason(
      releaseSignal,
      'Release / promote clear',
      'Release or promote caution',
      'Release or promote blocked',
    )
  } else if (promoteLamp === 'unknown') {
    releaseSignal = snapshot.release.signal
    if (releaseSignal === 'ok') {
      releaseReason = 'Narrative ready · awaiting live cutover'
    } else {
      releaseReason = signalReason(
        releaseSignal,
        'Narrative ready · awaiting live cutover',
        'Release caution · cutover narrative ready',
        'Release blocked · cutover narrative ready',
      )
    }
  } else {
    releaseSignal = worst(snapshot.release.signal, promoteLamp)
    releaseReason = signalReason(
      releaseSignal,
      'Release / promote clear',
      'Release or promote caution',
      'Release or promote blocked',
    )
  }

  const bays: ControlRoomBaySignal[] = [
    { id: 'mission', label: 'Mission', signal: snapshot.missionOverall, reason: missionReason },
    { id: 'launch', label: 'Launch', signal: launchSignal, reason: launchReason },
    { id: 'operate', label: 'Operate', signal: operateSignal, reason: operateReason },
    { id: 'release', label: 'Release', signal: releaseSignal, reason: releaseReason },
  ]

  if (showHealth) {
    const healthSignal = networkProbe ?? 'unknown'
    bays.push({
      id: 'health',
      label: 'Health',
      signal: healthSignal,
      reason: signalReason(healthSignal, 'Network probes ok', 'Network caution', 'Network fail'),
    })
  }

  bays.push({
    id: 'governance',
    label: 'Governance',
    signal: 'ok',
    reason: 'Rocket · Spokes · Tracks · Flywheel · Pipeline',
  })

  return bays
}

/** localStorage key for bay expand preference (legacy per-bay; still written for Multi). */
export function controlRoomBayStorageKey(id: ControlRoomBayId): string {
  return `bifrost_control_room_bay_${id}_open`
}

export type ControlRoomExpandMode = 'single' | 'multi'

export const CONTROL_ROOM_EXPAND_MODE_STORAGE_KEY = 'bifrost_control_room_expand_mode'

export function controlRoomExpandModeStorageKey(): string {
  return CONTROL_ROOM_EXPAND_MODE_STORAGE_KEY
}

export function loadControlRoomExpandMode(
  storage: Pick<Storage, 'getItem'> = localStorage,
): ControlRoomExpandMode {
  try {
    const raw = storage.getItem(CONTROL_ROOM_EXPAND_MODE_STORAGE_KEY)
    if (raw === 'multi') return 'multi'
    if (raw === 'single') return 'single'
  } catch {
    // ignore
  }
  return 'single'
}

export function saveControlRoomExpandMode(
  mode: ControlRoomExpandMode,
  storage: Pick<Storage, 'setItem'> = localStorage,
): void {
  try {
    storage.setItem(CONTROL_ROOM_EXPAND_MODE_STORAGE_KEY, mode)
  } catch {
    // ignore
  }
}

/** Read which bays were left open (legacy per-bay keys). */
export function loadOpenControlRoomBayIds(
  storage: Pick<Storage, 'getItem'> = localStorage,
): ControlRoomBayId[] {
  const open: ControlRoomBayId[] = []
  for (const id of CONTROL_ROOM_BAY_IDS) {
    try {
      if (storage.getItem(controlRoomBayStorageKey(id)) === 'true') open.push(id)
    } catch {
      // ignore
    }
  }
  return open
}

export function persistOpenControlRoomBayIds(
  openIds: Iterable<ControlRoomBayId>,
  storage: Pick<Storage, 'setItem'> = localStorage,
): void {
  const open = new Set(openIds)
  for (const id of CONTROL_ROOM_BAY_IDS) {
    try {
      storage.setItem(controlRoomBayStorageKey(id), String(open.has(id)))
    } catch {
      // ignore
    }
  }
}

/**
 * Initial open set for Control Room bays.
 * - Prefer hash / preferredId when present.
 * - Single mode: at most one bay (preferred → first stored open → none).
 * - Multi mode: all stored open bays; if preferred and empty, open preferred.
 */
export function resolveInitialOpenBayIds(opts: {
  mode: ControlRoomExpandMode
  preferredId?: ControlRoomBayId | null
  storedOpen?: ControlRoomBayId[]
}): ControlRoomBayId[] {
  const stored = opts.storedOpen ?? []
  const preferred = opts.preferredId ?? null

  if (opts.mode === 'single') {
    if (preferred != null) return [preferred]
    if (stored.length > 0) return [stored[0]!]
    return []
  }

  if (preferred != null && !stored.includes(preferred)) {
    return [...stored, preferred]
  }
  if (stored.length > 0) return [...stored]
  if (preferred != null) return [preferred]
  return []
}

/** Apply expand/collapse under Single (accordion) or Multi rules. */
export function nextOpenBayIds(
  mode: ControlRoomExpandMode,
  current: ReadonlySet<ControlRoomBayId>,
  id: ControlRoomBayId,
  open: boolean,
): Set<ControlRoomBayId> {
  if (mode === 'single') {
    return open ? new Set([id]) : new Set()
  }
  const next = new Set(current)
  if (open) next.add(id)
  else next.delete(id)
  return next
}

/** When switching Multi → Single, keep activeBay or the first still-open bay. */
export function collapseOpenBayIdsForSingleMode(
  current: ReadonlySet<ControlRoomBayId>,
  activeBay: ControlRoomBayId | null,
): Set<ControlRoomBayId> {
  if (activeBay != null && current.has(activeBay)) return new Set([activeBay])
  const first = CONTROL_ROOM_BAY_IDS.find(id => current.has(id))
  return first != null ? new Set([first]) : new Set()
}

export type ControlRoomVerdictLabel = 'NOMINAL' | 'CAUTION' | 'CRITICAL' | 'PROBING'

export function controlRoomVerdictLabel(signal: Signal): ControlRoomVerdictLabel {
  if (signal === 'ok') return 'NOMINAL'
  if (signal === 'degraded') return 'CAUTION'
  if (signal === 'fail') return 'CRITICAL'
  return 'PROBING'
}

export function controlRoomVerdictTagVariant(
  label: ControlRoomVerdictLabel,
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (label === 'NOMINAL') return 'success'
  if (label === 'CAUTION') return 'warning'
  if (label === 'CRITICAL') return 'danger'
  return 'neutral'
}

export function formatControlRoomFreshness(dataUpdatedAt: number, nowMs: number = Date.now()): string {
  if (!Number.isFinite(dataUpdatedAt) || dataUpdatedAt <= 0) return 'unknown'
  const age = Math.max(0, nowMs - dataUpdatedAt)
  if (age < 60_000) return `${Math.round(age / 1000)}s ago`
  return `${Math.round(age / 60_000)}m ago`
}

export type ControlRoomAttentionItem = {
  id: string
  bayId: ControlRoomBayId
  severity: 'critical' | 'warning' | 'info'
  summary: string
}

/** Cross-bay attention queue — fail/caution bays first; skip ok/unknown governance noise. */
export function buildControlRoomAttentionItems(
  bays: ControlRoomBaySignal[],
): ControlRoomAttentionItem[] {
  const items: ControlRoomAttentionItem[] = []
  for (const bay of bays) {
    if (bay.signal === 'ok' || bay.signal === 'unknown') continue
    if (bay.id === 'governance' && bay.signal !== 'fail' && bay.signal !== 'degraded') continue
    const severity: ControlRoomAttentionItem['severity'] =
      bay.signal === 'fail' ? 'critical' : 'warning'
    items.push({
      id: `bay-${bay.id}`,
      bayId: bay.id,
      severity,
      summary: `${bay.label}: ${bay.reason}`,
    })
  }
  items.sort((a, b) => {
    const rank = (s: ControlRoomAttentionItem['severity']) =>
      s === 'critical' ? 0 : s === 'warning' ? 1 : 2
    return rank(a.severity) - rank(b.severity)
  })
  return items
}

export function controlRoomBayCountsLabel(bays: ControlRoomBaySignal[]): string {
  let critical = 0
  let caution = 0
  let ok = 0
  let probing = 0
  for (const b of bays) {
    if (b.signal === 'fail') critical += 1
    else if (b.signal === 'degraded') caution += 1
    else if (b.signal === 'ok') ok += 1
    else probing += 1
  }
  return [
    critical > 0 ? `${critical} critical` : null,
    caution > 0 ? `${caution} caution` : null,
    probing > 0 ? `${probing} probing` : null,
    ok > 0 ? `${ok} clear` : null,
  ]
    .filter(Boolean)
    .join(' · ')
}
