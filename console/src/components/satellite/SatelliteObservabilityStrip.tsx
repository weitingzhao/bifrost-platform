/**
 * Compact monitoring coverage for Satellite pages.
 * Global system verdict lives on Mission Control → Observability — do not re-derive here.
 */

import { useQuery } from '@tanstack/react-query'
import { fetchClusterObservability } from '@/api/cluster'
import { MonitoringCoverageStrip } from '@/components/observability/MonitoringCoverageStrip'

interface SatelliteObservabilityStripProps {
  /** @deprecated Prefer MonitoringCoverageStrip; kept for call-site compatibility. */
  metrics?: unknown
  observability?: { layer_b_status?: 'not_installed' | 'partial' | 'ready' } | undefined
  metricsLoading?: boolean
  observabilityLoading?: boolean
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  onOpenObservability?: () => void
  variant?: 'elevated' | 'flat'
  /** Override OpsSection title (e.g. Shared lane “4 · Monitoring coverage”). */
  title?: string
}

export function SatelliteObservabilityStrip({
  observability: observabilityProp,
  observabilityLoading,
  onOpenCluster,
  onOpenObservability,
  onOpenTelemetry,
  variant = 'elevated',
  title = 'Monitoring coverage',
}: SatelliteObservabilityStripProps) {
  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 60_000,
    enabled: observabilityProp == null,
    retry: false,
  })

  const layerB = observabilityProp?.layer_b_status ?? observabilityQuery.data?.layer_b_status
  const loading = observabilityLoading ?? observabilityQuery.isLoading
  // With only the prop shape (layer_b_status), both ready and partial mean the
  // Prometheus stack is configured — partial must not display "not configured".
  const prometheusConfigured =
    observabilityProp != null
      ? observabilityProp.layer_b_status === 'ready' ||
        observabilityProp.layer_b_status === 'partial'
      : observabilityQuery.data?.prometheus_url != null &&
        observabilityQuery.data.prometheus_url !== ''

  return (
    <MonitoringCoverageStrip
      variant={variant}
      title={title}
      layerB={layerB}
      prometheusConfigured={prometheusConfigured}
      loading={loading}
      onOpenCluster={onOpenCluster}
      onOpenObservability={onOpenObservability ?? onOpenTelemetry}
    />
  )
}
