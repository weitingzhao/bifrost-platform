import type { Block, Tone } from '@bifrost/ui/city/cards'
import type { AuditRecord } from '@/api/auditTypes'
import {
  actuationCategory,
  actuationCategoryLabel,
  actuationOrigin,
  actuationOriginLabel,
  formatMigrateWaveAuditLabel,
  isMigrateWaveAudit,
} from '@/lib/audit/actuationCatalog'

export function relativeTime(isoDate: string): string {
  const diff = Date.now() - new Date(isoDate).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  const days = Math.floor(hrs / 24)
  return `${days}d ago`
}

export function actionLabel(record: AuditRecord): string {
  if (isMigrateWaveAudit(record)) return formatMigrateWaveAuditLabel(record)
  return record.action
}

/**
 * The audit status in the Ops words. The list is the statuses PROD's 500 kept records carried on 2026-11-23
 * (waiting 257, ok 63, wait 48, approved 27, …); a status not listed here stays unknown, never an unchecked ok.
 */
const STATUS_TONE: Record<string, Tone> = {
  ok: 'ok', success: 'ok', executed: 'ok', delivered: 'ok', released: 'ok', approved: 'ok', accepted: 'ok', answered: 'ok',
  waiting: 'pending', wait: 'pending', pending: 'pending', open: 'pending', started: 'pending', running: 'pending', full_ci_started: 'pending',
  error: 'fail', failed: 'fail',
  refused: 'boundary', 'not covered': 'boundary',
  noop: 'info',
}
export function auditStatusTone(status: string): Tone {
  return STATUS_TONE[status.toLowerCase()] ?? 'unknown'
}

/** The detail cell shows its start; the whole detail is in its hover and under the table when the row is selected. */
const DETAIL_CELL_CHARS = 80
/** `2026-11-23 19:17:47Z`: one width, and sorting the text sorts the time. */
function secondsUtc(iso: string): string {
  const at = new Date(iso)
  return Number.isNaN(at.getTime()) ? iso : `${at.toISOString().slice(0, 19).replace('T', ' ')}Z`
}

type LedgerTable = Extract<Block, { kind: 'table' }>
/** Records → Audit as one Ledger table: the same columns as the dense table, one row per kept record. */
export function auditLedgerTable(records: AuditRecord[]): LedgerTable {
  return {
    kind: 'table',
    head: ['Time', 'Category', 'Origin', 'Actor', 'Action', 'Target', 'Status', 'Detail'].map(label => ({ label })),
    rows: records.map(record => {
      const detail = (record.detail ?? '').replace(/\s+/g, ' ').trim()
      const short = detail.length > DETAIL_CELL_CHARS ? `${detail.slice(0, DETAIL_CELL_CHARS)}…` : detail
      return {
        key: record.id,
        // CardView makes a row selectable only through `open`; it carries the same id as the key.
        open: record.id,
        title: new Date(record.at).toLocaleString(),
        cells: [
          { text: secondsUtc(record.at), title: relativeTime(record.at) },
          actuationCategoryLabel(actuationCategory(record.action)),
          actuationOriginLabel(actuationOrigin(record)),
          record.actor,
          actionLabel(record),
          record.target,
          { text: record.status, tone: auditStatusTone(record.status) },
          detail ? { text: short, title: detail } : '—',
        ],
      }
    }),
    empty: 'No actuation records match',
  }
}
