import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  PageHeader,
  SegmentControl,
} from '@bifrost/ui'
import {
  fetchSatelliteBusDeep,
  fetchClusterMetrics,
  fetchClusterObservability,
  fetchClusterServiceReadiness,
  fetchClusterWorkloads,
  fetchMatrix,
  isAllMatrices,
  isAllSatelliteBusDeep,
} from '@/api/platform'
import type { MatrixResponse, Reachability, SatelliteBusDeepResponse } from '@/api/types'
import { ClusterServiceReadinessPanel } from '@/components/cluster/ClusterServiceReadinessPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { SatelliteObservabilityStrip } from '@/components/satellite/SatelliteObservabilityStrip'
import { StatusLamp } from '@/components/StatusLamp'
import {
  buildPayloadReadinessRows,
  type PayloadReadinessRow,
} from '@/lib/control-room/payloadReadiness'
import { worst, type Signal } from '@/lib/control-room/missionSignals'

const TRADE_ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

type TradeEnv = (typeof TRADE_ENV_OPTIONS)[number]['value']

const TRADE_NS: Record<TradeEnv, string> = {
  dev: 'bifrost-dev',
  stg: 'bifrost-stg',
  prod: 'bifrost-prod',
}

const SATELLITE_DOMAIN_IDS = ['workers', 'applications', 'database', 'redis'] as const

const CRITICAL_PROCESS_PATTERNS = [
  { pattern: /daemon/i, label: 'GsTrading daemon' },
  { pattern: /ingestor|ib-ingest/i, label: 'IB Ingestor' },
  { pattern: /operator|ib-operator/i, label: 'IB Operator' },
  { pattern: /account/i, label: 'IB Account Agent' },
  { pattern: /massive/i, label: 'Massive WS' },
  { pattern: /celery|worker/i, label: 'Celery worker' },
  { pattern: /flower/i, label: 'Flower' },
]

function tradeApiTargets(matrix: MatrixResponse | undefined): { ok: number; total: number } {
  if (matrix == null) return { ok: 0, total: 0 }
  const tradeTargets = matrix.targets.filter(
    t =>
      t.category === 'trade_api' ||
      t.category === 'trade_frontend' ||
      t.id === 'nginx-spa' ||
      t.id.startsWith('api-'),
  )
  const ok = tradeTargets.filter(t => t.reachability === 'ok').length
  return { ok, total: tradeTargets.length }
}

function signalFromReach(r: Reachability | undefined): Signal {
  if (r == null) return 'unknown'
  return r as Signal
}

function signalFromLamp(lamp: string | undefined): Signal {
  switch ((lamp ?? '').toLowerCase()) {
    case 'green':
      return 'ok'
    case 'yellow':
      return 'degraded'
    case 'red':
      return 'fail'
    default:
      return 'unknown'
  }
}

