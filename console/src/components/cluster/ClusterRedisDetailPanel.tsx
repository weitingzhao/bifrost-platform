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
  ClusterRedisStatusResponse,
  ClusterServiceReadinessResponse,
  Reachability,
  ServiceDomain,
} from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { StatusLamp } from '@/components/StatusLamp'
import { CopyChip } from '@/components/cluster/CopyChip'
import { REDIS_INSTANCES } from '@/lib/architecture/dataLayerCatalog'

interface ClusterRedisDetailPanelProps {
  redis: ClusterRedisStatusResponse | undefined
  redisLoading: boolean
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

function redisDomain(readiness: ClusterServiceReadinessResponse | undefined): ServiceDomain | undefined {
  return readiness?.domains.find(d => d.id === 'redis')
}

function roleVariant(role: string): 'info' | 'neutral' | 'category' {
  if (role === 'live') return 'info'
  if (role === 'queue') return 'neutral'
  return 'category'
}

function RedisLanAccessSection({ endpoints }: { endpoints: ClusterRedisStatusResponse['lan_endpoints'] }) {
  const list = endpoints ?? []
  const anyAvailable = list.some(ep => ep.available)

  return (
    <OpsSection
      title="LAN access (Redis Insight / redis-cli)"
      description="NodePort entry points for external clients on the local network — no port-forward needed"
      bodyPadding="compact"
    >
      {list.length === 0 ? (
        <p className="m-0 text-dense-meta text-[var(--muted-foreground)]">
          LAN endpoints not reported — restart platform-api after upgrade, or apply k8s/data/redis/redis-nodeport.yaml.
        </p>
      ) : (
        <div className="flex flex-col gap-2">
          {list.map(ep => (
            <div
              key={ep.name}
              className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--background)]/40 px-2 py-1.5"
            >
              <StatusLamp value={ep.reachability} kind="reach" />
              <span className="font-mono-tabular text-dense-meta min-w-[9rem]">{ep.name}</span>
              <DenseTag variant="category">{ep.environment}</DenseTag>
              <DenseTag variant={roleVariant(ep.role)}>{ep.role}</DenseTag>
              {ep.available && ep.host != null && ep.node_port != null ? (
                <>
                  <CopyChip label="Host" value={ep.host} />
                  <CopyChip label="Port" value={String(ep.node_port)} />
                  {ep.endpoint != null ? <CopyChip label="URL" value={`redis://${ep.endpoint}`} /> : null}
                </>
              ) : (
                <span className="text-dense-meta text-[var(--muted-foreground)]">{ep.detail ?? 'unavailable'}</span>
              )}
              {ep.database != null && ep.database !== '' ? (
                <span className="text-dense-caption text-[var(--muted-foreground)]">{ep.database}</span>
              ) : null}
            </div>
          ))}
          <p className="m-0 text-dense-caption text-[var(--muted-foreground)]">
            {anyAvailable
              ? 'No requirepass on phase-⑥ instances — leave Redis Insight password empty. LAN only; do not expose NodePorts to the public internet. Any Ready k3s node IP works if the listed host is unreachable.'
              : 'NodePorts not resolved yet — once available, connect Redis Insight with the listed host/port (no password).'}
          </p>
        </div>
      )}
    </OpsSection>
  )
}

