import { useState } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  DenseTagButton,
  SegmentControl,
  cn,
} from '@bifrost/ui'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchDataCloneSchedule, fetchDataFreshness } from '@/api/cluster'
import type { DataCloneGroup, DataFreshnessDatabase } from '@/api/clusterTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import {
  RequestDataClone,
  RequestDataCloneSchedule,
} from '@/components/shell/clusterActionRequests'

/**
 * Selective sync offers the clone groups the clone source's application publishes in its data
 * probe (`clone_groups` on the freshness response) — the platform names no tables itself.
 * Each group already includes every table that references it. Nothing is selected by default;
 * the API still refuses a selection that leaves out a referencing table and names those
 * tables, which the panel then offers to add.
 */
type CloneSyncMode = 'full' | 'selective'

function freshnessLagDays(db: DataFreshnessDatabase): number | null {
  if (db.lag_vs_prod_days != null) return db.lag_vs_prod_days
  if (db.stale_days != null) return db.stale_days
  return null
}

function freshnessBadgeVariant(
  db: DataFreshnessDatabase,
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (db.verdict === 'reference') return 'neutral'
  if (db.verdict === 'unknown') return 'neutral'
  const lag = freshnessLagDays(db)
  if (lag == null) return 'neutral'
  if (lag < 3) return 'success'
  if (lag < 7) return 'warning'
  return 'danger'
}

function freshnessBadgeLabel(db: DataFreshnessDatabase): string {
  if (db.verdict === 'reference') return 'reference'
  if (db.verdict === 'unknown') return 'unknown'
  if (db.verdict === 'aging') {
    const lag = freshnessLagDays(db)
    return lag == null ? 'aging' : `aging · ${lag.toFixed(1)}d lag`
  }
  if (db.verdict === 'stale') {
    const lag = freshnessLagDays(db)
    return lag == null ? 'stale' : `stale · ${lag.toFixed(0)}d lag`
  }
  const lag = freshnessLagDays(db)
  if (lag == null) return 'unknown'
  if (lag < 3) return `fresh · <3d lag`
  if (lag < 7) return `aging · ${lag.toFixed(1)}d lag`
  return `stale · ${lag.toFixed(0)}d lag`
}

