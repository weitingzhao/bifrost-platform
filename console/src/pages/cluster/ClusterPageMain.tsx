import type { Dispatch, SetStateAction } from 'react'
import type { ClusterNode, ClusterWorkload } from '@/api/clusterTypes'
import { ClusterNodeDrawer } from '@/components/cluster/ClusterNodeDrawer'
import { ClusterNodeWizardPanel } from '@/components/cluster/ClusterNodeWizardPanel'
import { ClusterWorkloadsExplorer } from '@/components/cluster/ClusterWorkloadsExplorer'
import {
  allowedNamespaceNames,
  nsFilterForNamespace,
  type NsFilterType,
} from '@/lib/cluster/namespaceCatalog'
import {
  DEFAULT_STORAGE_SERVICE,
  type StorageServiceId,
} from '@/lib/cluster/storageServiceCatalog'
import { ClusterDrawer } from '@/components/cluster/ClusterDrawer'
import { ClusterPostgresDetailPanel } from '@/components/cluster/ClusterPostgresDetailPanel'
import { ClusterApplicationsDetailPanel } from '@/components/cluster/ClusterApplicationsDetailPanel'
import { ClusterRedisDetailPanel } from '@/components/cluster/ClusterRedisDetailPanel'
import { ClusterServiceReadinessPanel } from '@/components/cluster/ClusterServiceReadinessPanel'
import { ClusterCategoryDetail } from '@/components/cluster/ClusterCategoryDetail'
import { ClusterGovernancePanel } from '@/components/cluster/ClusterGovernancePanel'
import { ClusterIssuesPanel } from '@/components/cluster/ClusterIssuesPanel'
import { ClusterFailureTriageStrip } from '@/components/cluster/ClusterFailureTriageStrip'
import { RemediationPanel } from '@/components/cluster/RemediationPanel'
import { ClusterNodesTable } from '@/components/cluster/ClusterNodesTable'
import { ClusterObservabilityPanel } from '@/components/cluster/ClusterObservabilityPanel'
import type { ClusterCategory } from '@/lib/cluster/clusterCategories'
import type { NodeWizardFlow } from '@/lib/cluster/nodeWizard'
import type { ClusterPageMutations } from './useClusterPageMutations'
import type { ClusterPageQueries } from './useClusterPageQueries'
import { ClusterPageDialogs } from './ClusterPageDialogs'

