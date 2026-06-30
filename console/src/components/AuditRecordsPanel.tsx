import { useMemo, useState } from 'react'
import {
  DenseTag,
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
  SegmentControl,
  type DenseTagVariant,
} from '@bifrost/ui'
import type { AuditRecord } from '@/api/types'
import {
  ACTUATION_CATEGORY_OPTIONS,
  filterAuditByCategory,
  formatMigrateWaveAuditLabel,
  isMigrateWaveAudit,
  type ActuationCategory,
} from '@/lib/audit/actuationCatalog'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'

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

function actionLabel(record: AuditRecord): string {
  if (isMigrateWaveAudit(record)) return formatMigrateWaveAuditLabel(record)
  return record.action
}

interface AuditRecordsPanelProps {
  records: AuditRecord[]
  isLoading: boolean
  limit?: number
  title?: string
  onViewAll?: () => void
  /** When false, hide the header refresh control (e.g. embedded preview). */
  showRefresh?: boolean
  /** Initial category filter (e.g. migrate-wave on Audit page deep link). */
  initialCategory?: ActuationCategory
  /** Show category filter chips. */
  showCategoryFilter?: boolean
}

export function AuditRecordsPanel({
  records,
  isLoading,
  limit = 20,
  title = 'Audit',
  onViewAll,
  showRefresh = true,
  initialCategory = 'all',
  showCategoryFilter = true,
}: AuditRecordsPanelProps) {
  const [category, setCategory] = useState<ActuationCategory>(initialCategory)
  const filtered = useMemo(
    () => filterAuditByCategory(records, category),
    [records, category],
  )
  const visible = filtered.slice(0, limit)
  const qc = useQueryClient()
  const auditFetching = useIsFetching({ queryKey: ['platform', 'audit'] }) > 0

  return (
    <OpsSection
      title={title}
      actions={
        <div className="flex items-center gap-3">
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {isLoading ? '...' : `${filtered.length}${category !== 'all' ? ` / ${records.length}` : ''} records`}
          </span>
          {showRefresh ? (
            <SectionRefreshButton
              isFetching={auditFetching || isLoading}
              onClick={() => void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })}
            />
          ) : null}
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
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      {showCategoryFilter && (
        <div className="border-b border-[var(--border)] px-3 py-2">
          <SegmentControl
            value={category}
            onChange={v => setCategory(v as ActuationCategory)}
            options={ACTUATION_CATEGORY_OPTIONS.map(o => ({ value: o.id, label: o.label }))}
            size="sm"
          />
        </div>
      )}
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
                {isLoading ? 'Loading...' : category === 'all' ? 'No actuation records yet' : 'No records in this category'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            visible.map(record => (
              <DenseTableRow key={record.id}>
                <DenseTableCell className="font-mono-tabular whitespace-nowrap" title={new Date(record.at).toLocaleString()}>
                  {relativeTime(record.at)}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{record.actor}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{actionLabel(record)}</DenseTableCell>
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
    </OpsSection>
  )
}
