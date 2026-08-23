export type ReadyCheckSnap = {
  count: number
  atMs: number
}

export type ReadyCheckHistory = {
  previous: ReadyCheckSnap | null
  current: ReadyCheckSnap | null
}

export function shiftReadyCheck(
  hist: ReadyCheckHistory,
  next: ReadyCheckSnap,
): ReadyCheckHistory {
  if (hist.current == null) {
    return { previous: null, current: next }
  }
  if (hist.current.atMs === next.atMs && hist.current.count === next.count) {
    return hist
  }
  if (hist.current.atMs === next.atMs) {
    return { previous: hist.previous, current: { ...hist.current, count: next.count } }
  }
  return { previous: hist.current, current: next }
}

export function readyCheckDelta(hist: ReadyCheckHistory): number | null {
  if (hist.previous == null || hist.current == null) return null
  return hist.current.count - hist.previous.count
}

export function formatSignedDelta(delta: number): string {
  if (delta === 0) return 'Δ 0'
  const sign = delta > 0 ? '+' : '−'
  return `Δ ${sign}${Math.abs(delta).toLocaleString()}`
}

export function formatCheckAge(atMs: number, nowMs: number): string {
  const sec = Math.max(0, Math.round((nowMs - atMs) / 1000))
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return m > 0 ? `${h}h ${m}m ago` : `${h}h ago`
}

export function formatReadyCheckCaption(opts: {
  hist: ReadyCheckHistory
  nowMs: number
  oldestLabel?: string | null
}): string {
  const { hist, nowMs, oldestLabel } = opts
  const bits: string[] = []
  if (hist.previous != null) {
    bits.push(`was ${hist.previous.count.toLocaleString()}`)
    bits.push(formatCheckAge(hist.previous.atMs, nowMs))
    const delta = readyCheckDelta(hist)
    if (delta != null) bits.push(formatSignedDelta(delta))
  } else {
    bits.push('first check')
  }
  if (oldestLabel) bits.push(oldestLabel)
  return bits.join(' · ')
}
