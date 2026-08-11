import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { ConfirmState, ClusterMutationActuation, ClusterPageMutationsInput, ScaleState } from './clusterMutationTypes'
export type { ConfirmState, ScaleState, ClusterPageMutationsInput } from './clusterMutationTypes'
import { useClusterBootstrapMutations } from './useClusterBootstrapMutations'
import { useClusterNodeMutations } from './useClusterNodeMutations'
import { useClusterRemediationMutations } from './useClusterRemediationMutations'
import { useClusterWorkloadMutations } from './useClusterWorkloadMutations'

export function useClusterPageMutations(input: ClusterPageMutationsInput) {
  const {
    selectedNode,
    wizardJoinProfileId,
    joinProfiles,
    canAdmin,
    observability,
    clusterSummary,
    serviceReadiness,
    governance,
    postgresStatus,
    queries,
    selectedNs,
    onOpenAgentDesk,
    onStartAgentJob,
    onExpandAgentDock,
    onSelectAgentJob,
    setDrawerOpen,
    setSelectedPod,
  } = input

  const qc = useQueryClient()
  const [syncError, setSyncError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  /** Actuation / Auto-Check success summary (may be markdown) — not kubeconfig sync. */
  const [actionSuccess, setActionSuccess] = useState<string | null>(null)
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null)
  const [scaleState, setScaleState] = useState<ScaleState | null>(null)

  function handleActuationSuccess(message: string) {
    setActionError(null)
    setConfirmState(null)
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    setActionSuccess(message)
  }

  function handleActuationError(err: Error) {
    setConfirmState(null)
    setActionSuccess(null)
    setActionError(err.message)
  }

  function requireConfirm(next: Omit<ConfirmState, 'open'>) {
    setActionError(null)
    setActionSuccess(null)
    setConfirmState({ ...next, open: true })
  }

  const actuation: ClusterMutationActuation = {
    handleActuationSuccess,
    handleActuationError,
    requireConfirm,
    setActionError,
    setScaleState,
    qc,
  }

  const bootstrap = useClusterBootstrapMutations(actuation, canAdmin, observability, setSyncError)
  const workloads = useClusterWorkloadMutations(actuation, setDrawerOpen, setSelectedPod)
  const nodes = useClusterNodeMutations(actuation, selectedNode, wizardJoinProfileId, joinProfiles)
  const remediation = useClusterRemediationMutations(actuation, {
    clusterSummary,
    serviceReadiness,
    governance,
    postgresStatus,
    queries,
    selectedNs,
    onOpenAgentDesk,
    onStartAgentJob,
    onExpandAgentDock,
    onSelectAgentJob,
  })

  function actionPending() {
    return (
      bootstrap.ensureMutation.isPending ||
      bootstrap.metricsServerMutation.isPending ||
      bootstrap.layerBInstallMutation.isPending ||
      workloads.restartMutation.isPending ||
      workloads.scaleMutation.isPending ||
      workloads.deletePodMutation.isPending ||
      nodes.wakeNodeMutation.isPending ||
      nodes.powerOffNodeMutation.isPending ||
      nodes.cordonNodeMutation.isPending ||
      nodes.uncordonNodeMutation.isPending ||
      nodes.drainNodeMutation.isPending ||
      nodes.joinNodeMutation.isPending
    )
  }

  return {
    syncError,
    setSyncError,
    actionError,
    setActionError,
    actionSuccess,
    setActionSuccess,
    confirmState,
    setConfirmState,
    scaleState,
    setScaleState,
    remediationPanelOpen: remediation.remediationPanelOpen,
    setRemediationPanelOpen: remediation.setRemediationPanelOpen,
    remediationJobId: remediation.remediationJobId,
    remediationJob: remediation.remediationJob,
    playbookFixMutation: remediation.playbookFixMutation,
    syncMutation: bootstrap.syncMutation,
    ensureMutation: bootstrap.ensureMutation,
    metricsServerMutation: bootstrap.metricsServerMutation,
    layerBInstallMutation: bootstrap.layerBInstallMutation,
    restartMutation: workloads.restartMutation,
    scaleMutation: workloads.scaleMutation,
    remediationStartMutation: remediation.remediationStartMutation,
    remediationCancelMutation: remediation.remediationCancelMutation,
    layerBInstallBlockedReason: bootstrap.layerBInstallBlockedReason,
    handleWakeComputeNode: nodes.handleWakeComputeNode,
    handlePowerOffComputeNode: nodes.handlePowerOffComputeNode,
    handleCordonNode: nodes.handleCordonNode,
    handleUncordonNode: nodes.handleUncordonNode,
    handleDrainNode: nodes.handleDrainNode,
    handleScaleComputeWorkload: workloads.handleScaleComputeWorkload,
    handleEnsureMetricsServer: bootstrap.handleEnsureMetricsServer,
    handleInstallLayerB: bootstrap.handleInstallLayerB,
    handleEnsureNamespaces: bootstrap.handleEnsureNamespaces,
    handleRestartDeployment: workloads.handleRestartDeployment,
    handleDeletePod: workloads.handleDeletePod,
    handleWizardAction: nodes.handleWizardAction,
    actionPending,
    handleAutoRemediate: remediation.handleAutoRemediate,
    followAmbientRemediationJob: remediation.followAmbientRemediationJob,
    handleOpenRemediationSession: remediation.handleOpenRemediationSession,
    handleRemediationComplete: remediation.handleRemediationComplete,
  }
}

export type ClusterPageMutations = ReturnType<typeof useClusterPageMutations>
