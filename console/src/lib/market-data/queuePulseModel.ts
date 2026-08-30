/**
 * Shell-level Market ingest queue pulse — derive view + pending Δ history.
 */

import type { IngestQueueDashboardResponse, IngestQueueKindCount } from '@/api/marketDataPlugin'
import {
  formatCheckAge,
  formatSignedDelta,
  readyCheckDelta,
  shiftReadyCheck,
  type ReadyCheckHistory,
  type ReadyCheckSnap,
} from '@/components/market-data/queueReadyCheck'
import type { Signal } from '@/lib/control-room/missionSignals'

export const QUEUE_PULSE_DELTA_STORE_KEY = 'bifrost.shell.queue-pulse.pending-delta.v1'

export type QueuePulseVerdict =
  | 'draining'
  | 'missed'
  | 'degraded'
  | 'due'
  | 'healthy'
  | 'idle'
  | 'unknown'

export type QueuePulseView = {
  verdict: QueuePulseVerdict
  /** Show Header chip / Dock strip when true. */
  active: boolean
  pending: number
  running: number
  ratePerMin: number | null
  etaMinutes: number | null
  topKind: string | null
  topKindPending: number
  detail: string
  /** StatusLamp / icon signal for sidebar. */
  lamp: Signal
  tagVariant: 'success' | 'warning' | 'danger' | 'neutral'
}

export function normalizeQueueVerdict(raw: string | null | undefined): QueuePulseVerdict {
  const v = (raw ?? '').toLowerCase().trim()
  if (v === 'draining') return 'draining'
  if (v === 'missed') return 'missed'
  if (v === 'degraded') return 'degraded'
  if (v === 'due') return 'due'
  if (v === 'healthy' || v === 'on_plan') return 'healthy'
  if (v === 'idle') return 'idle'
  return 'unknown'
}

export function queueVerdictToLamp(verdict: QueuePulseVerdict): Signal {
  if (verdict === 'missed' || verdict === 'degraded') return 'fail'
  if (verdict === 'draining' || verdict === 'due') return 'degraded'
  if (verdict === 'healthy' || verdict === 'idle') return 'ok'
  return 'unknown'
}

export function queueVerdictTagVariant(
  verdict: QueuePulseVerdict,
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (verdict === 'missed' || verdict === 'degraded') return 'danger'
  if (verdict === 'draining' || verdict === 'due') return 'warning'
  if (verdict === 'healthy' || verdict === 'idle') return 'success'
  return 'neutral'
}

function pickTopKind(kinds: IngestQueueKindCount[] | undefined): {
  kind: string | null
  pending: number
} {
  if (kinds == null || kinds.length === 0) return { kind: null, pending: 0 }
  let best: IngestQueueKindCount | null = null
  for (const row of kinds) {
    const score = (row.pending ?? 0) + (row.running ?? 0)
    if (best == null) {
      best = row
      continue
    }
    const bestScore = (best.pending ?? 0) + (best.running ?? 0)
    if (score > bestScore) best = row
  }
  if (best == null) return { kind: null, pending: 0 }
  return { kind: best.kind ?? null, pending: best.pending ?? 0 }
}

