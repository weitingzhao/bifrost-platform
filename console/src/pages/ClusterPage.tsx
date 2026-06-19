import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { Button } from '@bifrost/ui'
import {
  cordonNode,
  deletePod,
  drainNode,
  ensureBifrostNamespaces,
  ensureMetricsServer,
  fetchCluster,
  fetchClusterEvents,
  fetchClusterMetrics,
  fetchClusterNamespaces,
  fetchClusterNodes,
  fetchClusterPlacement,
  fetchClusterObservability,
  fetchClusterWorkloads,
  fetchJoinProfiles,
  fetchNodePower,
  fetchPodLogs,
  joinClusterNode,
  powerOffComputeNode,
  rolloutRestartDeployment,
  scaleDeployment,
  syncClusterKubeconfig,
  uncordonNode,
  wakeComputeNode,
} from '@/api/platform'
import type { ClusterNode, ClusterWorkload, ComputeWorkloadStatus } from '@/api/types'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { ClusterNodeDrawer } from '@/components/cluster/ClusterNodeDrawer'
import { ClusterNodeWizardPanel } from '@/components/cluster/ClusterNodeWizardPanel'
import { ClusterWorkloadsExplorer } from '@/components/cluster/ClusterWorkloadsExplorer'
import { ClusterDrawer } from '@/components/cluster/ClusterDrawer'
import { ClusterNodesTable } from '@/components/cluster/ClusterNodesTable'
import { ClusterObservabilityPanel } from '@/components/cluster/ClusterObservabilityPanel'
import { ClusterOverviewKpi } from '@/components/cluster/ClusterOverviewKpi'
import { ClusterTopPodsTable } from '@/components/cluster/ClusterTopPodsTable'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { bifrostNamespacesReady, clusterBootstrapNeedsActions } from '@/lib/cluster/clusterBootstrap'
import type { NodeWizardFlow, WizardAction } from '@/lib/cluster/nodeWizard'

type NsFilter = 'all' | 'bifrost'

interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmLabel: string
  action: () => void
}

interface ScaleState {
  workload: ClusterWorkload
  replicas: number
}

