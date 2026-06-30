import { Button, DenseTag } from '@bifrost/ui'
import type { AuditRecord } from '@/api/types'
import {
  formatMigrateWaveAuditLabel,
  migrateWaveAuditForStream,
} from '@/lib/audit/actuationCatalog'

function relativeTime(isoDate: string): string {
  const diff = Date.now() - new Date(isoDate).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

/** Recent migrate-wave actuation rows — mirrors platform-api audit log (WRITE_PATHS). */
export function BriefingWaveAuditPanel({
  streamId,
  records,
  isLoading,
  onOpenAudit,
}: {
  streamId: string
  records: AuditRecord[]
  isLoading?: boolean
  onOpenAudit?: () => void
}) {
  const recent = migrateWaveAuditForStream(records, streamId, 5)

  return (
    <div className="rounded border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="m-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
          Wave actuation audit · {streamId}
        </p>
        {onOpenAudit != null && (
          <Button type="button" variant="ghost" size="sm" className="h-auto px-0 py-0 text-[var(--text-dense-meta)]" onClick={onOpenAudit}>
            View full audit →
          </Button>
        )}
      </div>
      {isLoading ? (
        <p className="m-0 mt-1.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading audit…</p>
      ) : recent.length === 0 ? (
        <p className="m-0 mt-1.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No migrate-wave actuation yet — deliver / sign-off writes appear here (platform-api audit).
        </p>
      ) : (
        <ul className="m-0 mt-1.5 flex list-none flex-col gap-1.5 p-0">
          {recent.map(r => (
            <li key={r.id} className="flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
              <span className="font-mono-tabular text-[var(--text-dense-caption)] text-[var(--muted-foreground)]" title={new Date(r.at).toLocaleString()}>
                {relativeTime(r.at)}
              </span>
              <DenseTag variant={r.status === 'ok' ? 'success' : 'danger'}>{r.status}</DenseTag>
              <span className="min-w-0 flex-1">{formatMigrateWaveAuditLabel(r)}</span>
              <span className="font-mono-tabular text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">{r.actor}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
