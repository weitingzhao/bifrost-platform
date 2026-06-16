import { DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell, type DenseTagVariant } from '@bifrost/ui'
import type { AuditRecord } from '@/api/types'

function relativeTime(isoDate: string): string {
  const diff = Date.now() - new Date(isoDate).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  const days = Math.floor(hrs / 24)
  return `${days}d ago`
}

const STATUS_VARIANT: Record<string, DenseTagVariant> = {
  ok: 'neutral',
  success: 'neutral',
  error: 'danger',
  failed: 'danger',
  noop: 'neutral',
}

function statusVariant(status: string): DenseTagVariant {
  return STATUS_VARIANT[status.toLowerCase()] ?? 'category'
}

interface AuditRecordsPanelProps {
  records: AuditRecord[]
  isLoading: boolean
  limit?: number
  title?: string
  onViewAll?: () => void
}

export function AuditRecordsPanel({
  records,
  isLoading,
  limit = 20,
  title = 'Audit',
  onViewAll,
}: AuditRecordsPanelProps) {
  const visible = records.slice(0, limit)

  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="flex items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
        <h2 className="m-0 text-sm font-semibold">{title}</h2>
        <div className="flex items-center gap-3">
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {isLoading ? '...' : `${records.length} records`}
          </span>
          {onViewAll != null && records.length > 0 && (
            <button
              type="button"
              className="text-[var(--text-dense-meta)] text-[var(--primary)] underline-offset-2 hover:underline"
              onClick={onViewAll}
            >
              View full audit log
            </button>
          )}
        </div>
      </header>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Time</DenseTableHead>
            <DenseTableHead>Actor</DenseTableHead>
            <DenseTableHead>Action</DenseTableHead>
            <DenseTableHead>Target</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Detail</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {visible.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={6} className="text-[var(--muted-foreground)]">
                {isLoading ? 'Loading...' : 'No actuation records yet'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            visible.map(record => (
              <DenseTableRow key={record.id}>
                <DenseTableCell className="font-mono-tabular whitespace-nowrap" title={new Date(record.at).toLocaleString()}>
                  {relativeTime(record.at)}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{record.actor}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{record.action}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{record.target}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={statusVariant(record.status)}>{record.status}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>{record.detail}</DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </section>
  )
}