export function DataFreshnessPanel({
  canAdmin,
  onOpenFullPostgres,
  title = 'Data Freshness',
}: {
  canAdmin: boolean
  onOpenFullPostgres?: () => void
  title?: string
}) {
  const qc = useQueryClient()
  const [syncMode, setSyncMode] = useState<CloneSyncMode>('full')
  const [selectedTables, setSelectedTables] = useState<string[]>([])

  const freshnessQuery = useQuery({
    queryKey: ['cluster', 'data-freshness'],
    queryFn: fetchDataFreshness,
    refetchInterval: 60_000,
  })
  const scheduleQuery = useQuery({
    queryKey: ['cluster', 'data-clone-schedule'],
    queryFn: fetchDataCloneSchedule,
    refetchInterval: 60_000,
  })
  const toggleTable = (table: string) => {
    setSelectedTables(prev =>
      prev.includes(table) ? prev.filter(t => t !== table) : [...prev, table],
    )
  }
  const cloneGroups: DataCloneGroup[] = freshnessQuery.data?.clone_groups ?? []
  const cloneGroupsDetail = freshnessQuery.data?.clone_groups_detail
  const groupTables = [...new Set(cloneGroups.flatMap(g => g.tables))]
  const isGroupSelected = (group: DataCloneGroup) => group.tables.every(t => selectedTables.includes(t))
  const toggleGroup = (group: DataCloneGroup) => {
    setSelectedTables(prev =>
      group.tables.every(t => prev.includes(t))
        ? prev.filter(t => !group.tables.includes(t))
        : [...prev, ...group.tables.filter(t => !prev.includes(t))],
    )
  }
  const tableChips = [...groupTables, ...selectedTables.filter(t => !groupTables.includes(t))]
  const selectiveReady = syncMode === 'full' || selectedTables.length > 0

  const databases = freshnessQuery.data?.databases ?? []
  const schedule = scheduleQuery.data

  return (
    <>
      <OpsSection
        title={title}
        description="Trade DB clone lag vs bifrost_prod — not Golden Source husbandry (Market/Flex/Research asof)"
        bodyPadding="none"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {onOpenFullPostgres != null ? (
              <button
                type="button"
                className="text-[var(--text-dense-caption)] text-primary hover:underline"
                onClick={onOpenFullPostgres}
              >
                Open full Postgres on Cluster →
              </button>
            ) : null}
            <SectionRefreshButton
              isFetching={freshnessQuery.isFetching}
              onClick={() => {
                void qc.invalidateQueries({ queryKey: ['cluster', 'data-freshness'] })
                void qc.invalidateQueries({ queryKey: ['cluster', 'data-clone-schedule'] })
              }}
            />
            {canAdmin ? (
              <RequestDataClone
                mode={syncMode}
                tables={selectedTables}
                disabled={!selectiveReady}
              />
            ) : null}
          </div>
        }
      >
        {freshnessQuery.isLoading && freshnessQuery.data == null ? (
          <p className="m-0 px-3 py-3 text-dense-meta text-[var(--muted-foreground)]">Loading freshness…</p>
        ) : freshnessQuery.isError ? (
          <p className="m-0 px-3 py-3 text-dense-meta text-danger">
            {(freshnessQuery.error as Error).message}
          </p>
        ) : (
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Database</DenseTableHead>
                <DenseTableHead>Env</DenseTableHead>
                <DenseTableHead>Last activity</DenseTableHead>
                <DenseTableHead>Lag vs prod</DenseTableHead>
                <DenseTableHead>Verdict</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {databases.map(db => (
                <DenseTableRow key={db.name}>
                  <DenseTableCell className="font-mono-tabular">{db.name}</DenseTableCell>
                  <DenseTableCell>{db.environment}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">
                    {db.last_activity_ts ?? '—'}
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={freshnessBadgeVariant(db)}>{freshnessBadgeLabel(db)}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell>
                    <span className="text-dense-meta">
                      {db.verdict}
                      {db.detail != null && db.detail !== '' ? (
                        <span className="text-[var(--muted-foreground)]"> · {db.detail}</span>
                      ) : null}
                    </span>
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        )}

        <div className="flex flex-col gap-2 border-t border-[var(--table-rule)] px-3 py-2">
          {canAdmin ? (
            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap items-center gap-2 text-dense-meta">
                <span className="shrink-0 text-[var(--muted-foreground)]">Sync mode:</span>
                <SegmentControl
                  size="sm"
                  value={syncMode}
                  onChange={v => setSyncMode(v as CloneSyncMode)}
                  options={[
                    { value: 'full', label: 'Full' },
                    { value: 'selective', label: 'Selective' },
                  ]}
                />
              </div>
              {syncMode === 'selective' ? (
                <div className="flex flex-col gap-1.5">
                  <span className="text-dense-meta text-[var(--muted-foreground)]">
                    Groups (published by the bifrost_prod app&apos;s data probe):
                  </span>
                  {cloneGroups.length > 0 ? (
                    <div className="flex flex-wrap gap-1.5" role="group" aria-label="Selective sync groups">
                      {cloneGroups.map(group => {
                        const selected = isGroupSelected(group)
                        const tables = group.tables.join(', ')
                        return (
                          <DenseTagButton
                            key={group.name}
                            size="pill"
                            variant={selected ? 'info' : 'neutral'}
                            aria-pressed={selected}
                            title={group.note != null && group.note !== '' ? `${group.note} — ${tables}` : tables}
                            className={cn(
                              selected ? 'ring-1 ring-[var(--color-entity-category)]' : 'opacity-80',
                            )}
                            onClick={() => toggleGroup(group)}
                          >
                            {group.name} · {group.tables.length}
                          </DenseTagButton>
                        )
                      })}
                    </div>
                  ) : (
                    <p className="m-0 text-dense-caption text-[var(--muted-foreground)]">
                      Clone groups unknown
                      {cloneGroupsDetail != null && cloneGroupsDetail !== '' ? ` — ${cloneGroupsDetail}` : ''}. Full sync
                      still works.
                    </p>
                  )}
                  {tableChips.length > 0 ? (
                    <span className="text-dense-meta text-[var(--muted-foreground)]">
                      Tables (TRUNCATE + data-only restore from prod):
                    </span>
                  ) : null}
                  <div className="flex flex-wrap gap-1.5" role="group" aria-label="Selective sync tables">
                    {tableChips.map(table => {
                      const selected = selectedTables.includes(table)
                      return (
                        <DenseTagButton
                          key={table}
                          size="pill"
                          variant={selected ? 'info' : 'neutral'}
                          aria-pressed={selected}
                          className={cn(
                            'font-mono-tabular',
                            selected
                              ? 'ring-1 ring-[var(--color-entity-category)]'
                              : 'opacity-80',
                          )}
                          onClick={() => toggleTable(table)}
                        >
                          {table}
                        </DenseTagButton>
                      )
                    })}
                  </div>
                  {selectedTables.length === 0 ? (
                    <p className="m-0 text-dense-caption text-danger">Select a group or at least one table.</p>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}

          <div className="flex flex-wrap items-center gap-2 text-dense-meta">
            <span className="text-[var(--muted-foreground)]">Last clone</span>
            {freshnessQuery.data?.last_clone_at != null ? (
              <span className="font-mono-tabular text-dense-caption">{freshnessQuery.data.last_clone_at}</span>
            ) : (
              <span className="text-dense-caption text-[var(--muted-foreground)]">never</span>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2 text-dense-meta">
            <span className="text-[var(--muted-foreground)]">Auto clone</span>
            {schedule != null ? (
              <>
                <DenseTag variant={schedule.enabled ? 'warning' : 'neutral'}>
                  {schedule.enabled ? schedule.interval : 'disabled'}
                </DenseTag>
                {schedule.last_auto_run_at != null ? (
                  <span className="font-mono-tabular text-dense-caption">
                    last {schedule.last_auto_run_at}
                    {schedule.last_status != null ? ` · ${schedule.last_status}` : ''}
                  </span>
                ) : (
                  <span className="text-dense-caption text-[var(--muted-foreground)]">no auto runs yet</span>
                )}
                {canAdmin ? <RequestDataCloneSchedule enabled={!schedule.enabled} /> : null}
              </>
            ) : (
              <span className="text-[var(--muted-foreground)]">—</span>
            )}
          </div>

          <p className="m-0 text-dense-caption text-[var(--muted-foreground)]">
            Last activity is what each environment&apos;s app reports at /api/monitor/ops/data-probe (unknown when it does
            not answer). Verdict+badge use lag vs prod (fresh &lt;3d · aging 3–7d · stale ≥7d). bifrost_prod is reference. Full =
            DROP SCHEMA; Selective = TRUNCATE listed tables (no CASCADE) then data-only restore, refused unless every table
            referencing them is listed too. Requires admin token + confirm:true +
            confirmation_token. Auto-clone stays disabled unless Owner enables weekly.
          </p>
        </div>
      </OpsSection>
    </>
  )
}
