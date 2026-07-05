import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Button, PageHeader } from '@bifrost/ui'
import {
  ensureKubePrometheusStack,
  fetchCluster,
  fetchClusterMetrics,
  fetchClusterObservability,
} from '@/api/platform'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ClusterObservabilityPanel } from '@/components/cluster/ClusterObservabilityPanel'
import { ClusterTopPodsTable } from '@/components/cluster/ClusterTopPodsTable'
import { ClusterOverviewKpi } from '@/components/cluster/ClusterOverviewKpi'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmLabel: string
  action: () => void
}

const LAYER_B_INSTALL_DISABLED_MSG =
  'Layer B install is disabled on platform-api. Set PLATFORM_OBSERVABILITY_INSTALL_ENABLED=1 and restart platform-api.'

export function ObservabilityPage({
  onOpenCluster,
  onOpenStandards,
  onOpenRuntimeMap,
}: {
  onOpenCluster?: () => void
  onOpenStandards?: () => void
  onOpenRuntimeMap?: () => void
}) {
  const qc = useQueryClient()
  const { canAdmin, canOperate } = usePlatformAuth()
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionInfo, setActionInfo] = useState<string | null>(null)

  const metricsQuery = useQuery({
    queryKey: ['cluster', 'metrics'],
    queryFn: () => fetchClusterMetrics(8),
    refetchInterval: 30_000,
  })

  const summaryQuery = useQuery({
    queryKey: ['cluster', 'summary'],
    queryFn: fetchCluster,
    refetchInterval: 30_000,
  })

  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
    retry: false,
  })

  const installMutation = useMutation({
    mutationFn: ensureKubePrometheusStack,
    onSuccess: data => {
      setConfirmState(null)
      if (!data.ok) {
        setActionInfo(null)
        setActionError(data.message || 'Layer B install failed')
        return
      }
      setActionError(null)
      setActionInfo(data.message)
      void qc.invalidateQueries({ queryKey: ['cluster', 'observability'] })
      void qc.invalidateQueries({ queryKey: ['cluster', 'metrics'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => {
      setConfirmState(null)
      setActionInfo(null)
      setActionError(err.message)
    },
  })

  const layerBInstallEnabled = observabilityQuery.data?.layer_b_install_enabled === true
  const layerBReady = observabilityQuery.data?.layer_b_status === 'ready'
  const installBlockedReason = !canAdmin
    ? 'Install Layer B requires an admin token.'
    : !layerBInstallEnabled
      ? LAYER_B_INSTALL_DISABLED_MSG
      : null

  function requireConfirm(next: Omit<ConfirmState, 'open'>) {
    setActionError(null)
    setActionInfo(null)
    window.setTimeout(() => {
      setConfirmState({ ...next, open: true })
    }, 0)
  }

  function handleInstallLayerB() {
    if (!canAdmin) {
      setActionInfo(null)
      setActionError('Install Layer B requires an admin token.')
      return
    }
    if (!layerBInstallEnabled) {
      setActionInfo(null)
      setActionError(LAYER_B_INSTALL_DISABLED_MSG)
      return
    }
    requireConfirm({
      title: 'Install Layer B observability',
      message:
        'This installs kube-prometheus-stack in the monitoring namespace via platform-api actuation (Prometheus, Grafana, Alertmanager, node-exporter, kube-state-metrics).',
      confirmLabel: 'Install Layer B',
      action: () => installMutation.mutate(),
    })
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="Observability"
        description="Layer A metrics-server and Layer B kube-prometheus-stack installation status and actuation."
      />

      <section className="page-section panel-elevated px-4 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {layerBReady
              ? 'Layer B ready.'
              : 'Layer B not fully installed. Confirm install below when actuation is enabled.'}
            {!canOperate && (
              <>
                <span className="mx-1.5 text-[var(--muted-foreground)]/50">·</span>
                Authenticate to actuate
              </>
            )}
          </p>
          <div className="flex flex-wrap gap-2">
            {onOpenCluster != null && (
              <Button variant="outline" size="sm" onClick={onOpenCluster}>
                Open Cluster
              </Button>
            )}
            {observabilityQuery.data?.grafana_url != null &&
              observabilityQuery.data.grafana_url !== '' &&
              layerBReady && (
                <Button size="sm" asChild>
                  <a href={observabilityQuery.data.grafana_url} target="_blank" rel="noreferrer">
                    Open Grafana
                  </a>
                </Button>
              )}
          </div>
        </div>
        {installBlockedReason != null && !layerBReady && (
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] lamp-warn">{installBlockedReason}</p>
        )}
        {actionInfo != null && (
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{actionInfo}</p>
        )}
        {actionError != null && <p className="m-0 mt-1 text-[var(--text-dense-meta)] lamp-warn">{actionError}</p>}
      </section>

      <ClusterOverviewKpi
        summary={summaryQuery.data}
        metrics={metricsQuery.data}
        isLoading={metricsQuery.isLoading || summaryQuery.isLoading}
      />

      <section className="cluster-global-top-pods page-section" aria-label="Cluster-wide pod resource usage">
        <ClusterTopPodsTable metrics={metricsQuery.data} isLoading={metricsQuery.isLoading} />
      </section>

      <ClusterObservabilityPanel
        data={observabilityQuery.data}
        isLoading={observabilityQuery.isLoading}
        onOpenStandards={onOpenStandards}
        onOpenRuntimeMap={onOpenRuntimeMap}
        onInstallLayerB={handleInstallLayerB}
        installLayerBPending={installMutation.isPending}
        installLayerBDisabled={installBlockedReason != null}
      />

      <ConfirmDialog
        open={confirmState?.open === true}
        title={confirmState?.title ?? ''}
        message={confirmState?.message ?? ''}
        confirmLabel={confirmState?.confirmLabel}
        confirming={installMutation.isPending}
        onConfirm={() => confirmState?.action()}
        onCancel={() => setConfirmState(null)}
      />
    </div>
  )
}
