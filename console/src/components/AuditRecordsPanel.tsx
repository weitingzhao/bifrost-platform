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

const STATUS_CLASS: Record<string, string> = {
  ok: 'badge-ui badge-status-closed',
  success: 'badge-ui badge-status-closed',
  error: 'badge-ui badge-status-blocked',
  failed: 'badge-ui badge-status-blocked',
  noop: 'badge-ui badge-status-pending',
}

function statusBadge(status: string) {
  return STATUS_CLASS[status.toLowerCase()] ?? 'badge-ui'
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
      <div className="dense-table-scroll">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Time</th>
              <th>Actor</th>
              <th>Action</th>
              <th>Target</th>
              <th>Status</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 ? (
              <tr>
                <td colSpan={6} className="text-[var(--muted-foreground)]">
                  {isLoading ? 'Loading...' : 'No actuation records yet'}
                </td>
              </tr>
            ) : (
              visible.map(record => (
                <tr key={record.id}>
                  <td className="font-mono-tabular whitespace-nowrap" title={new Date(record.at).toLocaleString()}>
                    {relativeTime(record.at)}
                  </td>
                  <td className="font-mono-tabular">{record.actor}</td>
                  <td className="font-mono-tabular">{record.action}</td>
                  <td className="font-mono-tabular">{record.target}</td>
                  <td>
                    <span className={statusBadge(record.status)}>{record.status}</span>
                  </td>
                  <td>{record.detail}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
