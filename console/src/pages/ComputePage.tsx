import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { Button } from '@bifrost/ui'
import { cordonNode, drainNode, joinClusterNode, powerOffComputeNode, scaleDeployment, uncordonNode, wakeComputeNode } from '@/api/clusterActuation'
import { fetchClusterMetrics, fetchClusterNodes, fetchJoinProfiles, fetchNodePower } from '@/api/cluster'
import type { ClusterNode, ComputeWorkloadStatus } from '@/api/clusterTypes'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ClusterNodeDrawer } from '@/components/cluster/ClusterNodeDrawer'
import { ClusterNodeWizardPanel } from '@/components/cluster/ClusterNodeWizardPanel'
import { ClusterNodesTable } from '@/components/cluster/ClusterNodesTable'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { NodeWizardFlow, WizardAction } from '@/lib/cluster/nodeWizard'

interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmLabel: string
  action: () => void
}

export function ComputePage({
  onOpenCluster,
  onOpenAudit,
}: {
  onOpenCluster?: () => void
  onOpenAudit?: () => void
}) {
  const qc = useQueryClient()
  const [selectedNode, setSelectedNode] = useState<ClusterNode | null>(null)
  const [nodeDrawerOpen, setNodeDrawerOpen] = useState(false)
  const [wizardFlow, setWizardFlow] = useState<NodeWizardFlow>('maintenance')
  const [wizardJoinProfileId, setWizardJoinProfileId] = useState<string | null>(null)
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const { canOperate, canAdmin, caps, capsLoading } = usePlatformAuth()

  const nodesQuery = useQuery({
    queryKey: ['cluster', 'nodes'],
    queryFn: fetchClusterNodes,
    refetchInterval: 30_000,
  })

  const metricsQuery = useQuery({
    queryKey: ['cluster', 'metrics'],
    queryFn: () => fetchClusterMetrics(8),
    refetchInterval: 30_000,
  })

  const joinProfilesQuery = useQuery({
    queryKey: ['cluster', 'join-profiles'],
    queryFn: fetchJoinProfiles,
    refetchInterval: 60_000,
  })

  const clusterNodes = nodesQuery.data?.nodes ?? []

  const selectedNodeLive = useMemo(() => {
    if (selectedNode?.name == null) return null
    return clusterNodes.find(n => n.name === selectedNode.name) ?? selectedNode
  }, [clusterNodes, selectedNode])

  const nodePowerQuery = useQuery({
    queryKey: ['cluster', 'node-power', selectedNodeLive?.name],
    queryFn: () => fetchNodePower(selectedNodeLive?.name ?? ''),
    enabled:
      selectedNodeLive?.compute_managed === true &&
      selectedNodeLive.name != null &&
      (nodeDrawerOpen || wizardFlow !== 'join'),
    refetchInterval: 15_000,
  })

  const wakeNodeMutation = useMutation({
    mutationFn: (nodeName: string) => wakeComputeNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const powerOffNodeMutation = useMutation({
    mutationFn: (nodeName: string) => powerOffComputeNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const cordonNodeMutation = useMutation({
    mutationFn: (nodeName: string) => cordonNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const uncordonNodeMutation = useMutation({
    mutationFn: (nodeName: string) => uncordonNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const drainNodeMutation = useMutation({
    mutationFn: (nodeName: string) => drainNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const joinNodeMutation = useMutation({
    mutationFn: (profile: string) => joinClusterNode(profile),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const scaleMutation = useMutation({
    mutationFn: scaleDeployment,
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  function handleActuationSuccess(_message: string) {
    setActionError(null)
    setConfirmState(null)
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
  }

  function handleActuationError(err: Error) {
    setConfirmState(null)
    setActionError(err.message)
  }

  function requireConfirm(next: Omit<ConfirmState, 'open'>) {
    setActionError(null)
    setConfirmState({ ...next, open: true })
  }

  function handleSelectNode(node: ClusterNode) {
    setSelectedNode(node)
    setNodeDrawerOpen(true)
    if (node.compute_managed) {
      setWizardFlow('compute_shutdown')
    }
  }

  function handleWizardSelectNodeName(name: string | null) {
    if (name == null) {
      setSelectedNode(null)
      setNodeDrawerOpen(false)
      return
    }
    const node = clusterNodes.find(n => n.name === name) ?? null
    setSelectedNode(node)
    setNodeDrawerOpen(false)
  }

  function handleWakeComputeNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Wake compute node',
      message: `Send Wake-on-LAN to ${selectedNode.name}. The node should appear Ready within a few minutes.`,
      confirmLabel: 'Wake node',
      action: () => wakeNodeMutation.mutate(selectedNode.name),
    })
  }

  function handlePowerOffComputeNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Power off compute node',
      message: `Drain ${selectedNode.name} and send systemctl poweroff via SSH. Running workloads will be evicted.`,
      confirmLabel: 'Power off',
      action: () => powerOffNodeMutation.mutate(selectedNode.name),
    })
  }

  function handleCordonNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Cordon node',
      message: `Prevent new pods from scheduling on ${selectedNode.name}. Existing pods keep running.`,
      confirmLabel: 'Cordon',
      action: () => cordonNodeMutation.mutate(selectedNode.name),
    })
  }

  function handleUncordonNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Uncordon node',
      message: `Re-enable scheduling on ${selectedNode.name}.`,
      confirmLabel: 'Uncordon',
      action: () => uncordonNodeMutation.mutate(selectedNode.name),
    })
  }

  function handleDrainNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Drain node',
      message: `Evict user workloads from ${selectedNode.name}. DaemonSets remain. This does not shut down the machine.`,
      confirmLabel: 'Drain node',
      action: () => drainNodeMutation.mutate(selectedNode.name),
    })
  }

  function handleJoinNode(profileId: string, label: string) {
    requireConfirm({
      title: 'Join K3s node',
      message: `Run join profile "${label}" via infra script. Requires K3S_TOKEN or ~/.bifrost-k3s-node-token and PLATFORM_NODE_JOIN_ENABLED=1 on platform-api.`,
      confirmLabel: 'Run join',
      action: () => joinNodeMutation.mutate(profileId),
    })
  }

  function handleScaleComputeWorkload(workload: ComputeWorkloadStatus, replicas: number) {
    const verb = replicas === 0 ? 'Scale down' : 'Scale up'
    requireConfirm({
      title: `${verb} ${workload.label}`,
      message: `Set ${workload.namespace}/${workload.name} replicas to ${replicas}.`,
      confirmLabel: verb,
      action: () =>
        scaleMutation.mutate({
          namespace: workload.namespace,
          kind: 'Deployment',
          name: workload.name,
          replicas,
        }),
    })
  }

  function handleWizardAction(action: WizardAction, context?: { profileId?: string }) {
    switch (action) {
      case 'cordon':
        handleCordonNode()
        break
      case 'drain':
        handleDrainNode()
        break
      case 'uncordon':
        handleUncordonNode()
        break
      case 'wake':
        handleWakeComputeNode()
        break
      case 'poweroff':
        handlePowerOffComputeNode()
        break
      case 'join': {
        const profileId = context?.profileId ?? wizardJoinProfileId ?? joinProfilesQuery.data?.profiles[0]?.id
        const profile = joinProfilesQuery.data?.profiles.find(p => p.id === profileId)
        if (profile != null) {
          handleJoinNode(profile.id, profile.label)
        }
        break
      }
      default:
        break
    }
  }

  function actionPending() {
    return (
      wakeNodeMutation.isPending ||
      powerOffNodeMutation.isPending ||
      cordonNodeMutation.isPending ||
      uncordonNodeMutation.isPending ||
      drainNodeMutation.isPending ||
      joinNodeMutation.isPending ||
      scaleMutation.isPending
    )
  }

  const authLabel = canOperate
    ? `${caps?.principal ?? 'operator'}${canAdmin ? ' (admin)' : ''}`
    : capsLoading
      ? null
      : 'Authenticate to actuate'

  const notReadyNodes = clusterNodes.filter(
    n => n.status !== 'Ready' || n.elastic_mode === 'degraded' || n.reachability === 'fail',
  )
  const notReadyCount = notReadyNodes.length
  const readyCount = clusterNodes.filter(n => n.status === 'Ready').length

  let verdictLamp: OpsVerdictLamp
  let verdictTagLabel: string
  let verdictTagVariant: OpsVerdictTagVariant
  if (nodesQuery.isError) {
    verdictLamp = 'fail'
    verdictTagLabel = 'ERROR'
    verdictTagVariant = 'danger'
  } else if (nodesQuery.isLoading && nodesQuery.data == null) {
    verdictLamp = 'unknown'
    verdictTagLabel = 'LOADING'
    verdictTagVariant = 'neutral'
  } else if (clusterNodes.length === 0) {
    verdictLamp = 'unknown'
    verdictTagLabel = 'NO NODES'
    verdictTagVariant = 'neutral'
  } else if (notReadyCount === 0) {
    verdictLamp = 'ok'
    verdictTagLabel = 'READY'
    verdictTagVariant = 'success'
  } else if (notReadyCount === clusterNodes.length) {
    verdictLamp = 'fail'
    verdictTagLabel = `${notReadyCount} NOT READY`
    verdictTagVariant = 'danger'
  } else {
    verdictLamp = 'degraded'
    verdictTagLabel = `${notReadyCount} NOT READY`
    verdictTagVariant = 'warning'
  }

  const verdictSummary =
    nodesQuery.isError && nodesQuery.error instanceof Error
      ? nodesQuery.error.message
      : (nodesQuery.data?.detail ??
        (nodesQuery.isLoading
          ? 'Loading nodes…'
          : clusterNodes.length === 0
            ? 'No cluster nodes reported'
            : `${readyCount}/${clusterNodes.length} nodes Ready`))

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Compute nodes verdict"
        title="COMPUTE · NODES"
        lamp={verdictLamp}
        tagLabel={verdictTagLabel}
        tagVariant={verdictTagVariant}
        summary={verdictSummary}
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={nodesQuery.isFetching}
            onClick={() => void qc.invalidateQueries({ queryKey: ['cluster', 'nodes'] })}
          >
            {nodesQuery.isFetching ? 'Refreshing…' : 'Refresh'}
          </Button>
        }
        meta={
          <>
            {authLabel != null ? <span>{authLabel}</span> : null}
            {onOpenAudit != null ? (
              <button type="button" className="focus-strip-link shrink-0" onClick={onOpenAudit}>
                Audit
              </button>
            ) : null}
            {onOpenCluster != null ? (
              <button type="button" className="focus-strip-link shrink-0" onClick={onOpenCluster}>
                Rocket → Cluster
              </button>
            ) : null}
          </>
        }
      />

      {actionError != null && (
        <OpsFeedback variant="warning" title="Actuation">
          {actionError}
        </OpsFeedback>
      )}

      <div className="cluster-view-panels flex flex-col gap-2">
        <ClusterNodeWizardPanel
          flow={wizardFlow}
          onFlowChange={setWizardFlow}
          nodes={clusterNodes}
          selectedNodeName={selectedNodeLive?.name ?? null}
          onSelectNodeName={handleWizardSelectNodeName}
          selectedNode={selectedNodeLive}
          power={nodePowerQuery.data}
          joinProfiles={joinProfilesQuery.data}
          selectedJoinProfileId={wizardJoinProfileId ?? joinProfilesQuery.data?.profiles[0]?.id ?? null}
          onSelectJoinProfileId={setWizardJoinProfileId}
          canOperate={canOperate}
          canAdmin={canAdmin}
          actionPending={actionPending()}
          onWizardAction={handleWizardAction}
          onOpenNodeDetails={() => setNodeDrawerOpen(true)}
        />
        <ClusterNodesTable
          nodes={clusterNodes}
          isLoading={nodesQuery.isLoading}
          isFetching={nodesQuery.isFetching}
          metricsAvailable={metricsQuery.data?.metrics_server_available}
          selectedNode={selectedNode?.name ?? null}
          onSelectNode={handleSelectNode}
        />
      </div>

      <ClusterNodeDrawer
        open={nodeDrawerOpen}
        node={selectedNodeLive}
        power={nodePowerQuery.data}
        powerLoading={nodePowerQuery.isLoading}
        powerError={nodePowerQuery.error instanceof Error ? nodePowerQuery.error.message : null}
        canOperate={canOperate}
        canAdmin={canAdmin}
        actionPending={actionPending()}
        onClose={() => {
          setNodeDrawerOpen(false)
          setSelectedNode(null)
        }}
        onCordon={handleCordonNode}
        onUncordon={handleUncordonNode}
        onDrain={handleDrainNode}
        onWake={selectedNode?.compute_managed ? handleWakeComputeNode : undefined}
        onPowerOff={selectedNode?.compute_managed ? handlePowerOffComputeNode : undefined}
        onScaleWorkload={selectedNode?.compute_managed ? handleScaleComputeWorkload : undefined}
      />

      <ConfirmDialog
        open={confirmState?.open === true}
        title={confirmState?.title ?? ''}
        message={confirmState?.message ?? ''}
        confirmLabel={confirmState?.confirmLabel}
        confirming={actionPending()}
        onConfirm={() => confirmState?.action()}
        onCancel={() => setConfirmState(null)}
      />
    </div>
  )
}
