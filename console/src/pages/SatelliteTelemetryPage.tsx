/**
 * Satellite → Satellite Runtime
 * Scoped golden signals for the selected Trade namespace.
 * Global Layer B readiness / system verdict → Mission Control → Observability.
 */

import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  SegmentControl,
} from '@bifrost/ui'
import { fetchClusterObservability } from '@/api/cluster'
import { fetchTelemetryOverview } from '@/api/telemetry'
import type { TelemetryMetricResult } from '@/api/clusterTypes'
import { MonitoringCoverageStrip } from '@/components/observability/MonitoringCoverageStrip'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { PageToolbar } from '@/components/layout/PageToolbar'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { buildGrafanaDashboardUrl } from '@/lib/observability'

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

const API_METRIC_IDS = ['api_request_rate', 'api_latency_p99', 'api_error_rate'] as const
const DATA_METRIC_IDS = [
  'redis_memory_bytes',
  'redis_connected_clients',
  'pg_connections',
  'pg_replication_lag',
] as const

function metricByID(metrics: TelemetryMetricResult[] | undefined, id: string): TelemetryMetricResult | undefined {
  return metrics?.find(m => m.id === id)
}

function serviceLabel(labels: Record<string, string>): string {
  return labels.service ?? labels.pod ?? labels.instance ?? labels.job ?? 'unknown'
}

function formatMetricValue(value: number, unit?: string): string {
  if (unit === 'bytes') {
    if (value >= 1_073_741_824) return `${(value / 1_073_741_824).toFixed(2)} GiB`
    if (value >= 1_048_576) return `${(value / 1_048_576).toFixed(1)} MiB`
    return `${Math.round(value)} B`
  }
  if (unit === 'ratio') return `${(value * 100).toFixed(2)}%`
  if (unit === 's') return `${(value * 1000).toFixed(0)} ms`
  if (unit === 'req/s') return value.toFixed(3)
  if (Number.isInteger(value)) return String(value)
  return value.toFixed(3)
}

function buildApiPerformanceRows(metrics: TelemetryMetricResult[] | undefined) {
  const rate = metricByID(metrics, 'api_request_rate')
  const p99 = metricByID(metrics, 'api_latency_p99')
  const err = metricByID(metrics, 'api_error_rate')
  const services = new Set<string>()
  for (const metric of [rate, p99, err]) {
    for (const point of metric?.points ?? []) {
      services.add(serviceLabel(point.labels))
    }
  }
  return [...services].sort().map(service => {
    const find = (metric: TelemetryMetricResult | undefined) =>
      metric?.points.find(p => serviceLabel(p.labels) === service)
    return {
      service,
      rate: find(rate),
      p99: find(p99),
      errorRate: find(err),
    }
  })
}

interface SatelliteTelemetryPageProps {
  onOpenCluster?: () => void
  onOpenObservability?: () => void
}

