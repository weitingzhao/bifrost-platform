import { DenseTag, StatusLamp } from '@bifrost/ui'
import type { ClusterMetricsResponse, ClusterObservabilityResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'

interface SatelliteObservabilityStripProps {
  metrics: ClusterMetricsResponse | undefined
  observability: ClusterObservabilityResponse | undefined
  metricsLoading: boolean
  observabilityLoading: boolean
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  variant?: 'elevated' | 'flat'
}

function layerBLabel(status: ClusterObservabilityResponse['layer_b_status'] | undefined): string {
  switch (status) {
    case 'ready':
      return 'Ready'
    case 'partial':
      return 'Partial'
    default:
      return 'Not installed'
  }
}

function layerBLamp(status: ClusterObservabilityResponse['layer_b_status'] | undefined) {
  switch (status) {
    case 'ready':
      return 'ok' as const
    case 'partial':
      return 'degraded' as const
    default:
      return 'unknown' as const
  }
}

export function SatelliteObservabilityStrip({
  metrics,
  observability,
  metricsLoading,
  observabilityLoading,
  onOpenCluster,
  onOpenTelemetry,
  variant = 'elevated',
}: SatelliteObservabilityStripProps) {
  const layerA = metrics?.metrics_server_available === true
  const layerB = observability?.layer_b_status

  return (
    <OpsSection
      variant={variant}
      title="Observability"
      bodyPadding="compact"
      overflow="visible"
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div className="flex items-center gap-2">
          <StatusLamp value={layerA ? 'ok' : metricsLoading ? 'unknown' : 'fail'} kind="reach" />
          <span className="text-[var(--text-dense-label)] font-medium">Layer A</span>
          <DenseTag variant={layerA ? 'success' : 'neutral'}>
            {metricsLoading ? '…' : layerA ? 'metrics-server' : 'missing'}
          </DenseTag>
        </div>
        <div className="flex items-center gap-2">
          <StatusLamp value={layerBLamp(layerB)} kind="reach" />
          <span className="text-[var(--text-dense-label)] font-medium">Layer B</span>
          <DenseTag variant={layerB === 'ready' ? 'success' : layerB === 'partial' ? 'warning' : 'neutral'}>
            {observabilityLoading ? '…' : layerBLabel(layerB)}
          </DenseTag>
          {observability?.grafana_url != null && observability.grafana_url !== '' && layerB === 'ready' && (
            <a
              href={observability.grafana_url}
              target="_blank"
              rel="noreferrer"
              className="text-[var(--text-dense-meta)] text-primary underline-offset-2 hover:underline"
            >
              Open Grafana
            </a>
          )}
        </div>
        {onOpenCluster != null && (
          <button type="button" className="focus-strip-link text-[var(--text-dense-meta)]" onClick={onOpenCluster}>
            Rocket → Cluster
          </button>
        )}
        {onOpenTelemetry != null && (
          <button type="button" className="focus-strip-link text-[var(--text-dense-meta)]" onClick={onOpenTelemetry}>
            View Telemetry
          </button>
        )}
      </div>
      {layerB === 'not_installed' && !observabilityLoading && (
        <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Install Layer B via Rocket → Cluster for historical metrics and alerts.
        </p>
      )}
    </OpsSection>
  )
}
