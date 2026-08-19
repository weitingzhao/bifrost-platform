const DOW = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'] as const

function isNum(raw: string): boolean {
  return /^\d+$/.test(raw)
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

function fmtHm(hour: number, minute: number): string {
  return `${pad2(hour)}:${pad2(minute)}`
}

function dowName(raw: string): string | null {
  if (!isNum(raw)) return null
  const n = Number(raw)
  const idx = n === 7 ? 0 : n
  if (idx < 0 || idx > 6) return null
  return DOW[idx]
}

const DOW_SHORT = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const

function dowShort(n: number): string {
  const idx = n === 7 ? 0 : n
  if (idx < 0 || idx > 6) return String(n)
  return DOW_SHORT[idx]
}

function describeDow(raw: string): string | null {
  if (raw === '*') return null
  if (isNum(raw)) return dowName(raw)
  const rangeMatch = raw.match(/^(\d)-(\d)$/)
  if (rangeMatch) {
    const lo = Number(rangeMatch[1])
    const hi = Number(rangeMatch[2])
    if (lo === 1 && hi === 5) return 'Mon–Fri'
    if (lo === 0 && hi === 6) return null
    return `${dowShort(lo)}–${dowShort(hi)}`
  }
  const parts = raw.split(',')
  if (parts.length > 1 && parts.every(isNum)) {
    return parts.map(p => dowShort(Number(p))).join(', ')
  }
  return null
}

/** Human-readable 5-field cron (min hour dom month dow). Unknown shapes keep the raw expr. */
export function describeCronSchedule(expr: string): string {
  const fields = expr.trim().split(/\s+/)
  if (fields.length !== 5) return expr
  const [min, hour, dom, month, dow] = fields

  if (min === '*' && hour === '*' && dom === '*' && month === '*' && dow === '*') {
    return 'Every minute'
  }
  if (min.startsWith('*/') && hour === '*' && dom === '*' && month === '*' && dow === '*') {
    const step = min.slice(2)
    return isNum(step) ? `Every ${step} minutes` : expr
  }
  if (hour.startsWith('*/') && min === '0' && dom === '*' && month === '*' && dow === '*') {
    const step = hour.slice(2)
    return isNum(step) ? `Every ${step} hours` : expr
  }
  if (isNum(min) && hour === '*' && dom === '*' && month === '*' && dow === '*') {
    return `Hourly at :${pad2(Number(min))}`
  }
  if (isNum(min) && isNum(hour) && dom === '*' && month === '*') {
    const time = fmtHm(Number(hour), Number(min))
    const dowLabel = describeDow(dow)
    if (dow === '*') return `Daily at ${time} UTC`
    if (isNum(dow) && dowLabel != null) return `Weekly on ${dowLabel} at ${time} UTC`
    if (dowLabel != null) return `${dowLabel} at ${time} UTC`
  }
  if (isNum(min) && isNum(hour) && isNum(dom) && month === '*' && dow === '*') {
    return `Monthly on day ${dom} at ${fmtHm(Number(hour), Number(min))} UTC`
  }
  return expr
}

export function formatDurationParts(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000))
  const days = Math.floor(totalSec / 86_400)
  const hours = Math.floor((totalSec % 86_400) / 3600)
  const mins = Math.floor((totalSec % 3600) / 60)
  const secs = totalSec % 60
  if (days > 0) return hours > 0 ? `${days}d ${hours}h` : `${days}d`
  if (hours > 0) return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`
  if (mins > 0) return mins < 5 && secs > 0 ? `${mins}m ${secs}s` : `${mins}m`
  return `${secs}s`
}

/** Countdown / overdue label for a future ISO timestamp. */
export function formatCountdownTo(iso: string | undefined, now = Date.now()): string {
  if (iso == null || iso === '') return '—'
  const t = Date.parse(iso)
  if (!Number.isFinite(t)) return '—'
  const delta = t - now
  if (delta <= 0) {
    const late = Math.abs(delta)
    if (late < 60_000) return 'due now'
    return `overdue ${formatDurationParts(late)}`
  }
  return `in ${formatDurationParts(delta)}`
}

export function formatNextRunAt(iso: string | undefined): string {
  if (iso == null || iso === '') return '—'
  const t = Date.parse(iso)
  if (!Number.isFinite(t)) return '—'
  return new Date(t).toLocaleString()
}
