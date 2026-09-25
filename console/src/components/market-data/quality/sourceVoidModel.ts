/**
 * What a financial gap report means once the operator's source-void judgement
 * is read alongside it.
 *
 * Three readings used to collapse into one green tag: the plugin answered
 * `count: 0` both when it had checked and found nothing and when it could not
 * check at all (it returns a `note` in that case), and nothing in the console
 * knew that five of these six types have been acknowledged as vendor-void
 * since May. Measured 2026-09-25: all six SEPA gap sections read OK while the
 * underlying gate was short-circuiting, and every void the Owner had set on
 * the retired Trade page was invisible here.
 *
 * So: a report that could not be produced is `unknown`, never `ok`; an
 * acknowledged gap is `void` — a boundary, not work; and a gap larger than
 * what was acknowledged keeps the remainder actionable.
 */
import type { SourceVoidDataType, SourceVoidEntry } from '@/api/marketDataPlugin'

/** The gap endpoint's report_type ↔ the void table's data_type. Not the same words. */
export const VOID_DATA_TYPE: Record<string, SourceVoidDataType> = {
  income_statement: 'income_statements',
  balance_sheet: 'balance_sheets',
  cash_flow_statement: 'cash_flows',
  ratios: 'ratios',
  short_interest: 'short_interest',
  short_volume: 'short_volume',
}

export type GapVerdict =
  /** The read has not answered yet. Not a finding. */
  | { kind: 'pending' }
  /** The read failed, or the plugin said it could not check. Not a finding either. */
  | { kind: 'unknown'; reason: string }
  /** Checked, and nothing is missing. */
  | { kind: 'ok' }
  /** Missing, and the operator has said the vendor never had it. */
  | { kind: 'void'; acked: number; total: number; capped: boolean }
  /** Missing beyond anything acknowledged. */
  | { kind: 'gaps'; actionable: number; acked: number; total: number; capped: boolean }

export type GapReading = {
  loading: boolean
  /** Transport or proxy error, if any. */
  error: string | null
  count: number | null
  /** The plugin's own explanation for a zero — a zero with a note is not a zero. */
  note: string | null
  /** The `limit` the count was asked for. A count that reaches it is a floor, not a total. */
  limit: number
  /** The plugin's uncapped count, when it supplies one — then nothing is a floor. */
  total?: number | null
}

export function judgeGaps(reading: GapReading, ack: SourceVoidEntry | null | undefined): GapVerdict {
  if (reading.loading) return { kind: 'pending' }
  if (reading.error != null) return { kind: 'unknown', reason: reading.error }
  if (reading.count == null) return { kind: 'unknown', reason: 'no count returned' }
  if (reading.count === 0 && reading.note != null) {
    return { kind: 'unknown', reason: reading.note }
  }

  // The endpoint counts the rows it returned, and it returns at most `limit`,
  // so a count that reaches the limit says "at least this many" and no more.
  // Plugin 0.38.3 answers the same statement counted without the cap; where
  // that number is present nothing is a floor and the arithmetic below is safe.
  const total = reading.total ?? reading.count
  const capped = reading.total == null && reading.count >= reading.limit
  const isVoid = ack?.is_void === true
  const acked = isVoid ? Math.max(0, ack?.acked_gap_count ?? 0) : 0

  if (total === 0) return isVoid ? { kind: 'void', acked, total, capped } : { kind: 'ok' }

  // Subtracting the acknowledgement is the plugin's own arithmetic
  // (readiness_summary's *_actionable_gap_count), so it is safe to mirror —
  // but only against a real total. Against a floor it would claim containment
  // it cannot know, so a capped count defers to the standing judgement.
  if (isVoid && (capped || total <= acked)) return { kind: 'void', acked, total, capped }
  const actionable = isVoid ? Math.max(0, total - acked) : total
  return { kind: 'gaps', actionable, acked, total, capped }
}

export type GapTone = 'success' | 'warning' | 'neutral' | 'info'

/** Severity only — a void is slate because it is the plan's edge, not a fault. */
export function verdictTone(v: GapVerdict): GapTone {
  switch (v.kind) {
    case 'ok':
      return 'success'
    case 'gaps':
      return 'warning'
    case 'void':
    case 'unknown':
    case 'pending':
      return 'neutral'
  }
}

export function verdictLabel(v: GapVerdict): string {
  switch (v.kind) {
    case 'pending':
      return '…'
    case 'unknown':
      return 'Cannot check'
    case 'ok':
      return 'OK'
    case 'void':
      return 'Vendor void'
    case 'gaps':
      return v.capped
        ? `${v.actionable.toLocaleString('en-US')}+ gaps`
        : `${v.actionable.toLocaleString('en-US')} gaps`
  }
}

function missing(total: number, capped: boolean): string {
  const n = total.toLocaleString('en-US')
  return capped ? `at least ${n} missing` : `${n} missing`
}

/** The evidence line under the tag — what was counted, and what was forgiven. */
export function verdictDetail(v: GapVerdict): string | null {
  switch (v.kind) {
    case 'unknown':
      return v.reason
    case 'void':
      if (v.total === 0) return 'acknowledged as never published by the vendor'
      return v.capped
        ? `${missing(v.total, true)} · ${v.acked.toLocaleString('en-US')} acknowledged as vendor-void`
        : `${missing(v.total, false)}, all within the ${v.acked.toLocaleString('en-US')} acknowledged as vendor-void`
    case 'gaps':
      if (v.acked > 0) {
        return `${missing(v.total, v.capped)} − ${v.acked.toLocaleString('en-US')} acknowledged`
      }
      return v.capped ? missing(v.total, true) : null
    default:
      return null
  }
}

/** Sections that need a look stay open; a settled one collapses. */
export function shouldCollapse(v: GapVerdict): boolean {
  return v.kind === 'ok' || v.kind === 'void'
}