export function formatCompactCount(n: number): string {
  if (!Number.isFinite(n)) return '—'
  const abs = Math.abs(n)
  if (abs >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (abs >= 10_000) return `${Math.round(n / 1000)}k`
  if (abs >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(Math.round(n))
}

export function formatRatePerMin(rate: number | null): string {
  if (rate == null || !Number.isFinite(rate) || rate <= 0) return '—'
  if (rate >= 100) return `${Math.round(rate)}/min`
  if (rate >= 10) return `${rate.toFixed(1)}/min`
  return `${rate.toFixed(2)}/min`
}

export function formatEtaMinutes(eta: number | null): string {
  if (eta == null || !Number.isFinite(eta) || eta < 0) return '—'
  if (eta < 1) return '<1m'
  if (eta < 60) return `${Math.round(eta)}m`
  const h = Math.floor(eta / 60)
  const m = Math.round(eta % 60)
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

/** Derive pulse from queue-dashboard. Fail-soft when dash is null. */
export function buildQueuePulseView(
  dash: IngestQueueDashboardResponse | null | undefined,
): QueuePulseView {
  if (dash == null) {
    return {
      verdict: 'unknown',
      active: false,
      pending: 0,
      running: 0,
      ratePerMin: null,
      etaMinutes: null,
      topKind: null,
      topKindPending: 0,
      detail: 'queue-dashboard unavailable',
      lamp: 'unknown',
      tagVariant: 'neutral',
    }
  }

  const pending = Number(dash.queue?.pending ?? dash.queue?.ready_now ?? 0) || 0
  const running = Number(dash.queue?.running ?? 0) || 0
  const husVerdict = normalizeQueueVerdict(dash.husbandry?.verdict)
  const scheduleVerdict = normalizeQueueVerdict(dash.schedule?.verdict)
  const queueVerdict = normalizeQueueVerdict(dash.queue?.verdict)

  let verdict: QueuePulseVerdict = husVerdict
  if (verdict === 'unknown' || verdict === 'healthy' || verdict === 'idle') {
    if (scheduleVerdict === 'missed') verdict = 'missed'
    else if (queueVerdict === 'draining' || pending > 0) verdict = 'draining'
    else if (scheduleVerdict === 'due') verdict = 'due'
    else if (queueVerdict === 'idle' && pending === 0) verdict = 'idle'
    else if (husVerdict === 'healthy' || scheduleVerdict === 'healthy') verdict = 'healthy'
  }

  const rateRaw = dash.throughput?.jobs_per_min_15m
  const ratePerMin =
    rateRaw != null && Number.isFinite(rateRaw) && rateRaw > 0 ? Number(rateRaw) : null
  const etaRaw = dash.throughput?.eta_minutes_at_current_rate
  const etaMinutes =
    etaRaw != null && Number.isFinite(etaRaw) && etaRaw >= 0 ? Number(etaRaw) : null

  const top = pickTopKind(dash.queue?.kinds)
  const active =
    pending > 0 ||
    verdict === 'draining' ||
    verdict === 'missed' ||
    verdict === 'degraded' ||
    verdict === 'due'

  const detail =
    dash.husbandry?.detail ??
    (pending > 0
      ? `${pending} pending · ${running} running`
      : scheduleVerdict === 'missed'
        ? 'schedule missed'
        : 'queue idle')

  return {
    verdict,
    active,
    pending,
    running,
    ratePerMin,
    etaMinutes,
    topKind: top.kind,
    topKindPending: top.pending,
    detail,
    lamp: queueVerdictToLamp(verdict),
    tagVariant: queueVerdictTagVariant(verdict),
  }
}

export function isFastPollPulse(view: QueuePulseView): boolean {
  return (
    view.pending > 0 ||
    view.verdict === 'draining' ||
    view.verdict === 'missed' ||
    view.verdict === 'degraded'
  )
}

export function readPendingDeltaHistory(): ReadyCheckHistory {
  try {
    const raw = sessionStorage.getItem(QUEUE_PULSE_DELTA_STORE_KEY)
    if (raw == null || raw === '') return { previous: null, current: null }
    const parsed = JSON.parse(raw) as ReadyCheckHistory
    if (parsed != null && typeof parsed === 'object') return parsed
  } catch {
    /* ignore */
  }
  return { previous: null, current: null }
}

export function writePendingDeltaHistory(hist: ReadyCheckHistory): void {
  try {
    sessionStorage.setItem(QUEUE_PULSE_DELTA_STORE_KEY, JSON.stringify(hist))
  } catch {
    /* ignore quota */
  }
}

export function recordPendingSample(
  hist: ReadyCheckHistory,
  pending: number,
  atMs = Date.now(),
): ReadyCheckHistory {
  const next: ReadyCheckSnap = { count: pending, atMs }
  const shifted = shiftReadyCheck(hist, next)
  writePendingDeltaHistory(shifted)
  return shifted
}

export type PendingDeltaView = {
  delta: number | null
  label: string | null
  caption: string
}

export function pendingDeltaView(
  hist: ReadyCheckHistory,
  nowMs = Date.now(),
): PendingDeltaView {
  const delta = readyCheckDelta(hist)
  const label = delta != null ? formatSignedDelta(delta) : null
  const bits: string[] = []
  if (hist.previous != null) {
    bits.push(`was ${hist.previous.count.toLocaleString()}`)
    bits.push(formatCheckAge(hist.previous.atMs, nowMs))
    if (label != null) bits.push(label)
  } else {
    bits.push('first sample')
  }
  return { delta, label, caption: bits.join(' · ') }
}

export function formatChipLine(opts: {
  view: QueuePulseView
  deltaLabel: string | null
}): string {
  const { view, deltaLabel } = opts
  const parts = [
    'Queue',
    view.verdict.toUpperCase(),
    formatCompactCount(view.pending),
    formatRatePerMin(view.ratePerMin),
    `ETA ${formatEtaMinutes(view.etaMinutes)}`,
  ]
  if (deltaLabel != null) parts.push(deltaLabel)
  return parts.join(' · ')
}