export function SatelliteTelemetryPage({
  onOpenCluster,
  onOpenObservability,
}: SatelliteTelemetryPageProps) {
  const [tradeEnv, setTradeEnv] = useState<TradeEnv>('stg')
  const ns = TRADE_NS[tradeEnv]

  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 60_000,
  })

  const telemetryQuery = useQuery({
    queryKey: ['telemetry', 'overview', ns],
    queryFn: () => fetchTelemetryOverview(ns),
    refetchInterval: 30_000,
    retry: false,
  })

  const apiRows = useMemo(
    () => buildApiPerformanceRows(telemetryQuery.data?.metrics),
    [telemetryQuery.data?.metrics],
  )

  const dataMetrics = useMemo(() => {
    const metrics = telemetryQuery.data?.metrics ?? []
    return DATA_METRIC_IDS.map(id => metricByID(metrics, id)).filter(
      (m): m is TelemetryMetricResult => m != null,
    )
  }, [telemetryQuery.data?.metrics])

  const layerB = observabilityQuery.data?.layer_b_status
  const tradeDashUrl = buildGrafanaDashboardUrl({
    grafanaBaseUrl: observabilityQuery.data?.grafana_url,
    dashboardId: 'satellite-trade-overview',
    env: tradeEnv,
    namespace: ns,
  })
  const telemetryUnavailable =
    telemetryQuery.error instanceof Error &&
    telemetryQuery.error.message.includes('503')
  const prometheusConfigured = !telemetryUnavailable

  const apiMetricsEmpty =
    prometheusConfigured &&
    API_METRIC_IDS.every(id => {
      const metric = metricByID(telemetryQuery.data?.metrics, id)
      return metric == null || metric.status === 'empty'
    })

  const dataLayerEmpty = dataMetrics.every(m => m.points.length === 0)
  const telemetryError = telemetryQuery.isError && !telemetryUnavailable

  let verdictLamp: OpsVerdictLamp = 'ok'
  let verdictTag: string = 'METRICS OK'
  let verdictTagVariant: OpsVerdictTagVariant = 'success'
  let verdictSummary: string
  if (telemetryQuery.isLoading) {
    verdictLamp = 'unknown'
    verdictTag = 'LOADING'
    verdictTagVariant = 'neutral'
    verdictSummary = `Loading telemetry for ${ns}…`
  } else if (telemetryUnavailable || telemetryError) {
    verdictLamp = 'fail'
    verdictTag = 'ERROR'
    verdictTagVariant = 'danger'
    verdictSummary = telemetryUnavailable
      ? 'Prometheus unavailable — configure observability_urls.prometheus or PLATFORM_PROMETHEUS_URL.'
      : telemetryQuery.error instanceof Error
        ? telemetryQuery.error.message
        : 'Telemetry request failed.'
  } else if (apiMetricsEmpty || (apiRows.length === 0 && dataLayerEmpty)) {
    verdictLamp = 'degraded'
    verdictTag = 'NO DATA'
    verdictTagVariant = 'warning'
    verdictSummary = `No scrape data yet for ${ns} · wait for Prometheus targets after Track 4A/4B.`
  } else {
    const dataLayerLabel = dataLayerEmpty ? 'data layer empty' : 'data layer reporting'
    verdictSummary = `${apiRows.length} API service${apiRows.length === 1 ? '' : 's'} reporting · ${dataLayerLabel}`
  }

  return (
    <div className="flex flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Satellite runtime freshness"
        title={`SATELLITE RUNTIME · ${tradeEnv.toUpperCase()}`}
        lamp={verdictLamp}
        tagLabel={verdictTag}
        tagVariant={verdictTagVariant}
        summary={verdictSummary}
        actions={
          tradeDashUrl != null && layerB === 'ready' ? (
            <Button variant="outline" size="sm" asChild>
              <a href={tradeDashUrl} target="_blank" rel="noreferrer">
                Open Trade Dashboard
              </a>
            </Button>
          ) : undefined
        }
        meta={
          <span>
            Prometheus {prometheusConfigured ? 'configured' : 'not configured'} · {ns}
          </span>
        }
      />

      <PageToolbar align="between">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-medium text-muted-foreground shrink-0">Trade NS:</span>
            <SegmentControl
              value={tradeEnv}
              onChange={v => setTradeEnv(v as TradeEnv)}
              options={[...TRADE_ENV_OPTIONS]}
            />
            <SectionRefreshButton
              isFetching={telemetryQuery.isFetching || observabilityQuery.isFetching}
              onClick={() => {
                void observabilityQuery.refetch()
                void telemetryQuery.refetch()
              }}
            />
          </div>
      </PageToolbar>

      <MonitoringCoverageStrip
        layerB={layerB}
        prometheusConfigured={prometheusConfigured}
        loading={observabilityQuery.isLoading}
        onOpenCluster={onOpenCluster}
        onOpenObservability={onOpenObservability}
      />

      <OpsSection title="API Performance" bodyPadding="none" overflow="hidden">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Service</DenseTableHead>
              <DenseTableHead>Request rate</DenseTableHead>
              <DenseTableHead>P99 latency</DenseTableHead>
              <DenseTableHead>5xx rate</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {telemetryQuery.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={4} className="text-muted-foreground">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            ) : apiRows.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={4} className="text-muted-foreground">
                  No API metrics for {ns}
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              apiRows.map(row => (
                <DenseTableRow key={row.service}>
                  <DenseTableCell className="font-mono-tabular">{row.service}</DenseTableCell>
                  <DenseTableCell className="font-mono tabular-nums text-right">
                    {row.rate != null ? formatMetricValue(row.rate.value, 'req/s') : '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono tabular-nums text-right">
                    {row.p99 != null ? formatMetricValue(row.p99.value, 's') : '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono tabular-nums text-right">
                    {row.errorRate != null ? formatMetricValue(row.errorRate.value, 'ratio') : '—'}
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <OpsSection
        title="Shared data layer (evidence)"
        description="redis-ib / CNPG — shared dependencies; system rollup on Observability"
        bodyPadding="none"
        overflow="hidden"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Metric</DenseTableHead>
              <DenseTableHead>Instance</DenseTableHead>
              <DenseTableHead>Value</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {telemetryQuery.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={4} className="text-muted-foreground">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            ) : dataMetrics.every(m => m.points.length === 0) ? (
              <DenseTableRow>
                <DenseTableCell colSpan={4} className="text-muted-foreground">
                  No datastore metrics (CNPG PodMonitor + Redis exporter)
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              dataMetrics.flatMap(metric =>
                metric.points.length === 0
                  ? [
                      <DenseTableRow key={`${metric.id}-empty`}>
                        <DenseTableCell>{metric.title}</DenseTableCell>
                        <DenseTableCell>—</DenseTableCell>
                        <DenseTableCell>—</DenseTableCell>
                        <DenseTableCell className="text-muted-foreground">{metric.status}</DenseTableCell>
                      </DenseTableRow>,
                    ]
                  : metric.points.map((point, idx) => (
                      <DenseTableRow key={`${metric.id}-${idx}`}>
                        <DenseTableCell>{metric.title}</DenseTableCell>
                        <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">
                          {serviceLabel(point.labels)}
                        </DenseTableCell>
                        <DenseTableCell className="font-mono tabular-nums text-right">
                          {formatMetricValue(point.value, metric.unit)}
                        </DenseTableCell>
                        <DenseTableCell>{metric.status}</DenseTableCell>
                      </DenseTableRow>
                    )),
              )
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>
    </div>
  )
}
