export function fmtCount(n: number | null | undefined): string {
  if (n == null) return '—'
  return n.toLocaleString('en-US')
}

export function toneByLevel(
  level: 'ready' | 'thin' | 'blocked' | 'unknown' | 'ok' | 'scheduled' | 'missing' | 'degraded' | 'fail',
): string {
  if (level === 'ready' || level === 'ok') return 'bg-[var(--color-success)]'
  if (level === 'thin' || level === 'scheduled' || level === 'degraded') {
    return 'bg-[var(--color-warning)]'
  }
  if (level === 'blocked' || level === 'missing' || level === 'fail') {
    return 'bg-[var(--color-danger,var(--destructive))]'
  }
  return 'bg-[var(--muted-foreground)]/40'
}

/** Comparable rank for live ticks — numbers, or status words. */
export function tickRank(value: number | string | null | undefined): number | null {
  if (value == null || value === '') return null
  if (typeof value === 'number') return Number.isFinite(value) ? value : null
  const s = value.trim().toLowerCase()
  if (s === 'ok' || s === 'ready' || s === 'today ok') return 2
  if (s === 'degraded' || s === 'thin' || s.startsWith('scheduled')) return 1
  if (s === 'fail' || s === 'missing' || s === 'blocked' || s === 'unknown') return 0
  const n = Number(s.replace(/,/g, ''))
  return Number.isFinite(n) ? n : null
}

export function parseReadyRatio(ready?: string): { n: number; d: number } | null {
  const m = ready?.trim().match(/^(\d+)\s*\/\s*(\d+)$/)
  if (m == null) return null
  return { n: Number(m[1]), d: Number(m[2]) }
}
