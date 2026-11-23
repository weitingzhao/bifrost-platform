import { Fragment, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import {
  DenseTag,
  DenseTagButton,
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
  DenseTableDetailRow,
  cn,
  type DenseTagVariant,
} from '@bifrost/ui'
import type { AuditRecord } from '@/api/auditTypes'
import {
  ACTUATION_CATEGORY_OPTIONS,
  ACTUATION_CATEGORY_VARIANT,
  ACTUATION_ORIGIN_OPTIONS,
  actuationCategory,
  actuationCategoryLabel,
  actuationOrigin,
  actuationOriginLabel,
  actuationOriginTagClass,
  filterAuditRecords,
  type ActuationCategory,
  type ActuationOrigin,
} from '@/lib/audit/actuationCatalog'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { LedgerGrid } from '@bifrost/ui/city/ledger'
import { actionLabel, auditLedgerTable, relativeTime } from './auditLedger'

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

function categoryFilterVariant(id: ActuationCategory): DenseTagVariant {
  if (id === 'all') return 'neutral'
  return ACTUATION_CATEGORY_VARIANT[id]
}

function filterChipClass(selected: boolean): string {
  return cn(
    selected ? 'ring-1 ring-current/40 brightness-110' : 'opacity-55 hover:opacity-90',
  )
}

interface AuditRecordsPanelProps {
  records: AuditRecord[]
  isLoading: boolean
  limit?: number
  /** Omit when a page-level Verdict already carries the section identity. */
  title?: string | null
  onViewAll?: () => void
  /** When false, hide the header refresh control (e.g. embedded preview). */
  showRefresh?: boolean
  /** Initial category filter (e.g. migrate-wave on Audit page deep link). */
  initialCategory?: ActuationCategory
  /** Show category filter chips. */
  showCategoryFilter?: boolean
  /**
   * Every kept record in the shared Ledger (filter, sort, 50 a page) instead of the first `limit` rows;
   * the selected record's detail shows under the table. Records → Audit (Ledger line, 2026-11-23: the
   * platform keeps 500 and the page showed the first 100, so 101–500 could not be seen).
   */
  ledger?: boolean
}

export function AuditRecordsPanel({
  records,
  isLoading,
  limit = 20,
  title,
  onViewAll,
  showRefresh = true,
  initialCategory = 'all',
  showCategoryFilter = true,
  ledger = false,
}: AuditRecordsPanelProps) {
  const [selected, setSelected] = useState('')
  const [category, setCategory] = useState<ActuationCategory>(initialCategory)
  const [origin, setOrigin] = useState<ActuationOrigin | 'all'>('all')
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const toggleExpanded = (id: string) =>
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const filtered = useMemo(
    () => filterAuditRecords(records, category, origin),
    [records, category, origin],
  )
  const filtersActive = category !== 'all' || origin !== 'all'
  const visible = filtered.slice(0, limit)
  const ledgerTable = useMemo(() => (ledger ? auditLedgerTable(filtered) : null), [ledger, filtered])
  const selectedRecord = ledger && selected ? records.find(r => r.id === selected) : undefined
  const qc = useQueryClient()
  const auditFetching = useIsFetching({ queryKey: ['platform', 'audit'] }) > 0
  const hasTitle = title != null && title !== ''

  return (
    <OpsSection
      title={hasTitle ? title : undefined}
      actions={
        <div className="flex items-center gap-3">
          {(hasTitle || filtersActive) && (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {isLoading
                ? '…'
                : filtersActive
                  ? `${filtered.length} / ${records.length} matching`
                  : `${filtered.length} records`}
            </span>
          )}
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
        <div className="flex flex-col gap-2 border-b border-[var(--table-rule)] px-3 py-2">
          <div
            className="flex flex-wrap items-center gap-1.5"
            role="group"
            aria-label="Actuation category filter"
          >
            <span className="mr-1 shrink-0 text-[var(--text-dense-meta)] font-medium text-muted-foreground">
              Category:
            </span>
            {ACTUATION_CATEGORY_OPTIONS.map(opt => {
              const selected = category === opt.id
              return (
                <DenseTagButton
                  key={opt.id}
                  size="pill"
                  variant={categoryFilterVariant(opt.id)}
                  aria-pressed={selected}
                  className={filterChipClass(selected)}
                  onClick={() => setCategory(opt.id)}
                >
                  {opt.label}
                </DenseTagButton>
              )
            })}
          </div>
          <div
            className="flex flex-wrap items-center gap-1.5"
            role="group"
            aria-label="Actuation origin filter"
          >
            <span className="mr-1 shrink-0 text-[var(--text-dense-meta)] font-medium text-muted-foreground">
              Origin:
            </span>
            {ACTUATION_ORIGIN_OPTIONS.map(opt => {
              const selected = origin === opt.id
              const originTone =
                opt.id === 'all' ? 'border-transparent bg-[color-mix(in_srgb,currentColor_15%,transparent)] text-muted-foreground font-semibold' : actuationOriginTagClass(opt.id)
              return (
                <DenseTagButton
                  key={opt.id}
                  size="pill"
                  variant="neutral"
                  aria-pressed={selected}
                  className={cn(originTone, filterChipClass(selected))}
                  onClick={() => setOrigin(opt.id)}
                >
                  {opt.label}
                </DenseTagButton>
              )
            })}
          </div>
        </div>
      )}
      {ledgerTable != null ? (
        // Table spacing, and fail in the destructive colour as the old dense tag had it, are the host's until the
        // Ledger carries its own (Ledger line follow-up; spacing as in CI's RunSourceReport).
        <div className="min-w-0 px-3 py-2 text-xs [&_table]:w-full [&_th]:p-2 [&_th]:text-left [&_td]:p-2 [&_td]:align-top [&_td]:whitespace-nowrap [&_button]:whitespace-nowrap! [&_button]:text-left [&_td:last-child]:whitespace-normal [&_td:last-child]:min-w-[18rem] [&_td:last-child]:[overflow-wrap:anywhere] [&_tr]:border-b [&_tr]:border-[var(--table-rule)] [&_.ledger-controls]:flex [&_.ledger-controls]:flex-wrap [&_.ledger-controls]:items-center [&_.ledger-controls]:gap-3 [&_.ledger-grid_p]:my-1 [&_.ledger-grid_p]:text-[var(--muted-foreground)] [&_nav]:flex [&_nav]:gap-2 [&_.g2-fail]:text-[var(--destructive)]">
          {isLoading ? (
            <p className="m-0 text-[var(--muted-foreground)]">…</p>
          ) : (
            <LedgerGrid
              table={ledgerTable}
              selected={selected}
              onSelect={key => setSelected(key === selected ? '' : key)}
              scope={`Platform audit · the newest ${records.length} records the platform keeps${filtersActive ? ' · category/origin filter on' : ''}`}
            />
          )}
          {selectedRecord != null && (
            <section aria-label="Selected audit record" className="mt-2 border-t border-[var(--table-rule)] pt-2">
              <p className="m-0 font-mono-tabular text-[var(--text-dense-meta)]">
                {new Date(selectedRecord.at).toLocaleString()} · {selectedRecord.actor} · {actionLabel(selectedRecord)} · {selectedRecord.target} · {selectedRecord.status}
              </p>
              <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[var(--text-dense-meta)] leading-relaxed text-[var(--foreground)]">
                {(selectedRecord.detail ?? '').trim() || 'No detail recorded.'}
              </pre>
            </section>
          )}
        </div>
      ) : (
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="w-[4%] whitespace-nowrap" />
            <DenseTableHead className="w-[10%] whitespace-nowrap">Time</DenseTableHead>
            <DenseTableHead className="w-[11%] whitespace-nowrap">Category</DenseTableHead>
            <DenseTableHead className="w-[9%] whitespace-nowrap">Origin</DenseTableHead>
            <DenseTableHead className="w-[11%] whitespace-nowrap">Actor</DenseTableHead>
            <DenseTableHead className="w-[14%] whitespace-nowrap">Action</DenseTableHead>
            <DenseTableHead className="w-[14%] whitespace-nowrap">Target</DenseTableHead>
            <DenseTableHead className="w-[9%] whitespace-nowrap">Status</DenseTableHead>
            <DenseTableHead className="w-[18%]">Detail</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {visible.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={9} className="text-[var(--muted-foreground)]">
                {isLoading
                  ? '…'
                  : filtersActive
                    ? 'No records match filters'
                    : 'No actuation records yet'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            visible.map(record => {
              const detail = record.detail ?? ''
              const isExpanded = expanded.has(record.id)
              const hasDetail = detail.trim().length > 0
              const cat = actuationCategory(record.action)
              const rowOrigin = actuationOrigin(record)
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
                    <DenseTableCell>
                      <DenseTag variant={ACTUATION_CATEGORY_VARIANT[cat]}>{actuationCategoryLabel(cat)}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag
                        variant="neutral"
                        className={actuationOriginTagClass(rowOrigin)}
                        title={
                          rowOrigin === 'agent'
                            ? 'Agent/job terminal or agent.* write (Actor may still be the operator who started the job)'
                            : rowOrigin === 'system'
                              ? 'Platform/system principal (ops-agent, queue-sweep, …)'
                              : 'Operator/admin write or human control of an Agent job (start / respond / cancel)'
                        }
                      >
                        {actuationOriginLabel(rowOrigin)}
                      </DenseTag>
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
                      <DenseTableCell colSpan={9}>
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
      )}
    </OpsSection>
  )
}
