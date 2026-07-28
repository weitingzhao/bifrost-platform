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
import { CopyChip } from '@/components/cluster/CopyChip'
import {
  fetchDataCloneSchedule,
  fetchDataCloneStatus,
  fetchDataFreshness,
  triggerDataClone,
  updateDataCloneSchedule,
} from '@/api/cluster'
import type {
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  DataFreshnessDatabase,
  ServiceDomain,
} from '@/api/clusterTypes'
import type { Reachability } from '@/api/matrixTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'
import { DataFreshnessPanel } from '@/components/cluster/DataFreshnessPanel'

interface ClusterPostgresDetailPanelProps {
  postgres: ClusterPostgresStatusResponse | undefined
  postgresLoading: boolean
  serviceReadiness: ClusterServiceReadinessResponse | undefined
  canAdmin?: boolean
}

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

function reachVariant(reach: Reachability): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (reach) {
    case 'ok':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'fail':
      return 'danger'
    default:
      return 'neutral'
  }
}

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

function DepRow({ label, dep }: { label: string; dep: { reachability: Reachability; detail?: string } }) {
  return (
    <div className="flex items-start gap-2 text-dense-meta">
      <StatusLamp value={dep.reachability} kind="reach" />
      <span>
        <span className="font-medium">{label}</span>
        {dep.detail != null && dep.detail !== '' ? (
          <span className="text-[var(--muted-foreground)]"> — {dep.detail}</span>
        ) : null}
      </span>
    </div>
  )
}

function databaseDomain(readiness: ClusterServiceReadinessResponse | undefined): ServiceDomain | undefined {
  return readiness?.domains.find(d => d.id === 'database')
}

function PostgresLanAccessSection({ lan }: { lan: ClusterPostgresStatusResponse['lan_access'] }) {
  const dbName = 'bifrost_dev'
  const jdbc =
    lan.available && lan.endpoint != null
      ? `jdbc:postgresql://${lan.endpoint}/${dbName}`
      : ''

  return (
    <OpsSection
      title="LAN access (DBeaver / SQL clients)"
      description="NodePort entry point for external clients on the local network — no port-forward needed"
      bodyPadding="compact"
    >
      {!lan.available ? (
        <div className="flex items-start gap-2 text-dense-meta">
          <StatusLamp value={lan.reachability} kind="reach" />
          <span>
            <span className="font-medium">NodePort not available</span>
            {lan.detail != null && lan.detail !== '' ? (
              <span className="text-[var(--muted-foreground)]"> — {lan.detail}</span>
            ) : null}
          </span>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <CopyChip label="Host" value={lan.host ?? ''} />
            <CopyChip label="Port" value={String(lan.node_port ?? '')} />
            <CopyChip label="User" value={lan.user ?? 'bifrost'} />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <CopyChip label="JDBC" value={jdbc} />
          </div>
          <dl className="m-0 grid gap-1 text-dense-meta">
            <div className="flex flex-wrap items-center gap-1">
              <dt className="text-[var(--muted-foreground)]">Databases</dt>
              <dd className="m-0 font-mono-tabular">bifrost_dev · bifrost_stg · bifrost_prod</dd>
            </div>
            <div className="flex flex-wrap items-center gap-1">
              <dt className="text-[var(--muted-foreground)]">Password</dt>
              <dd className="m-0 font-mono-tabular">
                kubectl get secret bifrost-postgres-app -n data -o jsonpath=&apos;{'{.data.password}'}&apos; | base64 -d
              </dd>
            </div>
          </dl>
          <p className="m-0 text-dense-caption text-[var(--muted-foreground)]">
            LAN only — do not expose port {lan.node_port} to the public internet. JDBC shows bifrost_dev; swap the database
            name for stg/prod.
          </p>
        </div>
      )}
    </OpsSection>
  )
}

/**
 * @deprecated Use DataFreshnessPanel. Kept as a direct-module compatibility export.
 */
