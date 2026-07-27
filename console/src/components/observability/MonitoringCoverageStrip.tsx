/**
 * Compact monitoring coverage strip for Satellite Bus / API & Auth Probes / Runtime pages.
 * Links to Mission Control → Observability — does not re-derive global verdicts.
 */

import { DenseTag, StatusLamp } from '@bifrost/ui'
import type { LayerBStatus } from '@/api/clusterTypes'
import { OpsSection } from '@/components/layout/OpsSection'

function layerBLamp(status: LayerBStatus | undefined) {
  switch (status) {
    case 'ready':
      return 'ok' as const
    case 'partial':
      return 'degraded' as const
    default:
      return 'unknown' as const
  }
}

function layerBLabel(status: LayerBStatus | undefined): string {
  switch (status) {
    case 'ready':
      return 'Ready'
    case 'partial':
      return 'Partial'
    case 'not_installed':
      return 'Not installed'
    default:
      return 'Unknown'
  }
}

export function MonitoringCoverageStrip({
  layerB,
  prometheusConfigured,
  loading,
  onOpenObservability,
  onOpenCluster,
  variant = 'flat',
  title = 'Monitoring coverage',
}: {
  layerB: LayerBStatus | undefined
  prometheusConfigured: boolean
  loading?: boolean
  onOpenObservability?: () => void
  onOpenCluster?: () => void
  variant?: 'elevated' | 'flat'
  title?: string
}) {
  return (
    <OpsSection
      variant={variant}
      title={title}
      bodyPadding="compact"
      overflow="visible"
      description="Layer A/B install stays on Rocket → Cluster · system health on Mission Control → Observability"
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div className="flex items-center gap-1.5">
          <StatusLamp value={layerBLamp(layerB)} kind="reach" />
          <span className="text-[var(--text-dense-caption)] font-medium">Layer B</span>
          <DenseTag variant={layerB === 'ready' ? 'success' : layerB === 'partial' ? 'warning' : 'neutral'}>
            {loading ? '…' : layerBLabel(layerB)}
          </DenseTag>
        </div>
        <div className="flex items-center gap-1.5">
          <StatusLamp value={prometheusConfigured ? 'ok' : 'unknown'} kind="reach" />
          <span className="text-[var(--text-dense-caption)] font-medium">Prometheus</span>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {loading ? '…' : prometheusConfigured ? 'configured' : 'not configured'}
          </span>
        </div>
        {onOpenObservability != null && (
          <button
            type="button"
            className="focus-strip-link text-[var(--text-dense-caption)]"
            onClick={onOpenObservability}
          >
            View Observability
          </button>
        )}
        {onOpenCluster != null && (
          <button
            type="button"
            className="focus-strip-link text-[var(--text-dense-caption)]"
            onClick={onOpenCluster}
          >
            Rocket → Cluster
          </button>
        )}
      </div>
    </OpsSection>
  )
}
