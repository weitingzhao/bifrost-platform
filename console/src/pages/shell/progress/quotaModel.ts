import { formatDuration } from '@/api/agentProgress'
import type { ProgressPool } from '@/api/agentProgressExtra'
import { resetsIn } from '@/pages/shell/progress/progressModel'

export type QuotaTone = 'neutral' | 'positive' | 'warning'

function qty(value: number | undefined): string | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null
  return String(Math.round(value * 10) / 10)
}

export function isoSeconds(value: string | undefined): number | null {
  if (value == null || value.trim() === '') return null
  const ms = Date.parse(value)
  if (!Number.isFinite(ms)) return null
  return ms / 1000
}

/** Local date and time. Empty when the timestamp is not a date. */
export function formatLocalDateTime(ms: number): string {
  if (!Number.isFinite(ms)) return ''
  const date = new Date(ms)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

/** `resets in 1d 7h`, or `Reset due` once the window has ended. */
export function poolResetsLine(resetsAt: string | undefined, nowMs: number): string | null {
  const sec = isoSeconds(resetsAt)
  if (sec == null) return null
  if (sec * 1000 <= nowMs) return 'Reset due'
  return resetsIn(sec, nowMs)
}

export function usedLine(used: number | undefined): string | null {
  const text = qty(used)
  if (text == null) return null
  return `used ${text}%`
}

/** The pace sentence for on_pace, spare, and over. Missing numbers drop their phrase. */
export function paceLine(pool: ProgressPool): string | null {
  if (pool.verdict !== 'on_pace' && pool.verdict !== 'spare' && pool.verdict !== 'over') return null
  const parts: string[] = []
  const elapsed = qty(pool.elapsed_pct)
  if (elapsed != null) parts.push(`${elapsed}% of the window gone`)
  const left = qty(pool.left_pct)
  if (left != null) parts.push(`${left} pts left`)
  const allow = qty(pool.allow_per_day)
  if (allow != null) parts.push(`${allow}/day allowed`)
  const rate = qty(pool.rate_per_day)
  if (rate != null) {
    const basis = pool.rate_basis?.trim() ?? ''
    parts.push(basis !== '' ? `${rate}/day lately (${basis})` : `${rate}/day lately`)
  }
  const projected = qty(pool.projected)
  if (projected != null) parts.push(`about ${projected}% at reset`)
  return parts.length > 0 ? parts.join(' · ') : null
}

function overLine(pool: ProgressPool): string {
  const run = isoSeconds(pool.runs_out_at)
  const reset = isoSeconds(pool.resets_at)
  const parts: string[] = []
  if (run != null) {
    const when = formatLocalDateTime(run * 1000)
    if (when !== '') parts.push(`Runs out ${when}`)
  }
  if (run != null && reset != null && reset > run) parts.push(`${formatDuration(reset - run)} before the reset`)
  return parts.join(', ')
}

export function verdictChip(pool: ProgressPool): { text: string; tone: QuotaTone } | null {
  switch (pool.verdict) {
    case 'on_pace':
      return { text: 'On pace', tone: 'neutral' }
    case 'spare': {
      const spare = qty(pool.spare_pct)
      return {
        text: spare != null ? `Spare — about ${spare} pts will expire unused` : 'Spare',
        tone: 'positive',
      }
    }
    case 'over': {
      const text = overLine(pool)
      return text === '' ? null : { text, tone: 'warning' }
    }
    case 'too_early':
      return { text: 'Too early to tell', tone: 'neutral' }
    case 'short':
      return { text: 'Burst limit', tone: 'neutral' }
    case 'sublimit':
      return { text: 'Sub-limit of another pool', tone: 'neutral' }
    case 'unknown': {
      const note = pool.note?.trim() ?? ''
      return { text: note === '' ? 'Not readable' : `Not readable ${note}`, tone: 'neutral' }
    }
    case 'reset_due':
      return { text: 'Reset due', tone: 'warning' }
    default:
      return null
  }
}

export function poolsByVendor(pools: readonly ProgressPool[]): { vendor: string; pools: ProgressPool[] }[] {
  const groups: { vendor: string; pools: ProgressPool[] }[] = []
  const index = new Map<string, number>()
  for (const pool of pools) {
    const vendor = pool.vendor?.trim() ?? ''
    const at = index.get(vendor)
    if (at == null) {
      index.set(vendor, groups.length)
      groups.push({ vendor, pools: [pool] })
    } else {
      groups[at]?.pools.push(pool)
    }
  }
  return groups
}

export function barPercent(value: number | undefined): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null
  return Math.min(100, Math.max(0, value))
}