export function DataFreshnessSection({ canAdmin }: { canAdmin: boolean }) {
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
    const status = jobQuery.data?.status
    if (status === 'done') {
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
              <DenseTag
                variant={
                  job.status === 'done' ? 'success' : job.status === 'failed' ? 'danger' : 'warning'
                }
              >
                {job.step || job.status}
              </DenseTag>
              {job.detail !== '' ? (
                <span className="text-[var(--muted-foreground)]"> — {job.detail}</span>
              ) : null}
              {job.progress > 0 && job.status !== 'done' ? (
                <span className="ml-1 font-mono-tabular text-dense-caption">
                  {Math.round(job.progress * 100)}%
                </span>
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

export function ClusterPostgresDetailPanel({
  postgres,
  postgresLoading,
  serviceReadiness,
  canAdmin = false,
}: ClusterPostgresDetailPanelProps) {
  const qc = useQueryClient()
  const domain = databaseDomain(serviceReadiness)

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['cluster', 'postgres'] })
    void qc.invalidateQueries({ queryKey: ['cluster', 'service-readiness'] })
    void qc.invalidateQueries({ queryKey: ['cluster', 'data-freshness'] })
  }

  if (postgresLoading && postgres == null) {
    return <p className="m-0 px-3 py-4 text-dense-meta text-[var(--muted-foreground)]">Loading PostgreSQL status…</p>
  }

  if (postgres == null) {
    return (
      <p className="m-0 px-3 py-4 text-dense-meta text-[var(--muted-foreground)]">
        Cluster unreachable — cannot load CNPG status.
      </p>
    )
  }

  const haOk = postgres.instances_spec >= 2 && postgres.instances_ready >= 2
  const migrationLabel = `Phase ${postgres.migration_step}/${postgres.migration_total} · ${postgres.migration_phase}`

  return (
    <div className="cluster-postgres-detail flex flex-col gap-3 p-3">
      <section className="rounded-md border border-[var(--border)] bg-[var(--background)]/60 px-3 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <StatusLamp value={postgres.reachability} kind="reach" />
            <span className="text-dense-label font-semibold">{postgres.summary}</span>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant={reachVariant(postgres.reachability)}>
              {haOk ? 'HA 2/2' : `${postgres.instances_ready}/${postgres.instances_spec || 2} instances`}
            </DenseTag>
            <DenseTag variant="category">{migrationLabel}</DenseTag>
            <SectionRefreshButton isFetching={postgresLoading} onClick={refresh} />
          </div>
        </div>
        {domain != null && domain.summary !== postgres.summary ? (
          <p className="m-0 mt-1 text-dense-meta text-[var(--muted-foreground)]">{domain.summary}</p>
        ) : null}
      </section>

      <DataFreshnessPanel canAdmin={canAdmin} />

      <div className="grid gap-3 lg:grid-cols-2">
        <OpsSection title="HA & replication" description="CloudNativePG instances in data namespace" bodyPadding="none">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Pod</DenseTableHead>
                <DenseTableHead>Role</DenseTableHead>
                <DenseTableHead>Node</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {postgres.instances.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                    No CNPG pods — run make k3s-install-data-layer-phase1
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                postgres.instances.map(inst => (
                  <DenseTableRow key={inst.pod_name}>
                    <DenseTableCell className="font-mono-tabular text-dense-meta">{inst.pod_name}</DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={inst.role === 'primary' ? 'info' : 'neutral'}>{inst.role || '—'}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{inst.node || '—'}</DenseTableCell>
                    <DenseTableCell>
                      <span className="inline-flex items-center gap-1">
                        <StatusLamp value={inst.reachability} kind="reach" />
                        {inst.phase}
                      </span>
                    </DenseTableCell>
                  </DenseTableRow>
                ))
              )}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>

        <OpsSection title="Connection & storage" description="In-cluster endpoints · PGDATA on local-path" bodyPadding="compact">
          <dl className="cluster-postgres-kv m-0 grid gap-2 text-dense-meta">
            <div>
              <dt className="text-[var(--muted-foreground)]">RW service</dt>
              <dd className="m-0 font-mono-tabular">{postgres.rw_service}</dd>
            </div>
            <div>
              <dt className="text-[var(--muted-foreground)]">RO service</dt>
              <dd className="m-0 font-mono-tabular">{postgres.ro_service}</dd>
            </div>
            <div>
              <dt className="text-[var(--muted-foreground)]">Primary</dt>
              <dd className="m-0 font-mono-tabular">
                {postgres.primary_pod != null && postgres.primary_pod !== ''
                  ? `${postgres.primary_pod} @ ${postgres.primary_node ?? '?'}`
                  : '—'}
              </dd>
            </div>
            <div>
              <dt className="text-[var(--muted-foreground)]">PGDATA volume</dt>
              <dd className="m-0">
                {postgres.storage_size || '—'} · {postgres.storage_class || 'local-path'}
              </dd>
            </div>
          </dl>
        </OpsSection>
      </div>

      <PostgresLanAccessSection lan={postgres.lan_access} />

      <div className="grid gap-3 lg:grid-cols-2">
        <OpsSection title="Backup path" description="WAL/base → MinIO on nfs-hot (not PGDATA)" bodyPadding="compact">
          <div className="space-y-2">
            <DepRow label="MinIO" dep={postgres.minio} />
            <DepRow label="Barman / ScheduledBackup" dep={postgres.backup} />
            <DepRow label="Operator" dep={postgres.operator} />
            <DepRow label="postgres-role nodes" dep={postgres.postgres_role} />
          </div>
        </OpsSection>

        <OpsSection title="R-DV1 databases" description="Logical isolation on shared CNPG cluster" bodyPadding="none">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Database</DenseTableHead>
                <DenseTableHead>Env</DenseTableHead>
                <DenseTableHead>CR</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {postgres.databases.map(db => (
                <DenseTableRow key={db.name}>
                  <DenseTableCell className="font-mono-tabular">{db.name}</DenseTableCell>
                  <DenseTableCell>{db.environment}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{db.cr_name ?? '—'}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      </div>

      <OpsSection
        title="Cutover tracker"
        description="Target: apps → bifrost-postgres-rw.data.svc · retire embedded + bare .80"
        bodyPadding="none"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Source</DenseTableHead>
              <DenseTableHead>Location</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {postgres.legacy.map(item => (
              <DenseTableRow key={`legacy-${item.host ?? item.kind}`}>
                <DenseTableCell>Bare metal</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{item.host ?? '—'}</DenseTableCell>
                <DenseTableCell>
                  <span className="inline-flex items-center gap-1 text-dense-meta">
                    <StatusLamp value={item.reachability} kind="reach" />
                    {item.detail ?? '—'}
                  </span>
                </DenseTableCell>
              </DenseTableRow>
            ))}
            {postgres.embedded.map(item => (
              <DenseTableRow key={`embedded-${item.namespace}`}>
                <DenseTableCell>Embedded Deployment</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{item.namespace}</DenseTableCell>
                <DenseTableCell>
                  <span className="inline-flex items-center gap-1 text-dense-meta">
                    <StatusLamp value={item.reachability} kind="reach" />
                    {item.detail ?? '—'}
                  </span>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      {domain != null && domain.dependencies.length > 0 ? (
        <OpsSection title="Infrastructure dependencies" description="Storage, NFS, operator prerequisites" bodyPadding="compact">
          <ul className="m-0 list-none space-y-1">
            {domain.dependencies.map(dep => (
              <li key={dep.id} className="flex items-start gap-1.5 text-dense-meta">
                <StatusLamp value={dep.reachability} kind="reach" />
                <span>
                  <span className="font-medium">{dep.label}</span>
                  {dep.detail != null && dep.detail !== '' ? (
                    <span className="text-[var(--muted-foreground)]"> — {dep.detail}</span>
                  ) : null}
                </span>
              </li>
            ))}
          </ul>
        </OpsSection>
      ) : null}
    </div>
  )
}
