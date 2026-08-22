import type { IngestQueueKindCount } from '@/api/marketDataPlugin'

export type QueuePressureLevel = 'idle' | 'low' | 'elevated' | 'high' | 'stalled'

export type QueuePressureInput = {
  pending: number
  running: number
  ratePerMin: number | null | undefined
  etaMinutes?: number | null
  oldestPendingAgeSec?: number | null
  doneLast5m?: number
  doneLast15m?: number
  doneLast60m?: number
  kinds?: IngestQueueKindCount[]
  selectedKind?: string
  nowMs: number
}

export type KindPressure = {
  kind: string
  pending: number
  running: number
  active: number
  etaMinutes: number | null
}

export type QueuePressureView = {
  pending: number
  running: number
  ratePerMin: number
  etaMinutes: number | null
  emptyAtMs: number | null
  waitedSec: number | null
  progress01: number | null
  fillPct: number
  level: QueuePressureLevel
  kinds: KindPressure[]
  doneLast5m: number
  doneLast15m: number
  doneLast60m: number
}

const PRESSURE_CAP = 2000

export function utcClock(ms: number): string {
  const d = new Date(ms)
  const hh = String(d.getUTCHours()).padStart(2, '0')
  const mm = String(d.getUTCMinutes()).padStart(2, '0')
  return `${hh}:${mm} UTC`
}

export function etaMinutesFromRate(pending: number, ratePerMin: number): number | null {
  if (!(pending > 0) || !(ratePerMin > 0)) return null
  return Math.round((pending / ratePerMin) * 10) / 10
}

export function pressureLevel(args: {
  pending: number
  running: number
  ratePerMin: number
  etaMinutes: number | null
  waitedSec: number | null
}): QueuePressureLevel {
  if (args.pending <= 0 && args.running <= 0) return 'idle'
  if (args.pending > 0 && args.ratePerMin <= 0) return 'stalled'
  if (args.etaMinutes != null && args.etaMinutes >= 120) return 'high'
  if (args.waitedSec != null && args.waitedSec >= 3600) return 'high'
  if (args.etaMinutes != null && args.etaMinutes >= 15) return 'elevated'
  return 'low'
}

export function buildQueuePressure(input: QueuePressureInput): QueuePressureView {
  const selected = input.selectedKind?.trim() ?? ''
  const kindsAll = input.kinds ?? []
  const selectedRow = selected !== '' ? kindsAll.find(k => k.kind === selected) : undefined
  const pending =
    selectedRow != null ? selectedRow.pending : Math.max(0, input.pending)
  const running =
    selectedRow != null ? selectedRow.running : Math.max(0, input.running)
  const rate = Math.max(0, input.ratePerMin ?? 0)
  const etaFromApi =
    selected === '' && input.etaMinutes != null && Number.isFinite(input.etaMinutes)
      ? input.etaMinutes
      : null
  const etaMinutes = etaFromApi ?? etaMinutesFromRate(pending, rate)
  const waitedSec =
    input.oldestPendingAgeSec != null && Number.isFinite(input.oldestPendingAgeSec)
      ? Math.max(0, input.oldestPendingAgeSec)
      : null
  const remainSec = etaMinutes != null ? etaMinutes * 60 : null
  let progress01: number | null = null
  if (waitedSec != null && remainSec != null && waitedSec + remainSec > 0) {
    progress01 = Math.min(1, waitedSec / (waitedSec + remainSec))
  }
  const emptyAtMs =
    etaMinutes != null ? input.nowMs + etaMinutes * 60_000 : null
  const fillPct = Math.min(100, (pending / PRESSURE_CAP) * 100)
  const kinds: KindPressure[] = kindsAll.map(k => ({
    kind: k.kind,
    pending: k.pending,
    running: k.running,
    active: k.active,
    etaMinutes: etaMinutesFromRate(k.pending, rate),
  }))
  return {
    pending,
    running,
    ratePerMin: rate,
    etaMinutes,
    emptyAtMs,
    waitedSec,
    progress01,
    fillPct,
    level: pressureLevel({ pending, running, ratePerMin: rate, etaMinutes, waitedSec }),
    kinds,
    doneLast5m: input.doneLast5m ?? 0,
    doneLast15m: input.doneLast15m ?? 0,
    doneLast60m: input.doneLast60m ?? 0,
  }
}
