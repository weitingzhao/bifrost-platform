import { useEffect, useState } from 'react'
import {
  Button,
  ConfirmDialog,
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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  fetchDataCloneSchedule,
  fetchDataCloneStatus,
  fetchDataFreshness,
  triggerDataClone,
  updateDataCloneSchedule,
} from '@/api/cluster'
import type { DataFreshnessDatabase } from '@/api/clusterTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'

const CLONE_CONFIRM_TOKEN = 'CLONE-FROM-PROD'

/** Selective sync presets — freshness probe tables + common business tables. */
const SELECTIVE_TABLE_PRESETS = [
  'daemon_control',
  'daemon_run_status',
  'daemon_heartbeat',
  'account_positions',
  'job_bars_backfill',
  'strategy_opportunity',
  'preference_position_categories',
] as const

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
}: {
  canAdmin: boolean
  onOpenFullPostgres?: () => void
}) {
  const qc = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [activeJobId, setActiveJobId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [syncMode, setSyncMode] = useState<CloneSyncMode>('full')
  const [selectedTables, setSelectedTables] = useState<string[]>([
    'daemon_control',
    'account_positions',
  ])

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
  const jobQuery = useQuery({
    queryKey: ['cluster', 'data-clone', activeJobId],
    queryFn: () => fetchDataCloneStatus(activeJobId!),
    enabled: activeJobId != null,
    refetchInterval: q => {
      const status = q.state.data?.status
      if (status === 'done' || status === 'failed') return false
      return 2000
    },
  })

  useEffect(() => {
    if (jobQuery.data?.status === 'done') {
      void qc.invalidateQueries({ queryKey: ['cluster', 'data-freshness'] })
    }
  }, [jobQuery.data?.status, qc])

  const toggleTable = (table: string) => {
    setSelectedTables(prev =>
      prev.includes(table) ? prev.filter(t => t !== table) : [...prev, table],
    )
  }
  const selectiveReady = syncMode === 'full' || selectedTables.length > 0

  const cloneMutation = useMutation({
    mutationFn: () =>
      triggerDataClone({
        source: 'bifrost_prod',
        targets: ['bifrost_dev', 'bifrost_stg'],
        mode: syncMode,
        tables: syncMode === 'selective' ? selectedTables : undefined,
        confirmation_token: CLONE_CONFIRM_TOKEN,
        confirm: true,
      }),
    onSuccess: job => {
      setActiveJobId(job.id)
      setActionError(null)
      setConfirmOpen(false)
    },
    onError: (err: Error) => {
      setActionError(err.message)
      setConfirmOpen(false)
    },
  })
  const scheduleMutation = useMutation({
    mutationFn: (enabled: boolean) =>
      updateDataCloneSchedule({
        enabled,
        interval: enabled ? 'weekly' : 'disabled',
        source: 'bifrost_prod',
        targets: ['bifrost_dev', 'bifrost_stg'],
        mode: 'full',
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['cluster', 'data-clone-schedule'] })
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const databases = freshnessQuery.data?.databases ?? []
  const schedule = scheduleQuery.data
  const job = jobQuery.data
  const jobRunning = job != null && job.status !== 'done' && job.status !== 'failed'
  const confirmTitle =
    syncMode === 'selective'
      ? 'Selective sync bifrost_prod → bifrost_dev / bifrost_stg'
      : 'Sync bifrost_prod → bifrost_dev / bifrost_stg'
  const confirmMessage =
    syncMode === 'selective'
      ? `This TRUNCATEs then restores these tables from bifrost_prod on bifrost_dev and bifrost_stg: ${selectedTables.join(', ') || '(none)'}. Prod is never written.`
      : 'This overwrites bifrost_dev and bifrost_stg with a full copy of bifrost_prod (DROP SCHEMA public CASCADE). Prod is never written. Continue only if you intend to refresh non-prod data.'

  return (
    <>
      <OpsSection
        title="Data Freshness"
        description="Activity lag vs bifrost_prod · Sync clones prod → dev/stg on CNPG"
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
              <Button
                size="sm"
                variant="outline"
                disabled={cloneMutation.isPending || jobRunning || !selectiveReady}
                onClick={() => setConfirmOpen(true)}
              >
                Sync from Prod
              </Button>
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

        <div className="flex flex-col gap-2 border-t border-[var(--border)] px-3 py-2">
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
                    Tables (TRUNCATE + data-only restore from prod):
                  </span>
                  <div className="flex flex-wrap gap-1.5" role="group" aria-label="Selective sync tables">
                    {SELECTIVE_TABLE_PRESETS.map(table => {
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
                    <p className="m-0 text-dense-caption text-danger">Select at least one table.</p>
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
                {canAdmin ? (
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={scheduleMutation.isPending}
                    onClick={() => scheduleMutation.mutate(!schedule.enabled)}
                  >
                    {schedule.enabled ? 'Disable weekly' : 'Enable weekly'}
                  </Button>
                ) : null}
              </>
            ) : (
              <span className="text-[var(--muted-foreground)]">—</span>
            )}
          </div>

          {job != null ? (
            <div className="text-dense-meta">
              <span className="font-medium">Clone job</span>{' '}
              <span className="font-mono-tabular">{job.id}</span>
              {' · '}
              <DenseTag variant={job.status === 'done' ? 'success' : job.status === 'failed' ? 'danger' : 'warning'}>
                {job.step || job.status}
              </DenseTag>
              {job.detail !== '' ? <span className="text-[var(--muted-foreground)]"> — {job.detail}</span> : null}
              {job.progress > 0 && job.status !== 'done' ? (
                <span className="ml-1 font-mono-tabular text-dense-caption">{Math.round(job.progress * 100)}%</span>
              ) : null}
            </div>
          ) : null}

          {actionError != null ? <p className="m-0 text-dense-meta text-danger">{actionError}</p> : null}
          <p className="m-0 text-dense-caption text-[var(--muted-foreground)]">
            Verdict+badge use lag vs prod (fresh &lt;3d · aging 3–7d · stale ≥7d). bifrost_prod is reference. Full =
            DROP SCHEMA; Selective = TRUNCATE listed tables then data-only restore. Requires admin token + confirm:true +
            confirmation_token. Auto-clone stays disabled unless Owner enables weekly.
          </p>
        </div>
      </OpsSection>

      <ConfirmDialog
        open={confirmOpen}
        title={confirmTitle}
        message={confirmMessage}
        confirmLabel="Confirm sync from prod"
        confirming={cloneMutation.isPending}
        onConfirm={() => cloneMutation.mutate()}
        onCancel={() => setConfirmOpen(false)}
      />
    </>
  )
}