export function ClusterPageMain({
  q,
  m,
  selectedCategory,
  setCategory,
  nsFilter,
  setNsFilter,
  selectedNs,
  setSelectedNs,
  selectedStorageService,
  setSelectedStorageService,
  selectedPod,
  setSelectedPod,
  setPinnedWorkload,
  drawerOpen,
  setDrawerOpen,
  selectedNode,
  setSelectedNode,
  nodeDrawerOpen,
  setNodeDrawerOpen,
  wizardFlow,
  setWizardFlow,
  wizardJoinProfileId,
  setWizardJoinProfileId,
  canOperate,
  canAdmin,
  onOpenStandards,
  onOpenRuntimeMap,
  onOpenObservability,
  onOpenAgentDesk,
  onOpenDefects,
  onOpenServerConsole,
  handleSelectNs,
  handleSelectPod,
  handleSelectNode,
  handleWizardSelectNodeName,
  selectFirstNsInFilter,
}: {
  q: ClusterPageQueries
  m: ClusterPageMutations
  selectedCategory: ClusterCategory | null
  setCategory: (c: ClusterCategory | null) => void
  nsFilter: NsFilterType
  setNsFilter: Dispatch<SetStateAction<NsFilterType>>
  selectedNs: string | null
  setSelectedNs: Dispatch<SetStateAction<string | null>>
  selectedStorageService: StorageServiceId
  setSelectedStorageService: Dispatch<SetStateAction<StorageServiceId>>
  selectedPod: string | null
  setSelectedPod: Dispatch<SetStateAction<string | null>>
  setPinnedWorkload: Dispatch<SetStateAction<ClusterWorkload | null>>
  drawerOpen: boolean
  setDrawerOpen: Dispatch<SetStateAction<boolean>>
  selectedNode: ClusterNode | null
  setSelectedNode: Dispatch<SetStateAction<ClusterNode | null>>
  nodeDrawerOpen: boolean
  setNodeDrawerOpen: Dispatch<SetStateAction<boolean>>
  wizardFlow: NodeWizardFlow
  setWizardFlow: Dispatch<SetStateAction<NodeWizardFlow>>
  wizardJoinProfileId: string | null
  setWizardJoinProfileId: Dispatch<SetStateAction<string | null>>
  canOperate: boolean
  canAdmin: boolean
  onOpenStandards?: () => void
  onOpenRuntimeMap?: () => void
  onOpenObservability?: () => void
  onOpenAgentDesk?: (arg?: string | { prefill: string }) => void
  onOpenDefects?: () => void
  onOpenServerConsole?: () => void
  handleSelectNs: (name: string) => void
  handleSelectPod: (workload: ClusterWorkload) => void
  handleSelectNode: (node: ClusterNode) => void
  handleWizardSelectNodeName: (name: string | null) => void
  selectFirstNsInFilter: (filter: NsFilterType) => void
}) {
  return (
    <>
      <ClusterCategoryDetail
        category={selectedCategory}
        title={q.selectedCategoryTitle}
        copyState={
          selectedCategory != null && q.categoryCopyId === selectedCategory
            ? q.categoryCopyState
            : 'idle'
        }
        onCopyForLlm={
          selectedCategory != null
            ? () => {
                void q.handleCopyCategoryForLlm(
                  selectedCategory,
                  q.selectedCategoryTitle ?? selectedCategory,
                )
              }
            : undefined
        }
        applicationContent={domainId =>
          domainId === 'database' ? (
            <ClusterPostgresDetailPanel
              postgres={q.postgresStatusQuery.data}
              postgresLoading={q.postgresStatusQuery.isLoading}
              serviceReadiness={q.serviceReadinessQuery.data}
              canAdmin={canAdmin}
            />
          ) : domainId === 'redis' ? (
            <ClusterRedisDetailPanel
              redis={q.redisStatusQuery.data}
              redisLoading={q.redisStatusQuery.isLoading}
              serviceReadiness={q.serviceReadinessQuery.data}
            />
          ) : domainId === 'applications' ? (
            <ClusterApplicationsDetailPanel
              serviceReadiness={q.serviceReadinessQuery.data}
              isLoading={q.serviceReadinessQuery.isLoading}
            />
          ) : (
            <ClusterServiceReadinessPanel
              data={q.serviceReadinessQuery.data}
              isLoading={q.serviceReadinessQuery.isLoading}
              compact
              domainFilter={domainId}
            />
          )
        }
        nodesContent={
          <div className="cluster-view-panels">
            <ClusterNodeWizardPanel
              flow={wizardFlow}
              onFlowChange={setWizardFlow}
              nodes={q.nodesQuery.data?.nodes ?? []}
              selectedNodeName={q.selectedNodeLive?.name ?? null}
              onSelectNodeName={handleWizardSelectNodeName}
              selectedNode={q.selectedNodeLive}
              power={q.nodePowerQuery.data}
              joinProfiles={q.joinProfilesQuery.data}
              selectedJoinProfileId={
                wizardJoinProfileId ?? q.joinProfilesQuery.data?.profiles[0]?.id ?? null
              }
              onSelectJoinProfileId={setWizardJoinProfileId}
              canOperate={canOperate}
              canAdmin={canAdmin}
              actionPending={m.actionPending()}
              onWizardAction={m.handleWizardAction}
              onOpenNodeDetails={() => setNodeDrawerOpen(true)}
            />
            <ClusterNodesTable
              nodes={q.nodesQuery.data?.nodes ?? []}
              isLoading={q.nodesQuery.isLoading}
              isFetching={q.nodesQuery.isFetching}
              metricsAvailable={q.metricsQuery.data?.metrics_server_available}
              selectedNode={selectedNode?.name ?? null}
              onSelectNode={handleSelectNode}
            />
          </div>
        }
        workloadsContent={
          <div className="cluster-view-panels">
            <ClusterWorkloadsExplorer
              namespaces={q.visibleNamespaces}
              nsFilter={nsFilter}
              selectedNs={selectedNs}
              selectedStorageService={nsFilter === 'storage' ? selectedStorageService : null}
              workloads={q.workloadsQuery.data?.workloads ?? []}
              isLoadingNamespaces={q.namespacesQuery.isLoading}
              isLoadingWorkloads={q.workloadsQuery.isLoading}
              selectedPod={selectedPod}
              onFilterChange={filter => {
                setNsFilter(filter)
                setSelectedPod(null)
                setPinnedWorkload(null)
                setDrawerOpen(false)
                if (filter === 'storage') {
                  setSelectedStorageService(DEFAULT_STORAGE_SERVICE)
                  return
                }
                const allowed = allowedNamespaceNames(filter)
                if (allowed == null) {
                  if (selectedNs == null && q.visibleNamespaces.length > 0) {
                    setSelectedNs(q.visibleNamespaces[0]!.name)
                  }
                  return
                }
                if (selectedNs == null || !allowed.includes(selectedNs)) {
                  selectFirstNsInFilter(filter)
                }
              }}
              onSelectNs={handleSelectNs}
              onSelectStorageService={setSelectedStorageService}
              onSelectPod={handleSelectPod}
              onRestartDeployment={m.handleRestartDeployment}
              onScaleDeployment={workload => m.setScaleState({ workload, replicas: 1 })}
              onDeletePod={m.handleDeletePod}
            />
          </div>
        }
        governanceContent={
          <ClusterGovernancePanel
            data={q.governanceQuery.data}
            isLoading={q.governanceQuery.isLoading}
            compact
          />
        }
        observabilityContent={
          <ClusterObservabilityPanel
            data={q.observabilityQuery.data}
            isLoading={q.observabilityQuery.isLoading}
            onOpenStandards={onOpenStandards}
            onOpenRuntimeMap={onOpenRuntimeMap}
            onOpenObservability={onOpenObservability}
            onInstallLayerB={m.handleInstallLayerB}
            installLayerBPending={m.layerBInstallMutation.isPending}
            installLayerBDisabled={m.layerBInstallBlockedReason != null}
          />
        }
      />

      {q.clusterSummary != null && (
        <>
          <ClusterFailureTriageStrip
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
            canOperate={canOperate}
          />
          <ClusterIssuesPanel
            summary={q.clusterSummary}
            serviceReadiness={q.serviceReadinessQuery.data}
            postgresStatus={q.postgresStatusQuery.data}
            canOperate={canOperate}
            remediatePending={m.remediationStartMutation.isPending}
            activeRemediationJob={q.activeRemediationJob}
            onOpenRemediationSession={m.handleOpenRemediationSession}
            onAutoRemediate={m.handleAutoRemediate}
            onSelectPodNamespace={ns => {
              setNsFilter(nsFilterForNamespace(ns))
              handleSelectNs(ns)
              setCategory('workloads')
            }}
          />
        </>
      )}

      <ClusterDrawer
        open={drawerOpen}
        namespace={selectedNs}
        podName={selectedPod}
        workload={q.selectedWorkload}
        events={q.podEvents}
        eventsLoading={q.eventsQuery.isLoading}
        logs={q.logsQuery.data?.logs}
        logsLoading={q.logsQuery.isLoading}
        logsError={q.logsQuery.error instanceof Error ? q.logsQuery.error.message : null}
        onClose={() => {
          setDrawerOpen(false)
          setSelectedPod(null)
          setPinnedWorkload(null)
        }}
      />

      <ClusterNodeDrawer
        open={nodeDrawerOpen}
        node={q.selectedNodeLive}
        power={q.nodePowerQuery.data}
        powerLoading={q.nodePowerQuery.isLoading}
        powerError={q.nodePowerQuery.error instanceof Error ? q.nodePowerQuery.error.message : null}
        canOperate={canOperate}
        canAdmin={canAdmin}
        actionPending={m.actionPending()}
        onClose={() => {
          setNodeDrawerOpen(false)
          setSelectedNode(null)
        }}
        onCordon={m.handleCordonNode}
        onUncordon={m.handleUncordonNode}
        onDrain={m.handleDrainNode}
        onWake={selectedNode?.compute_managed ? m.handleWakeComputeNode : undefined}
        onPowerOff={selectedNode?.compute_managed ? m.handlePowerOffComputeNode : undefined}
        onScaleWorkload={
          selectedNode?.compute_managed ? m.handleScaleComputeWorkload : undefined
        }
      />

      <RemediationPanel
        open={m.remediationPanelOpen}
        jobId={m.remediationJobId}
        initialJob={m.remediationJob}
        stopping={m.remediationCancelMutation.isPending}
        onStop={id => m.remediationCancelMutation.mutate(id)}
        onComplete={m.handleRemediationComplete}
        onOpenServerConsole={onOpenServerConsole}
        onClose={() => {
          m.setRemediationPanelOpen(false)
        }}
      />

      <ClusterPageDialogs
        confirmState={m.confirmState}
        onCancelConfirm={() => m.setConfirmState(null)}
        actionPending={m.actionPending()}
        scaleState={m.scaleState}
        setScaleState={m.setScaleState}
        scaleMutation={m.scaleMutation}
      />
    </>
  )
}
