import { useCallback, useEffect, useState } from 'react'
import type { Reachability } from '@/api/matrixTypes'
import type { ClusterNode, ClusterWorkload } from '@/api/clusterTypes'
import { ClusterCategoryGrid } from '@/components/cluster/ClusterCategoryGrid'
import { ClusterOpsIssuesPanel } from '@/components/cluster/ClusterOpsIssuesPanel'
import { ClusterOverviewKpi } from '@/components/cluster/ClusterOverviewKpi'
import { ClusterTopPodsTable } from '@/components/cluster/ClusterTopPodsTable'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import type { NodeWizardFlow } from '@/lib/cluster/nodeWizard'
import { useClusterCategory } from '@/hooks/useClusterCategory'
import {
  allowedNamespaceNames,
  nsFilterForNamespace,
  type NsFilterType,
} from '@/lib/cluster/namespaceCatalog'
import {
  DEFAULT_STORAGE_SERVICE,
  type StorageServiceId,
} from '@/lib/cluster/storageServiceCatalog'
import { OpsSection } from '@/components/layout/OpsSection'
import { ClusterPageChrome } from '@/pages/cluster/ClusterPageChrome'
import { ClusterPageMain } from '@/pages/cluster/ClusterPageMain'
import { useClusterPageMutations } from '@/pages/cluster/useClusterPageMutations'
import { useClusterPageQueries } from '@/pages/cluster/useClusterPageQueries'