export function ClusterPage({
  onOpenStandards,
  onOpenEnvironments,
  onOpenAudit,
}: {
  onOpenStandards?: () => void
  onOpenEnvironments?: () => void
  onOpenAudit?: () => void
}) {
  const qc = useQueryClient()
  const [nsFilter, setNsFilter] = useState<NsFilter>('bifrost')
  const [selectedNs, setSelectedNs] = useState<string | null>('bifrost-stg')
  const [selectedPod, setSelectedPod] = useState<string | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [selectedNode, setSelectedNode] = useState<ClusterNode | null>(null)
  const [nodeDrawerOpen, setNodeDrawerOpen] = useState(false)
  const [syncError, setSyncError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null)
  const [scaleState, setScaleState] = useState<ScaleState | null>(null)
  const [wizardFlow, setWizardFlow] = useState<NodeWizardFlow>('maintenance')
  const [wizardJoinProfileId, setWizardJoinProfileId] = useState<string | null>(null)

  const { canOperate, canAdmin, caps, capsLoading } = usePlatformAuth()

  const summaryQuery = useQuery({
    queryKey: ['cluster', 'summary'],
    queryFn: fetchCluster,
    refetchInterval: 30_000,
  })

  const nodesQuery = useQuery({
    queryKey: ['cluster', 'nodes'],
    queryFn: fetchClusterNodes,
    refetchInterval: 30_000,
  })

  const clusterNodes = nodesQuery.data?.nodes ?? []

  /** Merge latest probe fields (unschedulable, status) — selectedNode state alone goes stale after actuation. */
  const selectedNodeLive = useMemo(() => {
    if (selectedNode?.name == null) return null
    return clusterNodes.find(n => n.name === selectedNode.name) ?? selectedNode
  }, [clusterNodes, selectedNode])

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

  const clusterFetching =
    summaryQuery.isFetching ||
    nodesQuery.isFetching ||
    metricsQuery.isFetching ||
    observabilityQuery.isFetching

  const namespacesQuery = useQuery({
    queryKey: ['cluster', 'namespaces', nsFilter],
    queryFn: () => fetchClusterNamespaces(nsFilter === 'bifrost' ? 'bifrost' : ''),
    refetchInterval: 30_000,
  })

  const placementQuery = useQuery({
    queryKey: ['cluster', 'placement'],
    queryFn: fetchClusterPlacement,
    refetchInterval: 30_000,
  })

  const workloadsQuery = useQuery({
    queryKey: ['cluster', 'workloads', selectedNs],
    queryFn: () => fetchClusterWorkloads(selectedNs ?? 'default'),
    enabled: selectedNs != null,
    refetchInterval: 30_000,
  })

  const eventsQuery = useQuery({
    queryKey: ['cluster', 'events', selectedNs],
    queryFn: () => fetchClusterEvents(selectedNs ?? undefined, 50),
    enabled: selectedNs != null && drawerOpen,
    refetchInterval: 30_000,
  })

  const logsQuery = useQuery({
    queryKey: ['cluster', 'pod-logs', selectedNs, selectedPod],
    queryFn: () => fetchPodLogs(selectedNs ?? '', selectedPod ?? '', 200),
    enabled: selectedNs != null && selectedPod != null && drawerOpen,
    refetchInterval: 30_000,
  })

  const joinProfilesQuery = useQuery({
    queryKey: ['cluster', 'join-profiles'],
    queryFn: fetchJoinProfiles,
    refetchInterval: 60_000,
  })

  const nodePowerQuery = useQuery({
    queryKey: ['cluster', 'node-power', selectedNodeLive?.name],
    queryFn: () => fetchNodePower(selectedNodeLive?.name ?? ''),
    enabled:
      selectedNodeLive?.compute_managed === true &&
      selectedNodeLive.name != null &&
      (nodeDrawerOpen || wizardFlow !== 'join'),
    refetchInterval: 15_000,
  })

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

  const restartMutation = useMutation({
    mutationFn: rolloutRestartDeployment,
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const scaleMutation = useMutation({
    mutationFn: scaleDeployment,
    onSuccess: data => {
      setScaleState(null)
      handleActuationSuccess(data.message)
    },
    onError: handleActuationError,
  })

  const deletePodMutation = useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) => deletePod(namespace, name),
    onSuccess: data => {
      setDrawerOpen(false)
      setSelectedPod(null)
      handleActuationSuccess(data.message)
    },
    onError: handleActuationError,
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

  const selectedWorkload = useMemo(() => {
    if (selectedPod == null) return undefined
    return workloadsQuery.data?.workloads.find(w => w.name === selectedPod)
  }, [workloadsQuery.data, selectedPod])

  const podEvents = useMemo(() => {
    if (selectedPod == null) return []
    return (eventsQuery.data?.events ?? []).filter(e => e.object?.includes(selectedPod) ?? false)
  }, [eventsQuery.data, selectedPod])

  const unreachable =
    summaryQuery.data?.reachability === 'fail' &&
    (summaryQuery.data?.detail?.includes('kubeconfig') ?? false)

  function refreshCluster() {
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
  }

  function formatUpdatedAt(ms: number): string {
    if (!ms) return '—'
    return new Date(ms).toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  }

  function handleActuationSuccess(message: string) {
    setActionError(null)
    setConfirmState(null)
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    setSyncError(message)
  }

  function handleActuationError(err: Error) {
    setConfirmState(null)
    setActionError(err.message)
  }

  function handleSelectNs(name: string) {
    setSelectedNs(name)
    setSelectedPod(null)
    setDrawerOpen(false)
    setDrawerOpen(false)
    setNodeDrawerOpen(false)
    setSelectedNode(null)
  }

  function handleSelectPod(name: string) {
    setSelectedPod(name)
    setDrawerOpen(true)
    setNodeDrawerOpen(false)
    setSelectedNode(null)
  }

  function handleSelectNode(node: ClusterNode) {
    setSelectedNode(node)
    setNodeDrawerOpen(true)
    setDrawerOpen(false)
    setSelectedPod(null)
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
    const node = nodesQuery.data?.nodes.find(n => n.name === name) ?? null
    setSelectedNode(node)
    // Wizard has its own State + Procedure — keep detail panel closed unless user opens it explicitly.
    setNodeDrawerOpen(false)
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
      confirmLabel: `${verb}`,
      action: () =>
        scaleMutation.mutate({
          namespace: workload.namespace,
          kind: 'Deployment',
          name: workload.name,
          replicas,
        }),
    })
  }

  function requireConfirm(next: Omit<ConfirmState, 'open'>) {
    setActionError(null)
    setConfirmState({ ...next, open: true })
  }

  function handleEnsureMetricsServer() {
    requireConfirm({
      title: 'Install metrics-server',
      message:
        'This installs metrics-server in kube-system (Layer A). Required for live CPU/memory usage and top pods. Does not install Prometheus or Grafana (Layer B).',
      confirmLabel: 'Install metrics-server',
      action: () => metricsServerMutation.mutate(),
    })
  }

  function handleEnsureNamespaces() {
    requireConfirm({
      title: 'Ensure Bifrost namespaces',
      message: 'This creates missing Bifrost namespaces from clusters.yaml. Existing namespaces are left unchanged.',
      confirmLabel: 'Ensure namespaces',
      action: () => ensureMutation.mutate(),
    })
  }

  function handleRestartDeployment(workload: ClusterWorkload) {
    requireConfirm({
      title: 'Restart deployment',
      message: `This requests a Kubernetes rollout restart for ${workload.namespace}/${workload.name}.`,
      confirmLabel: 'Restart deployment',
      action: () =>
        restartMutation.mutate({
          namespace: workload.namespace,
          kind: 'Deployment',
          name: workload.name,
        }),
    })
  }

  function handleDeletePod(workload: ClusterWorkload) {
    requireConfirm({
      title: 'Delete pod',
      message: `This deletes pod ${workload.namespace}/${workload.name}. Its controller may create a replacement pod.`,
      confirmLabel: 'Delete pod',
      action: () => deletePodMutation.mutate({ namespace: workload.namespace, name: workload.name }),
    })
  }

  function actionPending() {
    return (
      ensureMutation.isPending ||
      metricsServerMutation.isPending ||
      restartMutation.isPending ||
      scaleMutation.isPending ||
      deletePodMutation.isPending ||
      wakeNodeMutation.isPending ||
      powerOffNodeMutation.isPending ||
      cordonNodeMutation.isPending ||
      uncordonNodeMutation.isPending ||
      drainNodeMutation.isPending ||
      joinNodeMutation.isPending
    )
  }

  const metricsOk = metricsQuery.data?.metrics_server_available === true
  const clusterSummary = summaryQuery.data
  const bifrostNamespaces = namespacesQuery.data?.filter === 'bifrost' ? namespacesQuery.data.namespaces : []
  const showBootstrapActions = clusterBootstrapNeedsActions(metricsOk, bifrostNamespaces)
  const clusterStatusLabel = clusterSummary?.label ?? 'Loading…'
  const clusterUpdatedAt =
    nodesQuery.dataUpdatedAt > 0 ? formatUpdatedAt(nodesQuery.dataUpdatedAt) : null
  const clusterAuthLabel = showBootstrapActions
    ? null
    : canOperate
      ? `${caps?.principal ?? 'operator'}${canAdmin ? ' (admin)' : ''}`
      : capsLoading
        ? null
        : 'Authenticate to actuate'

  return (
    <div
      className={`flex w-full min-w-0 flex-col gap-4${nodeDrawerOpen ? ' cluster-page-shell--node-drawer' : ''}`}
    >
      <section className="page-section panel-elevated px-4 py-2">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <p className="m-0 min-w-0 flex-1 truncate text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            <span>{clusterStatusLabel}</span>
            <span className="mx-1.5 text-[var(--muted-foreground)]/50">·</span>
            <span>{clusterFetching ? 'Refreshing…' : clusterUpdatedAt != null ? `Updated ${clusterUpdatedAt}` : '30s refresh'}</span>
            {clusterAuthLabel != null && (
              <>
                <span className="mx-1.5 text-[var(--muted-foreground)]/50">·</span>
                <span>{clusterAuthLabel}</span>
              </>
            )}
            {onOpenAudit != null && (
              <>
                <span className="mx-1.5 text-[var(--muted-foreground)]/50">·</span>
                <button type="button" className="focus-strip-link shrink-0" onClick={onOpenAudit}>
                  Audit
                </button>
              </>
            )}
          </p>
          <div className="flex shrink-0 items-center gap-2">
            <Button variant="outline" size="sm" disabled={clusterFetching} onClick={refreshCluster}>
              {clusterFetching ? 'Refreshing…' : 'Refresh'}
            </Button>
            <Button
              size="sm"
              disabled={syncMutation.isPending}
              onClick={() => {
                setSyncError(null)
                syncMutation.mutate()
              }}
            >
              {syncMutation.isPending ? 'Syncing…' : 'Sync kubeconfig'}
            </Button>
          </div>
        </div>
        {(syncError != null || syncMutation.data?.ok === true || actionError != null) && (
          <div className="mt-1 space-y-0.5">
            {syncError != null && (
              <p className="m-0 text-[var(--text-dense-meta)] lamp-fail">{syncError}</p>
            )}
            {syncMutation.data?.ok === true && (
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {syncMutation.data.message}
              </p>
            )}
            {actionError != null && (
              <p className="m-0 text-[var(--text-dense-meta)] lamp-warn">
                {actionError.includes('401') || actionError.includes('operator token required')
                  ? 'Operator token required. Set PLATFORM_OPERATOR_TOKEN for the API and VITE_PLATFORM_OPERATOR_TOKEN for the console, then restart platform.'
                  : actionError}
              </p>
            )}
          </div>
        )}
      </section>

      {showBootstrapActions && (
        <section className="page-section panel-elevated px-4 py-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="m-0 text-sm font-semibold">Bootstrap shortcuts</h2>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                One-time cluster setup — hidden once metrics-server and core Bifrost namespaces exist.
              </p>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {canOperate
                  ? `Authenticated as ${caps?.principal ?? 'operator'}${canAdmin ? ' (admin)' : ''}`
                  : capsLoading
                    ? 'Checking operator session…'
                    : 'Use Authenticate in the header to enable write actions.'}
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {!metricsOk && (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!canAdmin || metricsServerMutation.isPending}
                  onClick={handleEnsureMetricsServer}
                >
                  {metricsServerMutation.isPending ? 'Installing…' : 'Install metrics-server'}
                </Button>
              )}
              {!bifrostNamespacesReady(bifrostNamespaces) && (
                <Button size="sm" disabled={!canOperate || ensureMutation.isPending} onClick={handleEnsureNamespaces}>
                  {ensureMutation.isPending ? 'Ensuring…' : 'Ensure Bifrost namespaces'}
                </Button>
              )}
            </div>
          </div>
          {!canOperate && !capsLoading && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Dev default token: <code>platform-operator-dev</code> (operator) or{' '}
              <code>platform-admin-dev</code> (admin + metrics-server). See{' '}
              <code>config/platform-auth.yaml</code>.
            </p>
          )}
          {!metricsOk && canOperate && !canAdmin && !capsLoading && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Install metrics-server requires admin token (<code>platform-admin-dev</code>).
            </p>
          )}
        </section>
      )}

      {unreachable && (
        <section className="page-section panel-elevated px-4 py-3 lamp-warn">
          <strong>Kubeconfig required.</strong> Run from infra repo:
          <pre className="mt-2 overflow-x-auto rounded bg-[var(--background)] p-2 text-[var(--text-dense-meta)] font-mono-tabular">
            {`cd bifrost-trade-infra && make k3s-fetch-kubeconfig
export PLATFORM_KUBECONFIG=$HOME/.kube/bifrost-k3s.yaml
cd ../bifrost-platform && make start`}
          </pre>
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Or click <strong>Sync kubeconfig</strong> above (requires PLATFORM_CLUSTER_SYNC_ENABLED=1).
          </p>
        </section>
      )}

      <ClusterOverviewKpi
        summary={summaryQuery.data}
        metrics={metricsQuery.data}
        isLoading={summaryQuery.isLoading || metricsQuery.isLoading}
      />

      <ClusterNodeWizardPanel
        flow={wizardFlow}
        onFlowChange={setWizardFlow}
        nodes={nodesQuery.data?.nodes ?? []}
        selectedNodeName={selectedNodeLive?.name ?? null}
        onSelectNodeName={handleWizardSelectNodeName}
        selectedNode={selectedNodeLive}
        power={nodePowerQuery.data}
        joinProfiles={joinProfilesQuery.data}
        selectedJoinProfileId={
          wizardJoinProfileId ?? joinProfilesQuery.data?.profiles[0]?.id ?? null
        }
        onSelectJoinProfileId={setWizardJoinProfileId}
        canOperate={canOperate}
        canAdmin={canAdmin}
        actionPending={actionPending()}
        onWizardAction={handleWizardAction}
        onOpenNodeDetails={() => setNodeDrawerOpen(true)}
      />

      <ClusterNodesTable
        nodes={nodesQuery.data?.nodes ?? []}
        isLoading={nodesQuery.isLoading}
        isFetching={nodesQuery.isFetching}
        metricsAvailable={metricsQuery.data?.metrics_server_available}
        selectedNode={selectedNode?.name ?? null}
        onSelectNode={handleSelectNode}
      />

      <ClusterWorkloadsExplorer
        namespaces={namespacesQuery.data?.namespaces ?? []}
        nsFilter={nsFilter}
        selectedNs={selectedNs}
        workloads={workloadsQuery.data?.workloads ?? []}
        isLoadingNamespaces={namespacesQuery.isLoading}
        isLoadingWorkloads={workloadsQuery.isLoading}
        selectedPod={selectedPod}
        onFilterChange={filter => {
          setNsFilter(filter)
          setSelectedPod(null)
          setDrawerOpen(false)
        }}
        onSelectNs={handleSelectNs}
        onSelectPod={handleSelectPod}
        onRestartDeployment={handleRestartDeployment}
        onScaleDeployment={workload => setScaleState({ workload, replicas: 1 })}
        onDeletePod={handleDeletePod}
        placementRules={placementQuery.data?.rules}
      />

      <ClusterTopPodsTable metrics={metricsQuery.data} isLoading={metricsQuery.isLoading} />

      <ClusterObservabilityPanel
        data={observabilityQuery.data}
        isLoading={observabilityQuery.isLoading}
        onOpenStandards={onOpenStandards}
        onOpenEnvironments={onOpenEnvironments}
      />

      <ClusterDrawer
        open={drawerOpen}
        namespace={selectedNs}
        podName={selectedPod}
        workload={selectedWorkload}
        events={podEvents}
        eventsLoading={eventsQuery.isLoading}
        logs={logsQuery.data?.logs}
        logsLoading={logsQuery.isLoading}
        logsError={logsQuery.error instanceof Error ? logsQuery.error.message : null}
        onClose={() => {
          setDrawerOpen(false)
          setSelectedPod(null)
        }}
      />

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
        onScaleWorkload={
          selectedNode?.compute_managed ? handleScaleComputeWorkload : undefined
        }
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

      {scaleState != null && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4" role="presentation">
          <div className="w-full max-w-md rounded-lg border border-[var(--border)] bg-[var(--card)] p-4 shadow-xl">
            <h2 className="m-0 text-base font-semibold">Scale deployment</h2>
            <p className="mt-2 text-sm text-[var(--muted-foreground)]">
              Set replicas for {scaleState.workload.namespace}/{scaleState.workload.name}.
            </p>
            <label className="mt-3 block text-sm">
              Replicas
              <input
                className="mt-1 w-full rounded border border-[var(--border)] bg-[var(--background)] px-2 py-1 font-mono-tabular"
                type="number"
                min={0}
                max={20}
                value={scaleState.replicas}
                onChange={event =>
                  setScaleState({
                    ...scaleState,
                    replicas: Number(event.currentTarget.value),
                  })
                }
              />
            </label>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setScaleState(null)}>
                Cancel
              </Button>
              <Button
                size="sm"
                disabled={scaleMutation.isPending}
                onClick={() =>
                  scaleMutation.mutate({
                    namespace: scaleState.workload.namespace,
                    kind: 'Deployment',
                    name: scaleState.workload.name,
                    replicas: scaleState.replicas,
                  })
                }
              >
                {scaleMutation.isPending ? 'Scaling…' : 'Scale deployment'}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
