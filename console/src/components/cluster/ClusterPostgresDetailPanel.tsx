import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import { useQueryClient } from '@tanstack/react-query'
import type {
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  Reachability,
  ServiceDomain,
} from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'

interface ClusterPostgresDetailPanelProps {
  postgres: ClusterPostgresStatusResponse | undefined
  postgresLoading: boolean
  serviceReadiness: ClusterServiceReadinessResponse | undefined
}

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

export function ClusterPostgresDetailPanel({
  postgres,
  postgresLoading,
  serviceReadiness,
}: ClusterPostgresDetailPanelProps) {
  const qc = useQueryClient()
  const domain = databaseDomain(serviceReadiness)

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['cluster', 'postgres'] })
    void qc.invalidateQueries({ queryKey: ['cluster', 'service-readiness'] })
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
