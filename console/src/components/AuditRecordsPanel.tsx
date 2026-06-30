import { Fragment, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import {
  DenseTag,
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
  DenseTableDetailRow,
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
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const toggleExpanded = (id: string) =>
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
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
            <DenseTableHead className="w-[5%] whitespace-nowrap" />
            <DenseTableHead className="w-[12%] whitespace-nowrap">Time</DenseTableHead>
            <DenseTableHead className="w-[14%] whitespace-nowrap">Actor</DenseTableHead>
            <DenseTableHead className="w-[16%] whitespace-nowrap">Action</DenseTableHead>
            <DenseTableHead className="w-[16%] whitespace-nowrap">Target</DenseTableHead>
            <DenseTableHead className="w-[10%] whitespace-nowrap">Status</DenseTableHead>
            <DenseTableHead className="w-[27%]">Detail</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {visible.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={7} className="text-[var(--muted-foreground)]">
                {isLoading ? 'Loading...' : category === 'all' ? 'No actuation records yet' : 'No records in this category'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            visible.map(record => {
              const detail = record.detail ?? ''
              const isExpanded = expanded.has(record.id)
              const hasDetail = detail.trim().length > 0
              return (
                <Fragment key={record.id}>
                  <DenseTableRow
                    className={hasDetail ? 'cursor-pointer' : undefined}
                    onClick={hasDetail ? () => toggleExpanded(record.id) : undefined}
                  >
                    <DenseTableCell className="text-center align-middle text-[var(--muted-foreground)]">
                      {hasDetail ? (
                        isExpanded ? (
                          <ChevronDown className="inline h-3.5 w-3.5" />
                        ) : (
                          <ChevronRight className="inline h-3.5 w-3.5" />
                        )
                      ) : null}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular whitespace-nowrap" title={new Date(record.at).toLocaleString()}>
                      {relativeTime(record.at)}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular truncate" title={record.actor}>{record.actor}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular truncate" title={actionLabel(record)}>{actionLabel(record)}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular truncate" title={record.target}>{record.target}</DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={statusVariant(record.status)}>{record.status}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className="truncate text-[var(--muted-foreground)]" title={hasDetail ? detail : undefined}>
                      {hasDetail ? detail.replace(/\s+/g, ' ').trim() : '—'}
                    </DenseTableCell>
                  </DenseTableRow>
                  {hasDetail && isExpanded && (
                    <DenseTableDetailRow>
                      <DenseTableCell colSpan={7}>
                        <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[var(--text-dense-meta)] leading-relaxed text-[var(--foreground)]">
                          {detail}
                        </pre>
                      </DenseTableCell>
                    </DenseTableDetailRow>
                  )}
                </Fragment>
              )
            })
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
