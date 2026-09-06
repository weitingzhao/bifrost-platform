export function flexStatusVariant(
  status: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  const s = status.toLowerCase()
  if (s === 'done' || s === 'success' || s === 'on_plan') return 'success'
  if (s === 'running' || s === 'pending' || s === 'due') return 'info'
  if (s === 'failed' || s === 'error' || s === 'missed' || s === 'late') return 'danger'
  if (s === 'no_data') return 'neutral'
  return 'neutral'
}

export function flexReachToVerdict(reach: 'ok' | 'degraded' | 'fail' | 'unknown'): {
  lamp: 'ok' | 'degraded' | 'fail' | 'unknown'
  tagLabel: string
  tagVariant: 'success' | 'warning' | 'danger' | 'neutral'
} {
  switch (reach) {
    case 'ok':
      return { lamp: 'ok', tagLabel: 'OK', tagVariant: 'success' }
    case 'degraded':
      return { lamp: 'degraded', tagLabel: 'DEGRADED', tagVariant: 'warning' }
    case 'fail':
      return { lamp: 'fail', tagLabel: 'FAIL', tagVariant: 'danger' }
    default:
      return { lamp: 'unknown', tagLabel: 'UNKNOWN', tagVariant: 'neutral' }
  }
}

export function slotAdherenceKind(adherence: string | undefined): 'ok' | 'scheduled' | 'missing' | 'unknown' {
  const a = (adherence ?? '').toLowerCase()
  if (a === 'on_plan') return 'ok'
  if (a === 'late' || a === 'due') return 'scheduled'
  if (a === 'no_data') return 'unknown'
  if (a === 'missed') return 'missing'
  return 'unknown'
}

export function shortBrokerageTable(name: string | undefined): string {
  if (name == null || name === '') return '—'
  return name.replace(/^brokerage\./, '').replace(/^public\./, 'p.')
}

export function shortDay(iso: string | null | undefined): string {
  if (iso == null || iso === '') return '—'
  return iso.slice(0, 10).slice(5)
}

export function kpiVariantToTone(
  v: 'success' | 'warning' | 'danger' | 'neutral',
): 'ok' | 'scheduled' | 'missing' | 'unknown' {
  if (v === 'success') return 'ok'
  if (v === 'warning') return 'scheduled'
  if (v === 'danger') return 'missing'
  return 'unknown'
}

export function kpiAgeVariant(
  ageSecs: number | null | undefined,
  warnThreshold: number,
  dangerThreshold: number,
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (ageSecs == null) return 'neutral'
  if (ageSecs > dangerThreshold) return 'danger'
  if (ageSecs > warnThreshold) return 'warning'
  return 'success'
}

export function formatFlexResult(result: unknown): string {
  if (result == null) return '—'
  if (typeof result === 'string') return result.slice(0, 80)
  try {
    return JSON.stringify(result).slice(0, 80)
  } catch {
    return String(result)
  }
}

export type FlexKpiTone = 'ok' | 'scheduled' | 'missing' | 'unknown'

export function flexKpiCardMeta(tone: FlexKpiTone): {
  pass: boolean
  tag: string
  tagVariant: 'success' | 'warning' | 'danger' | 'neutral'
  ringBucket: 'ready' | 'thin' | 'blocked' | 'unknown'
} {
  switch (tone) {
    case 'ok':
      return { pass: true, tag: 'PASS', tagVariant: 'success', ringBucket: 'ready' }
    case 'scheduled':
      return { pass: false, tag: 'WARN', tagVariant: 'warning', ringBucket: 'thin' }
    case 'missing':
      return { pass: false, tag: 'FAIL', tagVariant: 'danger', ringBucket: 'blocked' }
    default:
      return { pass: false, tag: 'N/A', tagVariant: 'neutral', ringBucket: 'unknown' }
  }
}

export function lastRunTone(status: string | null | undefined): FlexKpiTone {
  const s = (status ?? '').toLowerCase()
  if (s === 'failed') return 'missing'
  if (s === 'done') return 'ok'
  if (s === 'running' || s === 'pending') return 'scheduled'
  return 'unknown'
}


/** Tag variant for a self-check verdict (GET /flex/ops/check). */
export function checkVerdictVariant(
  verdict: string | null | undefined,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  switch ((verdict ?? '').toLowerCase()) {
    case 'ok':
      return 'success'
    case 'failed':
    case 'missed':
      return 'danger'
    case 'throttled':
    case 'waiting':
    case 'attention':
      return 'warning'
    case 'running':
    case 'queued':
      return 'info'
    default:
      return 'neutral'
  }
}

/** Lamp for the verdict strip: red only when a human has to act. */
export function checkVerdictLamp(
  verdict: string | null | undefined,
): 'ok' | 'degraded' | 'fail' | 'unknown' {
  switch ((verdict ?? '').toLowerCase()) {
    case 'ok':
    case 'running':
    case 'queued':
      return 'ok'
    case 'failed':
    case 'missed':
      return 'fail'
    case 'throttled':
    case 'waiting':
    case 'attention':
    case 'idle':
      return 'degraded'
    default:
      return 'unknown'
  }
}

/** "in 27m" / "now" from a seconds-until value. */
export function fmtUntil(secs: number | null | undefined): string {
  if (secs == null) return '—'
  if (secs <= 0) return 'now'
  if (secs < 3600) return `in ${Math.ceil(secs / 60)}m`
  const h = Math.floor(secs / 3600)
  const m = Math.round((secs % 3600) / 60)
  return m ? `in ${h}h ${m}m` : `in ${h}h`
}