export function ClusterPage({
  onOpenStandards,
  onOpenRuntimeMap,
  onOpenAudit,
  onOpenServerConsole,
  onOpenAgentDesk,
  onOpenDefects,
  onOpenObservability,
  onOpenDelivery,
  ambientJobId,
  onStartAgentJob,
  onExpandAgentDock,
  onSelectAgentJob,
}: {
  onOpenStandards?: () => void
  onOpenRuntimeMap?: () => void
  onOpenAudit?: () => void
  onOpenServerConsole?: () => void
  onOpenAgentDesk?: (arg?: string | { prefill: string }) => void
  onOpenDefects?: () => void
  onOpenObservability?: () => void
  onOpenDelivery?: () => void
  ambientJobId?: string | null
  onStartAgentJob?: (job: AmbientAgentJob) => void
  onExpandAgentDock?: () => void
  onSelectAgentJob?: (job: AmbientAgentJob) => void
}) {
  const [nsFilter, setNsFilter] = useState<NsFilterType>('trade')
  const [selectedNs, setSelectedNs] = useState<string | null>('bifrost-stg')
  const [selectedStorageService, setSelectedStorageService] =
    useState<StorageServiceId>(DEFAULT_STORAGE_SERVICE)
  const [selectedPod, setSelectedPod] = useState<string | null>(null)
  const [pinnedWorkload, setPinnedWorkload] = useState<ClusterWorkload | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [selectedNode, setSelectedNode] = useState<ClusterNode | null>(null)
  const [nodeDrawerOpen, setNodeDrawerOpen] = useState(false)
  const [wizardFlow, setWizardFlow] = useState<NodeWizardFlow>('maintenance')
  const [wizardJoinProfileId, setWizardJoinProfileId] = useState<string | null>(null)
  const [opsHealth, setOpsHealth] = useState<{
    reach: Reachability
    summaryLine: string
    needsFix: boolean
  } | null>(null)
  const { category: selectedCategory, setCategory, toggleCategory } = useClusterCategory()
  const { canOperate, canAdmin, caps, capsLoading } = usePlatformAuth()

  const handleOpsHealthChange = useCallback(
    (health: { reach: Reachability; summaryLine: string; needsFix: boolean }) => {
      setOpsHealth(health)
    },
    [],
  )

  const q = useClusterPageQueries({
    selectedNs,
    selectedPod,
    drawerOpen,
    selectedNode,
    nodeDrawerOpen,
    wizardFlow,
    pinnedWorkload,
    selectedCategory,
  })

  const m = useClusterPageMutations({
    selectedNode,
    wizardJoinProfileId,
    joinProfiles: q.joinProfilesQuery.data,
    canAdmin,
    observability: q.observabilityQuery.data,
    clusterSummary: q.clusterSummary,
    serviceReadiness: q.serviceReadinessQuery.data,
    governance: q.governanceQuery.data,
    postgresStatus: q.postgresStatusQuery.data,
    queries: q,
    selectedNs,
    onOpenAgentDesk,
    onStartAgentJob,
    onExpandAgentDock,
    onSelectAgentJob,
    setDrawerOpen,
    setSelectedPod,
  })

  // Follow ambient Agent job on this page (do not discard the shell prop).
  // Dock owns the session UI — only track job id locally; never open RemediationPanel.
  useEffect(() => {
    if (ambientJobId == null || ambientJobId === '') return
    m.followAmbientRemediationJob(ambientJobId)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- follow once per ambient id
  }, [ambientJobId])

  const clusterAuthLabel = q.showBootstrapActions
    ? null
    : canOperate
      ? `${caps?.principal ?? 'operator'}${canAdmin ? ' (admin)' : ''}`
      : capsLoading
        ? null
        : 'Authenticate to actuate'

  function handleSelectNs(name: string) {
    setSelectedNs(name)
    setSelectedPod(null)
    setPinnedWorkload(null)
    setDrawerOpen(false)
    setDrawerOpen(false)
    setNodeDrawerOpen(false)
    setSelectedNode(null)
  }

  function handleSelectPod(workload: ClusterWorkload) {
    setSelectedNs(workload.namespace)
    setSelectedPod(workload.name)
    setPinnedWorkload(workload)
    setDrawerOpen(true)
    setNodeDrawerOpen(false)
    setSelectedNode(null)
  }

  /** Top-pods row → Workloads category + pod drawer (same detail surface). */
  function handleSelectTopPod(pod: { namespace: string; name: string }) {
    setNsFilter(nsFilterForNamespace(pod.namespace))
    setCategory('workloads')
    handleSelectPod({
      namespace: pod.namespace,
      kind: 'Pod',
      name: pod.name,
      ready: '—',
      status: 'Running',
      restarts: 0,
      age: '—',
      reachability: 'ok',
    })
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
    const node = q.nodesQuery.data?.nodes.find(n => n.name === name) ?? null
    setSelectedNode(node)
    setNodeDrawerOpen(false)
  }

  function selectFirstNsInFilter(filter: NsFilterType) {
    const allowed = allowedNamespaceNames(filter)
    const pool =
      allowed == null
        ? q.visibleNamespaces
        : q.visibleNamespaces.filter(ns => allowed.includes(ns.name))
    setSelectedNs(pool[0]?.name ?? null)
  }

  const sidePanelOpen = nodeDrawerOpen || m.remediationPanelOpen

  return (
    <div
      className={`cluster-page-shell flex w-full min-w-0 flex-col gap-2${sidePanelOpen ? ' cluster-page-shell--node-drawer' : ''}${m.remediationPanelOpen ? ' cluster-page-shell--remediation-drawer' : ''}`}
    >
      <ClusterPageChrome
        clusterStatusLabel={q.clusterStatusLabel}
        clusterFetching={q.clusterFetching}
        clusterUpdatedAt={q.clusterUpdatedAt}
        clusterAuthLabel={clusterAuthLabel}
        onOpenAudit={onOpenAudit}
        copyState={q.copyState}
        onCopyForLlm={() => void q.handleCopyForLlm()}
        onRefresh={q.refreshCluster}
        syncPending={m.syncMutation.isPending}
        onSyncKubeconfig={() => {
          m.setSyncError(null)
          m.syncMutation.mutate()
        }}
        syncError={m.syncError}
        syncOkMessage={
          m.syncMutation.data?.ok === true ? m.syncMutation.data.message : undefined
        }
        actionError={m.actionError}
        actionSuccess={m.actionSuccess}
        summaryError={q.clusterSummaryError}
        opsReach={opsHealth?.reach}
        opsSummaryLine={opsHealth?.summaryLine}
        ambientJobId={ambientJobId}
        onOpenAgentDesk={onOpenAgentDesk}
        onExpandAgentDock={onExpandAgentDock}
        showBootstrapActions={q.showBootstrapActions}
        canOperate={canOperate}
        canAdmin={canAdmin}
        caps={caps}
        capsLoading={capsLoading}
        metricsOk={q.metricsOk}
        bifrostNsReady={q.bifrostNsReady}
        metricsServerPending={m.metricsServerMutation.isPending}
        ensurePending={m.ensureMutation.isPending}
        onEnsureMetricsServer={m.handleEnsureMetricsServer}
        onEnsureNamespaces={m.handleEnsureNamespaces}
        unreachable={q.unreachable}
        clusterSummary={q.clusterSummary}
        summaryFailed={q.clusterSummaryError != null}
        isProbing={q.summaryQuery.isPending && q.clusterSummary == null}
        healthBody={
          q.clusterSummary != null ? (
            <ClusterOpsIssuesPanel
              embedded
              summary={q.clusterSummary}
              serviceReadiness={q.serviceReadinessQuery.data}
              postgresStatus={q.postgresStatusQuery.data}
              onOpenAgentDesk={opts => onOpenAgentDesk?.(opts)}
              onOpenDefects={onOpenDefects}
              onPlaybookFix={({ scope, prompt }) => {
                if (!canOperate) return
                m.playbookFixMutation.mutate({ scope, prompt })
              }}
              playbookFixPending={m.playbookFixMutation.isPending}
              onAutoCheck={m.handleAutoRemediate}
              autoCheckPending={m.remediationStartMutation.isPending}
              canOperate={canOperate}
              activeRemediationJob={q.activeRemediationJob}
              onOpenRemediationSession={m.handleOpenRemediationSession}
              onHealthChange={handleOpsHealthChange}
              autoAssess
              onSelectPodNamespace={ns => {
                setNsFilter(nsFilterForNamespace(ns))
                handleSelectNs(ns)
                setCategory('workloads')
              }}
            />
          ) : null
        }
      />

      <div className="cluster-overview-row">
        <div className="cluster-overview-row__kpi">
          <ClusterOverviewKpi
            summary={q.summaryQuery.data}
            metrics={q.metricsQuery.data}
            isLoading={q.summaryQuery.isLoading || q.metricsQuery.isLoading}
          />
        </div>
        <div className="cluster-overview-row__pods">
          <ClusterTopPodsTable
            metrics={q.metricsQuery.data}
            isLoading={q.metricsQuery.isLoading}
            selectedPodKey={
              selectedNs != null && selectedPod != null ? `${selectedNs}/${selectedPod}` : null
            }
            onSelectPod={handleSelectTopPod}
          />
        </div>
      </div>

      <OpsSection
        className="cluster-home-summaries"
        title="Categories"
        description="Pick a dimension to expand tags · then open a category detail"
        bodyPadding="compact"
        overflow="visible"
      >
        <ClusterCategoryGrid
          summary={q.clusterSummary}
          summaryLoading={q.summaryQuery.isLoading}
          serviceReadiness={q.serviceReadinessQuery.data}
          serviceReadinessLoading={q.serviceReadinessQuery.isLoading}
          governance={q.governanceQuery.data}
          governanceLoading={q.governanceQuery.isLoading}
          observability={q.observabilityQuery.data}
          observabilityLoading={q.observabilityQuery.isLoading}
          placement={q.placementQuery.data}
          placementLoading={q.placementQuery.isLoading}
          metrics={q.metricsQuery.data}
          selectedCategory={selectedCategory}
          onSelectCategory={toggleCategory}
          categoryCopyId={q.categoryCopyId}
          categoryCopyState={q.categoryCopyState}
          onCopyCategory={q.handleCopyCategoryForLlm}
        />
      </OpsSection>

      <ClusterPageMain
        q={q}
        m={m}
        selectedCategory={selectedCategory}
        setCategory={setCategory}
        nsFilter={nsFilter}
        setNsFilter={setNsFilter}
        selectedNs={selectedNs}
        setSelectedNs={setSelectedNs}
        selectedStorageService={selectedStorageService}
        setSelectedStorageService={setSelectedStorageService}
        selectedPod={selectedPod}
        setSelectedPod={setSelectedPod}
        setPinnedWorkload={setPinnedWorkload}
        drawerOpen={drawerOpen}
        setDrawerOpen={setDrawerOpen}
        selectedNode={selectedNode}
        setSelectedNode={setSelectedNode}
        nodeDrawerOpen={nodeDrawerOpen}
        setNodeDrawerOpen={setNodeDrawerOpen}
        wizardFlow={wizardFlow}
        setWizardFlow={setWizardFlow}
        wizardJoinProfileId={wizardJoinProfileId}
        setWizardJoinProfileId={setWizardJoinProfileId}
        canOperate={canOperate}
        canAdmin={canAdmin}
        onOpenStandards={onOpenStandards}
        onOpenRuntimeMap={onOpenRuntimeMap}
        onOpenObservability={onOpenObservability}
        onOpenDelivery={onOpenDelivery}
        onOpenServerConsole={onOpenServerConsole}
        handleSelectNs={handleSelectNs}
        handleSelectPod={handleSelectPod}
        handleSelectNode={handleSelectNode}
        handleWizardSelectNodeName={handleWizardSelectNodeName}
        selectFirstNsInFilter={selectFirstNsInFilter}
      />
    </div>
  )
}
