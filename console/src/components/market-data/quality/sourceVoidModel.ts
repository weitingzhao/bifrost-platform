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
  | { kind: 'void'; acked: number; total: number }
  /** Missing beyond anything acknowledged. */
  | { kind: 'gaps'; actionable: number; acked: number; total: number }

export type GapReading = {
  loading: boolean
  /** Transport or proxy error, if any. */
  error: string | null
  count: number | null
  /** The plugin's own explanation for a zero — a zero with a note is not a zero. */
  note: string | null
}

export function judgeGaps(reading: GapReading, ack: SourceVoidEntry | null | undefined): GapVerdict {
  if (reading.loading) return { kind: 'pending' }
  if (reading.error != null) return { kind: 'unknown', reason: reading.error }
  if (reading.count == null) return { kind: 'unknown', reason: 'no count returned' }
  if (reading.count === 0 && reading.note != null) {
    return { kind: 'unknown', reason: reading.note }
  }

  const total = reading.count
  const isVoid = ack?.is_void === true
  const acked = isVoid ? Math.max(0, ack?.acked_gap_count ?? 0) : 0

  if (total === 0) return isVoid ? { kind: 'void', acked, total } : { kind: 'ok' }

  const actionable = Math.max(0, total - acked)
  if (isVoid && actionable === 0) return { kind: 'void', acked, total }
  return { kind: 'gaps', actionable, acked, total }
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
      return `${v.actionable.toLocaleString('en-US')} gaps`
  }
}

/** The evidence line under the tag — what was counted, and what was forgiven. */
export function verdictDetail(v: GapVerdict): string | null {
  switch (v.kind) {
    case 'unknown':
      return v.reason
    case 'void':
      return v.total === 0
        ? 'acknowledged as never published by the vendor'
        : `${v.total.toLocaleString('en-US')} missing, all within the ${v.acked.toLocaleString('en-US')} acknowledged as vendor-void`
    case 'gaps':
      return v.acked > 0
        ? `${v.total.toLocaleString('en-US')} missing − ${v.acked.toLocaleString('en-US')} acknowledged`
        : null
    default:
      return null
  }
}

/** Sections that need a look stay open; a settled one collapses. */
export function shouldCollapse(v: GapVerdict): boolean {
  return v.kind === 'ok' || v.kind === 'void'
}
