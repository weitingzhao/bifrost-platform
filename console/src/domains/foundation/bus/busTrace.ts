/** Read-only DTOs of the bus gate (api/internal/bus/gate/gate.go) and the trace timeline the page draws. */

/** One CloudEvents envelope as the gate sends it (relay.Envelope). */
export interface BusEnvelope {
  specversion: string
  id: string
  source: string
  type: string
  time: string
  subject: string
  tenant: string
  env: string
  cursor: string
  ref: string
  actor: string
  key?: string
  change?: string
  causeid?: string
}

export interface BusTraceStep {
  step: string
  at: string
  since?: string
  seconds?: number
  seq?: number
}

/** GET /api/v1/bus/trace?id=ev-<n>. */
export interface BusTraceResponse {
  id: string
  type: string
  subject: string
  actor: string
  cursor: string
  ref: string
  steps: BusTraceStep[]
  /** Why the stream's step is absent (not configured, unreadable, not found in the window). */
  nats?: string
  receipts_error?: string
}

export interface TimelineRow {
  key: string
  label: string
  /** Consumer name of a receipt. */
  consumer?: string
  at: string | null
  /** "+0.42s" since the previous present step; null for the first step or a missing one. */
  delta: string | null
  missing: boolean
  note?: string
  seq?: number
}

export const EVENT_ID_PATTERN = /^ev-\d+$/

/** "+0.042s" / "+1.25s" / "+73.4s"; a negative gap (clock skew between hosts) keeps its sign. */
export function formatDelta(seconds: number): string {
  const sign = seconds < 0 ? '−' : '+'
  const abs = Math.abs(seconds)
  const digits = abs < 1 ? 3 : abs < 10 ? 2 : 1
  return `${sign}${abs.toFixed(digits)}s`
}

function gap(prev: TimelineRow | undefined, step: BusTraceStep): string | null {
  if (prev == null || prev.at == null) return null
  if (step.seconds != null && step.since != null && prev.key === step.since) return formatDelta(step.seconds)
  const a = Date.parse(prev.at)
  const b = Date.parse(step.at)
  if (Number.isNaN(a) || Number.isNaN(b)) return null
  return formatDelta((b - a) / 1000)
}

/**
 * The journey as fixed rows: ledger written → published to NATS → each receipt. A step the trace
 * did not find stays as a missing row with the reason the gate gave, never dropped.
 */
export function traceTimeline(trace: BusTraceResponse): TimelineRow[] {
  const steps = trace.steps ?? []
  const rows: TimelineRow[] = []
  let lastPresent: TimelineRow | undefined
  const add = (row: TimelineRow) => {
    rows.push(row)
    if (!row.missing) lastPresent = row
  }
  const ledger = steps.find(s => s.step === 'ledger')
  add(ledger == null
    ? { key: 'ledger', label: 'Ledger written', at: null, delta: null, missing: true }
    : { key: 'ledger', label: 'Ledger written', at: ledger.at, delta: null, missing: false })
  const stream = steps.find(s => s.step === 'stream')
  add(stream == null
    ? { key: 'stream', label: 'Published to NATS', at: null, delta: null, missing: true, note: trace.nats ?? 'not found' }
    : { key: 'stream', label: 'Published to NATS', at: stream.at, delta: gap(lastPresent, stream), missing: false, seq: stream.seq })
  const receipts = steps.filter(s => s.step.startsWith('receipt '))
  if (receipts.length === 0) {
    add({ key: 'receipt', label: 'Receipt', at: null, delta: null, missing: true,
      note: trace.receipts_error ?? (trace.nats?.startsWith('not configured') ? 'NATS not configured: receipts are not read' : 'no consumer has received it yet') })
  }
  receipts.forEach((s, i) => {
    add({ key: `receipt-${i}`, label: 'Receipt', consumer: s.step.slice('receipt '.length), at: s.at, delta: gap(lastPresent, s), missing: false })
  })
  if (receipts.length > 0 && trace.receipts_error) {
    rows.push({ key: 'receipt-error', label: 'Receipts', at: null, delta: null, missing: true, note: trace.receipts_error })
  }
  return rows
}

/** "14:03:07.412" in local time; the raw text when it does not parse. */
export function clockTime(iso: string): string {
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return iso
  const pad = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}.${pad(t.getMilliseconds(), 3)}`
}
