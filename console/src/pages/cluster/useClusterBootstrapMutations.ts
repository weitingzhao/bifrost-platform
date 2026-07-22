import { useMutation } from '@tanstack/react-query'
import {
  ensureBifrostNamespaces,
  ensureKubePrometheusStack,
  ensureMetricsServer,
  syncClusterKubeconfig,
} from '@/api/clusterActuation'
import type { ClusterMutationActuation } from './clusterMutationTypes'

export function useClusterBootstrapMutations(
  actuation: ClusterMutationActuation,
  canAdmin: boolean,
  observability: { layer_b_install_enabled?: boolean } | undefined,
  setSyncError: (message: string | null) => void,
) {
  const { handleActuationSuccess, handleActuationError, requireConfirm, setActionError, qc } = actuation

  const syncMutation = useMutation({
    mutationFn: syncClusterKubeconfig,
    onSuccess: data => {
      setSyncError(data.ok ? null : data.message)
      if (data.ok) {
        void qc.invalidateQueries({ queryKey: ['cluster'] })
      }
    },
    onError: (err: Error) => setSyncError(err.message),
  })

  const ensureMutation = useMutation({
    mutationFn: ensureBifrostNamespaces,
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const metricsServerMutation = useMutation({
    mutationFn: ensureMetricsServer,
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const layerBInstallMutation = useMutation({
    mutationFn: ensureKubePrometheusStack,
    onSuccess: data => {
      if (!data.ok) {
        handleActuationError(new Error(data.message || 'Layer B install failed'))
        return
      }
      handleActuationSuccess(data.message)
      void qc.invalidateQueries({ queryKey: ['cluster', 'observability'] })
    },
    onError: handleActuationError,
  })

  const layerBInstallEnabled = observability?.layer_b_install_enabled === true
  const layerBInstallBlockedReason = !canAdmin
    ? 'Install Layer B requires an admin token.'
    : !layerBInstallEnabled
      ? 'Layer B install is disabled on platform-api. Set PLATFORM_OBSERVABILITY_INSTALL_ENABLED=1 and restart platform-api.'
      : null

  function handleEnsureMetricsServer() {
    requireConfirm({
      title: 'Install metrics-server',
      message:
        'This installs metrics-server in kube-system (Layer A). Required for live CPU/memory usage and top pods. Does not install Prometheus or Grafana (Layer B).',
      confirmLabel: 'Install metrics-server',
      action: () => metricsServerMutation.mutate(),
    })
  }

  function handleInstallLayerB() {
    if (layerBInstallBlockedReason != null) {
      setActionError(layerBInstallBlockedReason)
      return
    }
    requireConfirm({
      title: 'Install Layer B observability',
      message:
        'This installs kube-prometheus-stack in the monitoring namespace via platform-api actuation (Prometheus, Grafana, Alertmanager, node-exporter, kube-state-metrics).',
      confirmLabel: 'Install Layer B',
      action: () => layerBInstallMutation.mutate(),
    })
  }

  function handleEnsureNamespaces() {
    requireConfirm({
      title: 'Ensure Bifrost namespaces',
      message:
        'This creates missing Bifrost namespaces from clusters.yaml. Existing namespaces are left unchanged.',
      confirmLabel: 'Ensure namespaces',
      action: () => ensureMutation.mutate(),
    })
  }

  return {
    syncMutation,
    ensureMutation,
    metricsServerMutation,
    layerBInstallMutation,
    layerBInstallBlockedReason,
    handleEnsureMetricsServer,
    handleInstallLayerB,
    handleEnsureNamespaces,
  }
}

export type ClusterBootstrapMutations = ReturnType<typeof useClusterBootstrapMutations>