function renderText(value: unknown): string {
  if (value == null) return '—'
  if (typeof value === 'string' && value.trim() === '') return '—'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function PayloadReadinessTable({ rows }: { rows: PayloadReadinessRow[] }) {
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Subsystem</DenseTableHead>
          <DenseTableHead>Role</DenseTableHead>
          <DenseTableHead>Dev</DenseTableHead>
          <DenseTableHead>Prod</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {rows.map(row => (
          <DenseTableRow key={row.id}>
            <DenseTableCell className="font-medium">{row.label}</DenseTableCell>
            <DenseTableCell className="text-[var(--muted-foreground)]">{row.role}</DenseTableCell>
            <DenseTableCell>
              <StatusLamp value={row.dev.signal} kind="reach" />{' '}
              <span className="text-[var(--text-dense-meta)]">{row.dev.detail}</span>
            </DenseTableCell>
            <DenseTableCell>
              <StatusLamp value={row.prod.signal} kind="reach" />{' '}
              <span className="text-[var(--text-dense-meta)]">{row.prod.detail}</span>
              {row.envDiverges && <DenseTag variant="warning" className="ml-2">Diverged</DenseTag>}
            </DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}

export function SatelliteBusPage({
  onOpenCluster,
  onOpenTelemetry,
  onOpenPluginGallery,
  onOpenApiHealth,
}: {
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  onOpenPluginGallery?: () => void
  onOpenApiHealth?: () => void
}) {
  const [tradeEnv, setTradeEnv] = useState<TradeEnv>('stg')
  const ns = TRADE_NS[tradeEnv]

  const matrixQuery = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    refetchInterval: 30_000,
  })

  const serviceReadinessQuery = useQuery({
    queryKey: ['cluster', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: 30_000,
  })

  const workloadsQuery = useQuery({
    queryKey: ['cluster', 'workloads', ns, 'satellite-bus'],
    queryFn: () => fetchClusterWorkloads(ns),
    refetchInterval: 30_000,
  })

  const metricsQuery = useQuery({
    queryKey: ['cluster', 'metrics'],
    queryFn: () => fetchClusterMetrics(8),
    refetchInterval: 30_000,
  })

  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
    retry: false,
  })

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQuery.data
    if (data == null) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixQuery.data])

  const payloadRows = useMemo(() => buildPayloadReadinessRows(matrices), [matrices])
  const envMatrix = matrices.find(m => m.environment === tradeEnv)
  const tradeApi = tradeApiTargets(envMatrix)

  const busDeepQuery = useQuery({
    queryKey: ['satellite', 'bus-deep', tradeEnv],
    queryFn: () => fetchSatelliteBusDeep(tradeEnv),
    refetchInterval: 30_000,
  })

  const busDeep = useMemo((): SatelliteBusDeepResponse | undefined => {
    const data = busDeepQuery.data
    if (data == null) return undefined
    if (isAllSatelliteBusDeep(data)) return data.buses.find(b => b.environment === tradeEnv)
    return data
  }, [busDeepQuery.data, tradeEnv])

  const busSignal = useMemo((): Signal => {
    const workloadSignals = (workloadsQuery.data?.workloads ?? []).map(w => w.reachability as Signal)
    const domainSignals = (serviceReadinessQuery.data?.domains ?? [])
      .filter(d => SATELLITE_DOMAIN_IDS.includes(d.id as (typeof SATELLITE_DOMAIN_IDS)[number]))
      .map(d => signalFromReach(d.reachability))
    const deepSignal = signalFromReach(busDeep?.reachability)
    return worst(
      deepSignal,
      tradeApi.total === 0 ? 'unknown' : tradeApi.ok === tradeApi.total ? 'ok' : tradeApi.ok > 0 ? 'degraded' : 'fail',
      ...workloadSignals,
      ...domainSignals,
    )
  }, [busDeep?.reachability, serviceReadinessQuery.data?.domains, tradeApi.ok, tradeApi.total, workloadsQuery.data?.workloads])

  const criticalProcesses = useMemo(() => {
    const workloads = workloadsQuery.data?.workloads ?? []
    return CRITICAL_PROCESS_PATTERNS.map(({ pattern, label }) => {
      const match = workloads.find(w => pattern.test(w.name))
      return {
        label,
        name: match?.name ?? '—',
        namespace: match?.namespace ?? ns,
        reachability: match?.reachability ?? ('unknown' as Reachability),
        ready: match?.ready ?? '—',
        status: match?.status ?? 'not deployed',
      }
    })
  }, [ns, workloadsQuery.data?.workloads])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="Bus Status"
        description="Trade satellite backbone with deep daemon/socket/celery semantics from monitor and ops APIs."
      />

      <section className="page-section panel-elevated px-3 py-2">
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-xs font-medium text-muted-foreground shrink-0">Trade NS:</span>
          <SegmentControl
            value={tradeEnv}
            options={[...TRADE_ENV_OPTIONS]}
            onChange={v => setTradeEnv(v as TradeEnv)}
          />
          <StatusLamp value={busSignal} kind="reach" />
          <span className="text-[var(--text-dense-meta)]">
            {tradeApi.ok}/{tradeApi.total} API targets · namespace {ns}
          </span>
          {busDeep != null && (
            <span className="text-[var(--text-dense-meta)]">
              Monitor lamp {String(busDeep.monitor.health.status_lamp ?? 'unknown').toUpperCase()}
            </span>
          )}
          {onOpenApiHealth != null && (
            <button type="button" className="focus-strip-link text-[var(--text-dense-meta)]" onClick={onOpenApiHealth}>
              Satellite → API Health
            </button>
          )}
          {onOpenPluginGallery != null && (
            <button type="button" className="focus-strip-link text-[var(--text-dense-meta)]" onClick={onOpenPluginGallery}>
              Subcontractors → Plugin Gallery
            </button>
          )}
        </div>
      </section>

      <section className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {(
          [
            { label: 'Bus signal', value: busSignal.toUpperCase(), lamp: busSignal },
            {
              label: 'Monitor lamp',
              value: String(busDeep?.monitor.health.status_lamp ?? 'unknown').toUpperCase(),
              lamp: signalFromLamp(busDeep?.monitor.health.status_lamp),
            },
            {
              label: 'Daemon self-check',
              value: String(busDeep?.monitor.daemon.self_check ?? 'unknown'),
              lamp: signalFromReach(busDeep?.monitor.daemon.reachability),
            },
            {
              label: 'Probe time',
              value:
                busDeep?.generated_at != null
                  ? new Date(busDeep.generated_at).toLocaleTimeString()
                  : envMatrix?.generated_at != null
                    ? new Date(envMatrix.generated_at).toLocaleTimeString()
                    : '—',
              lamp: signalFromReach(busDeep?.reachability),
            },
          ] as const
        ).map(kpi => (
          <section key={kpi.label} className="page-section panel-elevated px-3 py-2">
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">{kpi.label}</p>
            <p className="m-0 mt-1 flex items-center gap-2 text-sm font-semibold">
              {kpi.lamp !== 'unknown' && <StatusLamp value={kpi.lamp} kind="reach" />}
              {kpi.value}
            </p>
          </section>
        ))}
      </section>

      <OpsSection title="Daemon FSM" description="Daemon heartbeat, self-check, and block reasons from monitor status body." bodyPadding="none" overflow="hidden">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Field</DenseTableHead>
              <DenseTableHead>Value</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {busDeepQuery.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={2} className="text-[var(--muted-foreground)]">
                  Loading deep bus…
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              <>
                <DenseTableRow>
                  <DenseTableCell className="font-medium">Reachability</DenseTableCell>
                  <DenseTableCell>
                    <StatusLamp value={busDeep?.monitor.daemon.reachability ?? 'unknown'} kind="reach" />{' '}
                    {renderText(busDeep?.monitor.daemon.reachability)}
                  </DenseTableCell>
                </DenseTableRow>
                <DenseTableRow>
                  <DenseTableCell className="font-medium">Self check</DenseTableCell>
                  <DenseTableCell>{renderText(busDeep?.monitor.daemon.self_check)}</DenseTableCell>
                </DenseTableRow>
                <DenseTableRow>
                  <DenseTableCell className="font-medium">Lamp</DenseTableCell>
                  <DenseTableCell>{renderText(busDeep?.monitor.daemon.lamp)}</DenseTableCell>
                </DenseTableRow>
                <DenseTableRow>
                  <DenseTableCell className="font-medium">Block reasons</DenseTableCell>
                  <DenseTableCell>
                    {(busDeep?.monitor.daemon.block_reasons ?? []).length === 0
                      ? '—'
                      : (busDeep?.monitor.daemon.block_reasons ?? []).map(reason => (
                        <DenseTag key={reason} variant="warning" className="mr-1">
                          {reason}
                        </DenseTag>
                      ))}
                  </DenseTableCell>
                </DenseTableRow>
                <DenseTableRow>
                  <DenseTableCell className="font-medium">Trading suspended</DenseTableCell>
                  <DenseTableCell>{renderText(busDeep?.monitor.daemon.trading?.trading_suspended)}</DenseTableCell>
                </DenseTableRow>
                <DenseTableRow>
                  <DenseTableCell className="font-medium">Heartbeat daemon_alive</DenseTableCell>
                  <DenseTableCell>{renderText(busDeep?.monitor.daemon.heartbeat?.daemon_alive)}</DenseTableCell>
                </DenseTableRow>
                <DenseTableRow>
                  <DenseTableCell className="font-medium">Heartbeat ib_connected</DenseTableCell>
                  <DenseTableCell>{renderText(busDeep?.monitor.daemon.heartbeat?.ib_connected)}</DenseTableCell>
                </DenseTableRow>
                <DenseTableRow>
                  <DenseTableCell className="font-medium">Heartbeat seconds_until_retry</DenseTableCell>
                  <DenseTableCell>{renderText(busDeep?.monitor.daemon.heartbeat?.seconds_until_retry)}</DenseTableCell>
                </DenseTableRow>
              </>
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <OpsSection title="Socket health" description="Massive and IB socket components reported by monitor status schema v9." bodyPadding="none" overflow="hidden">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Socket</DenseTableHead>
              <DenseTableHead>Reach</DenseTableHead>
              <DenseTableHead>Self check</DenseTableHead>
              <DenseTableHead>Detail</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {([
              ['massive', busDeep?.monitor.socket.massive],
              ['ib_ingestor', busDeep?.monitor.socket.ib_ingestor],
              ['ib_account_agent', busDeep?.monitor.socket.ib_account_agent],
              ['ib_operator', busDeep?.monitor.socket.ib_operator],
              ['platform_ib_gateway', busDeep?.monitor.socket.platform_ib_gateway],
            ] as const).map(([name, row]) => (
              <DenseTableRow key={name}>
                <DenseTableCell className="font-medium">{name}</DenseTableCell>
                <DenseTableCell>
                  <StatusLamp value={row?.reachability ?? 'unknown'} kind="reach" /> {renderText(row?.reachability)}
                </DenseTableCell>
                <DenseTableCell>{renderText(row?.self_check)}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{renderText(row?.detail)}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <OpsSection title="Celery" description="Celery broker and worker semantics from monitor payload." bodyPadding="none" overflow="hidden">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Field</DenseTableHead>
              <DenseTableHead>Value</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            <DenseTableRow>
              <DenseTableCell className="font-medium">Reachability</DenseTableCell>
              <DenseTableCell>
                <StatusLamp value={busDeep?.monitor.celery.reachability ?? 'unknown'} kind="reach" />{' '}
                {renderText(busDeep?.monitor.celery.reachability)}
              </DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">broker_connected</DenseTableCell>
              <DenseTableCell>{renderText(busDeep?.monitor.celery.broker_connected)}</DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">workers</DenseTableCell>
              <DenseTableCell>{(busDeep?.monitor.celery.workers ?? []).join(', ') || '—'}</DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">worker_ib_connected</DenseTableCell>
              <DenseTableCell>{renderText(busDeep?.monitor.celery.worker_ib_connected)}</DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">worker_ib_client_id</DenseTableCell>
              <DenseTableCell>{renderText(busDeep?.monitor.celery.worker_ib_client_id)}</DenseTableCell>
            </DenseTableRow>
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <OpsSection title="Account Sync" description="Account sync daemon liveness and stream lag from monitor payload." bodyPadding="none" overflow="hidden">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Field</DenseTableHead>
              <DenseTableHead>Value</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            <DenseTableRow>
              <DenseTableCell className="font-medium">Reachability</DenseTableCell>
              <DenseTableCell>
                <StatusLamp value={busDeep?.monitor.account_sync.reachability ?? 'unknown'} kind="reach" />{' '}
                {renderText(busDeep?.monitor.account_sync.reachability)}
              </DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">daemon_alive</DenseTableCell>
              <DenseTableCell>{renderText(busDeep?.monitor.account_sync.daemon_alive)}</DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">stream_lag</DenseTableCell>
              <DenseTableCell>{renderText(busDeep?.monitor.account_sync.stream_lag)}</DenseTableCell>
            </DenseTableRow>
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <OpsSection title="Ops executor" description="Ops API execution plane health." bodyPadding="none" overflow="hidden">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Field</DenseTableHead>
              <DenseTableHead>Value</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            <DenseTableRow>
              <DenseTableCell className="font-medium">Reachability</DenseTableCell>
              <DenseTableCell>
                <StatusLamp value={busDeep?.ops.reachability ?? 'unknown'} kind="reach" /> {renderText(busDeep?.ops.reachability)}
              </DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">executor_mode</DenseTableCell>
              <DenseTableCell>{renderText(busDeep?.ops.executor_mode)}</DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">k8s_reachable</DenseTableCell>
              <DenseTableCell>{renderText(busDeep?.ops.k8s_reachable)}</DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableCell className="font-medium">status</DenseTableCell>
              <DenseTableCell>{renderText(busDeep?.ops.status)}</DenseTableCell>
            </DenseTableRow>
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <SatelliteObservabilityStrip
        metrics={metricsQuery.data}
        observability={observabilityQuery.data}
        metricsLoading={metricsQuery.isLoading}
        observabilityLoading={observabilityQuery.isLoading}
        onOpenCluster={onOpenCluster}
        onOpenTelemetry={onOpenTelemetry}
      />

      <OpsSection title="Payload readiness (matrix L0)" bodyPadding="none" overflow="hidden">
        <PayloadReadinessTable rows={payloadRows} />
      </OpsSection>

      <OpsSection title="Service domains" description="Trade stack domains from cluster service-readiness probe." bodyPadding="none" overflow="hidden">
        {SATELLITE_DOMAIN_IDS.map(domainId => (
          <div key={domainId} className="border-t border-[var(--border)] first:border-t-0">
            <ClusterServiceReadinessPanel
              data={serviceReadinessQuery.data}
              isLoading={serviceReadinessQuery.isLoading}
              compact
              domainFilter={domainId}
            />
          </div>
        ))}
      </OpsSection>

      <OpsSection
        title={`Critical processes · ${ns}`}
        description="Pod-level readiness in the selected trade namespace. IB socket plugins live under Subcontractors."
        bodyPadding="none"
        overflow="hidden"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Process</DenseTableHead>
              <DenseTableHead>Workload</DenseTableHead>
              <DenseTableHead>Ready</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {workloadsQuery.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              criticalProcesses.map(row => (
                <DenseTableRow key={row.label}>
                  <DenseTableCell className="font-medium">{row.label}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{row.name}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{row.ready}</DenseTableCell>
                  <DenseTableCell>
                    <StatusLamp value={row.reachability} kind="reach" />{' '}
                    <span className="font-mono-tabular">{row.status}</span>
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>
    </div>
  )
}