export function ClusterRedisDetailPanel({
  redis,
  redisLoading,
  serviceReadiness,
}: ClusterRedisDetailPanelProps) {
  const qc = useQueryClient()
  const domain = redisDomain(serviceReadiness)

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['cluster', 'redis'] })
    void qc.invalidateQueries({ queryKey: ['cluster', 'service-readiness'] })
  }

  if (redisLoading && redis == null) {
    return <p className="m-0 px-3 py-4 text-dense-meta text-[var(--muted-foreground)]">Loading Redis status…</p>
  }

  if (redis == null) {
    return (
      <p className="m-0 px-3 py-4 text-dense-meta text-[var(--muted-foreground)]">
        Cluster unreachable — cannot load Redis status.
      </p>
    )
  }

  const streamLabel = `Spine ${redis.migration_step}/${redis.migration_total}`
  const redisPhaseLabel = `Redis target · phase ${redis.migration_redis_step}`

  return (
    <div className="cluster-redis-detail flex flex-col gap-3 p-3">
      <section className="rounded-md border border-[var(--border)] bg-[var(--background)]/60 px-3 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <StatusLamp value={redis.reachability} kind="reach" />
            <span className="text-dense-label font-semibold">{redis.summary}</span>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant={reachVariant(redis.reachability)}>
              {redis.targets_ready}/{redis.targets_total} data NS targets
            </DenseTag>
            {redis.embedded_active > 0 ? (
              <DenseTag variant="warning">{redis.embedded_active} embedded active</DenseTag>
            ) : null}
            <DenseTag variant="category">{streamLabel}</DenseTag>
            <DenseTag variant="neutral">{redisPhaseLabel}</DenseTag>
            <SectionRefreshButton isFetching={redisLoading} onClick={refresh} />
          </div>
        </div>
        {domain != null && domain.summary !== redis.summary ? (
          <p className="m-0 mt-1 text-dense-meta text-[var(--muted-foreground)]">{domain.summary}</p>
        ) : null}
      </section>

      <div className="grid gap-3 lg:grid-cols-2">
        <OpsSection
          title="Environment isolation (R-DV1)"
          description="Separate live / queue endpoints per env @ data namespace"
          bodyPadding="none"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Env</DenseTableHead>
                <DenseTableHead>Live</DenseTableHead>
                <DenseTableHead>Queue</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {redis.env_endpoints.map(row => (
                <DenseTableRow key={row.environment}>
                  <DenseTableCell>
                    <DenseTag variant="category">{row.environment}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{row.live_service}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{row.queue_service}</DenseTableCell>
                  <DenseTableCell>
                    <span className="inline-flex items-center gap-2 text-dense-meta">
                      <StatusLamp value={row.live_reachability} kind="reach" />
                      <StatusLamp value={row.queue_reachability} kind="reach" />
                      <span className="text-[var(--muted-foreground)]">{row.detail ?? '—'}</span>
                    </span>
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>

        <OpsSection title="Instance roles (design)" description="Bitnami live vs queue split · phase ⑥ deploy" bodyPadding="none">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Instance</DenseTableHead>
                <DenseTableHead>Policy</DenseTableHead>
                <DenseTableHead>HA</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {REDIS_INSTANCES.map(inst => (
                <DenseTableRow key={inst.name}>
                  <DenseTableCell>
                    <span className="font-mono-tabular text-dense-meta">{inst.name}</span>
                    <p className="m-0 mt-0.5 text-dense-caption text-[var(--muted-foreground)]">{inst.roles}</p>
                  </DenseTableCell>
                  <DenseTableCell className="text-dense-meta">{inst.maxmemoryPolicy}</DenseTableCell>
                  <DenseTableCell className="text-dense-meta">{inst.ha}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      </div>

      <RedisLanAccessSection endpoints={redis.lan_endpoints} />

      <OpsSection title="Data namespace targets" description="Expected Services @ data · k8s/data/redis/" bodyPadding="none">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Name</DenseTableHead>
              <DenseTableHead>Env</DenseTableHead>
              <DenseTableHead>Role</DenseTableHead>
              <DenseTableHead>Endpoint</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {redis.target_instances.map(t => (
              <DenseTableRow key={t.name}>
                <DenseTableCell className="font-mono-tabular text-dense-meta">{t.name}</DenseTableCell>
                <DenseTableCell>{t.environment}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={t.role === 'live' ? 'info' : t.role === 'queue' ? 'neutral' : 'category'}>
                    {t.role}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-dense-meta">{t.service}</DenseTableCell>
                <DenseTableCell>
                  <span className="inline-flex items-center gap-1 text-dense-meta">
                    <StatusLamp value={t.reachability} kind="reach" />
                    {t.detail ?? '—'}
                  </span>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <div className="grid gap-3 lg:grid-cols-2">
        <OpsSection title="Backup path" description="RDB snapshots → MinIO @ nfs-hot (live priority)" bodyPadding="compact">
          <div className="space-y-2">
            <DepRow label="MinIO" dep={redis.minio} />
            <DepRow label="RDB backup CronJob" dep={redis.backup} />
          </div>
        </OpsSection>

        <OpsSection
          title="Embedded redis (interim)"
          description="Single redis:7-alpine per namespace · live+queue db=0 · retire phase ⑥"
          bodyPadding="none"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Namespace</DenseTableHead>
                <DenseTableHead>Service</DenseTableHead>
                <DenseTableHead>Image</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {redis.embedded.map(item => (
                <DenseTableRow key={item.namespace}>
                  <DenseTableCell className="font-mono-tabular">{item.namespace}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{item.host}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{item.image ?? '—'}</DenseTableCell>
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
      </div>

      <OpsSection title="Legacy & external" description="Bare-metal / compose paths outside data NS" bodyPadding="none">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Source</DenseTableHead>
              <DenseTableHead>Host</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {redis.legacy.map(item => (
              <DenseTableRow key={`legacy-${item.host ?? item.kind}`}>
                <DenseTableCell>{item.kind}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{item.host ?? '—'}</DenseTableCell>
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
        <OpsSection title="Infrastructure dependencies" description="Storage, targets, embedded interim" bodyPadding="compact">
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
