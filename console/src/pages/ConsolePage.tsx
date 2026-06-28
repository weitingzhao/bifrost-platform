import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, PageHeader, PageShell, SidebarInset, SidebarProvider, TooltipProvider } from '@bifrost/ui'
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import type { MatrixResponse } from '@/api/types'
import {
  fetchAudit,
  fetchCluster,
  fetchContext,
  fetchEnvironments,
  fetchGitOpsApps,
  fetchStackAddons,
  fetchDeliveryPipelines,
  fetchMatrix,
  fetchPlatformHealth,
  fetchStgSmoke,
  fetchReleaseGate,
  fetchSupplyChain,
  fetchTierBStatus,
  fetchTopology,
  isAllMatrices,
} from '@/api/platform'
import { consoleNavPlane } from '@/lib/consoleNavConfig'
import { isPipelineRunSucceeded } from '@/lib/delivery/pipelineRunAskPack'
import type { OpenRuntimeMapFn, RuntimeMapNavigateOptions } from '@/lib/runtime-map/runtimeMapNavigation'
import { EnvironmentStrip, type EnvFilter } from '@/components/EnvironmentStrip'
import { FocusStrip } from '@/components/FocusStrip'
import { PlatformAuthBar } from '@/components/PlatformAuthBar'
import { ConsoleHeader, OpsContextBar } from '@/components/ConsoleHeader'
import { ConsoleSidebar, type ConsoleViewTab } from '@/components/ConsoleSidebar'
import { buildFullArchitectureLlmPack } from '@/lib/architecture/buildArchitectureLlmPack'
import { AgentDeskPage } from '@/pages/AgentDeskPage'
import { AgentProtocolPage } from '@/pages/AgentProtocolPage'
import { AuditPage } from '@/pages/AuditPage'
import { BlueprintPage } from '@/pages/BlueprintPage'
import { BriefingPage } from '@/pages/BriefingPage'
import { ClusterPage } from '@/pages/ClusterPage'
import { DeliveryPage } from '@/pages/DeliveryPage'
import { DeployMainlinePage } from '@/pages/DeployMainlinePage'
import { EnvironmentsPage } from '@/pages/EnvironmentsPage'
import { PlacementPage } from '@/pages/PlacementPage'
import { PlatformReleasePage } from '@/pages/PlatformReleasePage'
import { ProgramPage } from '@/pages/ProgramPage'
import { PromotePage } from '@/pages/PromotePage'
import { RuntimeMapPage } from '@/pages/RuntimeMapPage'
import { ServerConsolePage } from '@/pages/ServerConsolePage'
import { DesignSystemPage } from '@/pages/DesignSystemPage'
import { K3sArchitecturePage } from '@/pages/K3sArchitecturePage'
import { K3sBootstrapPage } from '@/pages/K3sBootstrapPage'
import { CicdBootstrapPage } from '@/pages/CicdBootstrapPage'
import { RoadmapPage } from '@/pages/RoadmapPage'
import { DataLayerPage } from '@/pages/DataLayerPage'
import { DualFlywheelVisionPage } from '@/pages/DualFlywheelVisionPage'
import { McpContractPage } from '@/pages/McpContractPage'
import { NetworkUpgradePage } from '@/pages/NetworkUpgradePage'
import { OperatorPlanePage } from '@/pages/OperatorPlanePage'
import { AutonomousSkillsPage } from '@/pages/AutonomousSkillsPage'
import { ExecutionLogPage } from '@/pages/ExecutionLogPage'
import { AgentGovernancePage } from '@/pages/AgentGovernancePage'
import { DefectsPage } from '@/pages/DefectsPage'
import { StandardsPage } from '@/pages/StandardsPage'

const ControlRoomPage = lazy(() =>
  import('@/pages/ControlRoomPage').then(m => ({ default: m.ControlRoomPage })),
)

const VIEW_TITLES: Record<ConsoleViewTab, string> = {
  'agent-desk': 'Agent Desk',
  briefing: 'Agent Briefing',
  'autonomous-skills': 'Skills & Schedules',
  'execution-log': 'Execution Log',
  'agent-governance': 'Trust & Autonomy',
  'operator-plane': 'Operator Plane',
  'control-room': 'Control Room',
  audit: 'Audit',
  'runtime-map': 'Runtime Map',
  cluster: 'Cluster',
  placement: 'Placement',
  delivery: 'Delivery',
  program: 'Milestones',
  promote: 'Promote',
  blueprint: 'Blueprint',
  'flywheel-vision': 'Vision',
  environments: 'Environments',
  roadmap: 'Roadmap',
  'k3s-architecture': 'K3s Architecture',
  'k3s-bootstrap': 'K3s Bootstrap',
  'cicd-bootstrap': 'CI/CD Bootstrap',
  'data-layer': 'Data Layer',
  'deploy-mainline': 'Deploy Mainline',
  'platform-release': 'Platform Release',
  'platform-standards': 'Platform',
  'agent-protocol': 'Agent Protocol',
  'mcp-contract': 'MCP Contract',
  'design-system': 'Design System',
  'network-upgrade': 'Network Upgrade',
  console: 'Server console',
  defects: 'Defects',
}

const OPS_CONTEXT_TABS: ConsoleViewTab[] = [
  'agent-desk',
  'autonomous-skills',
  'execution-log',
  'agent-governance',
  'operator-plane',
  'audit',
  'briefing',
  'console',
  'control-room',
  'promote',
  'delivery',
  'platform-release',
  'cluster',
  'placement',
  'runtime-map',
  'program',
  'deploy-mainline',
]

const LEGACY_RUNTIME_HASHES: Record<string, ConsoleViewTab> = {
  topology: 'runtime-map',
  matrix: 'runtime-map',
  pulse: 'control-room',
}

function isConsoleViewTab(value: string): value is ConsoleViewTab {
  return Object.prototype.hasOwnProperty.call(VIEW_TITLES, value)
}

/** Resolve the active tab from the URL hash so refresh/deep-link stays put. */
function tabFromHash(): ConsoleViewTab | null {
  const hash = window.location.hash.replace(/^#/, '')
  if (!hash) return null
  if (isConsoleViewTab(hash)) return hash
  return LEGACY_RUNTIME_HASHES[hash] ?? null
}

export function ConsolePage() {
  const [envFilter, setEnvFilter] = useState<EnvFilter>('prod')
  const [viewTab, setViewTabState] = useState<ConsoleViewTab>(() => tabFromHash() ?? 'control-room')
  const [agentDeskJobId, setAgentDeskJobId] = useState<string | null>(null)
  const [agentDeskPrefill, setAgentDeskPrefill] = useState<string | null>(null)
  const [runtimeMapFocus, setRuntimeMapFocus] = useState<RuntimeMapNavigateOptions | null>(null)
  const qc = useQueryClient()

  const envForRuntime = envFilter === 'all' ? 'prod' : envFilter

  const setViewTab = useCallback((tab: ConsoleViewTab) => {
    setViewTabState(tab)
    const nextHash = `#${tab}`
    if (window.location.hash !== nextHash) {
      window.history.replaceState(null, '', nextHash)
    }
  }, [])

  useEffect(() => {
    const onHashChange = () => {
      const t = tabFromHash()
      if (t != null) setViewTabState(t)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  const contextQuery = useQuery({
    queryKey: ['context'],
    queryFn: fetchContext,
    staleTime: 60_000,
  })

  const envQuery = useQuery({
    queryKey: ['environments'],
    queryFn: fetchEnvironments,
  })

  const healthQuery = useQuery({
    queryKey: ['platform-health'],
    queryFn: fetchPlatformHealth,
    refetchInterval: 15_000,
  })

  const matrixForPulse = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    refetchInterval: 30_000,
    enabled:
      viewTab === 'briefing' ||
      viewTab === 'control-room' ||
      viewTab === 'promote' ||
      viewTab === 'delivery',
  })

  const clusterQuery = useQuery({
    queryKey: ['cluster', 'summary'],
    queryFn: fetchCluster,
    refetchInterval: 30_000,
    enabled:
      viewTab === 'briefing' ||
      viewTab === 'cluster' ||
      viewTab === 'placement' ||
      viewTab === 'control-room' ||
      viewTab === 'runtime-map' ||
      viewTab === 'delivery',
  })

  const gitopsQuery = useQuery({
    queryKey: ['gitops', 'apps'],
    queryFn: fetchGitOpsApps,
    refetchInterval: 30_000,
    enabled: viewTab === 'delivery',
  })

  const stackQuery = useQuery({
    queryKey: ['stack', 'addons'],
    queryFn: fetchStackAddons,
    refetchInterval: 30_000,
    enabled: viewTab === 'delivery',
  })

  const pipelinesQuery = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 30_000,
    enabled: viewTab === 'delivery' || viewTab === 'placement',
  })

  const stgSmokeQuery = useQuery({
    queryKey: ['delivery', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 30_000,
    enabled: viewTab === 'delivery' || viewTab === 'control-room',
  })

  const releaseGateStgQuery = useQuery({
    queryKey: ['promote', 'release-gate', 'stg'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 30_000,
    enabled: viewTab === 'promote' || viewTab === 'delivery',
  })

  const releaseGateProdQuery = useQuery({
    queryKey: ['promote', 'release-gate', 'prod'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: 30_000,
    enabled: viewTab === 'promote',
  })

  const supplyChainQuery = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 30_000,
    enabled: viewTab === 'delivery' || viewTab === 'promote',
  })

  const tierBQuery = useQuery({
    queryKey: ['promote', 'tier-b'],
    queryFn: fetchTierBStatus,
    refetchInterval: 30_000,
    enabled: viewTab === 'delivery' || viewTab === 'promote',
  })

  const lastDeliverSucceeded = useMemo(() => {
    const run = supplyChainQuery.data?.last_deliver_success
    return run != null && isPipelineRunSucceeded(run)
  }, [supplyChainQuery.data?.last_deliver_success])

  const auditQuery = useQuery({
    queryKey: ['platform', 'audit'],
    queryFn: fetchAudit,
    refetchInterval: 30_000,
    enabled: viewTab === 'briefing' || viewTab === 'audit',
  })

  const auditRecords = auditQuery.data?.records ?? []

  const runtimeMatrixQuery = useQuery({
    queryKey: ['matrix', envForRuntime],
    queryFn: () => fetchMatrix(envForRuntime),
    refetchInterval: 30_000,
    enabled: viewTab === 'runtime-map',
  })

  const topologyQuery = useQuery({
    queryKey: ['topology', envForRuntime],
    queryFn: () => fetchTopology(envForRuntime),
    refetchInterval: 30_000,
    enabled: viewTab === 'runtime-map',
  })

  const pulseMatrices = useMemo((): MatrixResponse[] => {
    const data = matrixForPulse.data
    if (!data) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixForPulse.data])

  const runtimeMatrix = useMemo((): MatrixResponse | undefined => {
    const data = runtimeMatrixQuery.data
    if (!data) return undefined
    if (isAllMatrices(data)) return data.matrices[0]
    return data
  }, [runtimeMatrixQuery.data])


  function refreshAll() {
    void qc.invalidateQueries({ queryKey: ['matrix'] })
    void qc.invalidateQueries({ queryKey: ['topology'] })
    void qc.invalidateQueries({ queryKey: ['platform-health'] })
    void qc.invalidateQueries({ queryKey: ['context'] })
    void qc.invalidateQueries({ queryKey: ['cluster'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
  }

  const openProgram = () => setViewTab('program')
  const openDelivery = () => setViewTab('delivery')
  const openPromote = () => setViewTab('promote')
  const openDeployMainline = () => setViewTab('deploy-mainline')
  const clearRuntimeMapFocus = useCallback(() => setRuntimeMapFocus(null), [])

  const openRuntimeMap: OpenRuntimeMapFn = useCallback((options) => {
    if (options?.env) {
      setEnvFilter(options.env)
    } else if (envFilter === 'all') {
      setEnvFilter('prod')
    }
    setRuntimeMapFocus(options ?? null)
    setViewTab('runtime-map')
  }, [envFilter])
  const openCluster = () => setViewTab('cluster')
  const openPlacement = () => setViewTab('placement')
  const openAudit = () => setViewTab('audit')
  const openBriefing = () => setViewTab('briefing')
  const openOperatorPlane = () => setViewTab('operator-plane')
  const openAgentDesk = useCallback((jobIdOrOpts?: string | { prefill: string }) => {
    if (typeof jobIdOrOpts === 'string') {
      setAgentDeskJobId(jobIdOrOpts)
    } else if (jobIdOrOpts != null && 'prefill' in jobIdOrOpts) {
      setAgentDeskPrefill(jobIdOrOpts.prefill)
    }
    setViewTab('agent-desk')
  }, [])
  const openBlueprint = () => setViewTab('blueprint')
  const openStandards = () => setViewTab('platform-standards')
  const openEnvironments = () => setViewTab('environments')

  const [govCopyState, setGovCopyState] = useState<'idle' | 'copied' | 'error'>('idle')
  const isArchTab =
    viewTab === 'blueprint' ||
    viewTab === 'flywheel-vision' ||
    viewTab === 'environments' ||
    viewTab === 'roadmap' ||
    viewTab === 'k3s-architecture' ||
    viewTab === 'k3s-bootstrap' ||
    viewTab === 'cicd-bootstrap' ||
    viewTab === 'data-layer' ||
    viewTab === 'network-upgrade'
  const isStdTab = viewTab === 'platform-standards' || viewTab === 'agent-protocol' || viewTab === 'mcp-contract' || viewTab === 'design-system'
  const isGovernanceTab = isArchTab || isStdTab
  const handleCopyAllGovernance = async () => {
    let spine = contextQuery.data
    if (spine == null) {
      try { spine = await fetchContext() } catch { /* static only */ }
    }
    const text = buildFullArchitectureLlmPack(spine)
    try {
      await navigator.clipboard.writeText(text)
      setGovCopyState('copied')
      window.setTimeout(() => setGovCopyState('idle'), 2000)
    } catch {
      setGovCopyState('error')
      window.setTimeout(() => setGovCopyState('idle'), 3000)
    }
  }


  const showEnvStrip = viewTab === 'runtime-map'
  const showPageHeader = ![
    'agent-desk',
    'briefing',
    'autonomous-skills',
    'execution-log',
    'agent-governance',
    'operator-plane',
    'control-room',
    'runtime-map',
    'cluster',
    'delivery',
    'program',
    'promote',
    'deploy-mainline',
    'blueprint',
    'environments',
    'platform-standards',
    'agent-protocol',
    'mcp-contract',
    'data-layer',
    'design-system',
    'console',
    'platform-release',
    'defects',
  ].includes(viewTab)

  const runtimeLoading = topologyQuery.isLoading || runtimeMatrixQuery.isLoading
  const runtimeError =
    (topologyQuery.error as Error | null) ?? (runtimeMatrixQuery.error as Error | null)

  return (
    <TooltipProvider>
    <SidebarProvider>
      <ConsoleSidebar
        activeTab={viewTab}
        onSelect={(id) => setViewTab(id as ConsoleViewTab)}
      />
      <SidebarInset>
        <div className="sticky top-0 z-20 bg-card">
          <ConsoleHeader
            title={VIEW_TITLES[viewTab]}
            plane={consoleNavPlane(viewTab)}
            healthy={healthQuery.data}
            onRefresh={refreshAll}
          >
            <PlatformAuthBar compact hideRefresh />
          </ConsoleHeader>
          {OPS_CONTEXT_TABS.includes(viewTab) && (
            <OpsContextBar>
              <FocusStrip
                onNavigate={tab => setViewTab(tab as ConsoleViewTab)}
                onOpenAgentDeskWithPrefill={prefill => openAgentDesk({ prefill })}
              />
            </OpsContextBar>
          )}
        </div>
      <PageShell padding="compact" className="flex w-full min-w-0 flex-col gap-4">
        {showPageHeader && (
          <PageHeader
            title={VIEW_TITLES[viewTab]}
            description="L0 read-only probes — collapse the sidebar to use full width."
          />
        )}

        {envQuery.data && showEnvStrip && (
          <EnvironmentStrip
            environments={envQuery.data}
            selected={envFilter}
            onSelect={id => {
              setEnvFilter(id)
            }}
          />
        )}

        {viewTab === 'agent-desk' && (
          <AgentDeskPage
            context={contextQuery.data}
            initialJobId={agentDeskJobId}
            prefillPrompt={agentDeskPrefill}
            onInitialJobConsumed={() => setAgentDeskJobId(null)}
            onPrefillConsumed={() => setAgentDeskPrefill(null)}
            onOpenBriefing={openBriefing}
            onOpenCluster={openCluster}
            onOpenMcpContract={() => setViewTab('mcp-contract')}
            onOpenAgentProtocol={() => setViewTab('agent-protocol')}
            onOpenOperatorPlane={openOperatorPlane}
          />
        )}

        {viewTab === 'operator-plane' && (
          <OperatorPlanePage
            onOpenMcpContract={() => setViewTab('mcp-contract')}
            onOpenBriefing={openBriefing}
          />
        )}

        {viewTab === 'autonomous-skills' && <AutonomousSkillsPage />}

        {viewTab === 'execution-log' && <ExecutionLogPage />}

        {viewTab === 'agent-governance' && <AgentGovernancePage />}

        {viewTab === 'briefing' && (
          <>
            <PageHeader
              title={VIEW_TITLES.briefing}
              description="Pick work intent, review UI progress, generate a full context pack for a new Cursor Agent session."
            />
            <BriefingPage
              context={contextQuery.data}
              contextLoading={contextQuery.isLoading}
              matrices={pulseMatrices}
              matrixLoading={matrixForPulse.isLoading}
              clusterSummary={clusterQuery.data}
              clusterLoading={clusterQuery.isLoading}
              platformHealthy={healthQuery.data}
              auditRecords={auditRecords}
              auditLoading={auditQuery.isLoading}
              onOpenAgentDesk={openAgentDesk}
            />
          </>
        )}

        {viewTab === 'control-room' && (
          <>
            <Suspense fallback={<p className="text-[var(--muted-foreground)]">Loading mission control…</p>}>
              <ControlRoomPage
                context={contextQuery.data}
                contextLoading={contextQuery.isLoading}
                matrices={pulseMatrices}
                matrixLoading={matrixForPulse.isLoading}
                matrixError={matrixForPulse.error as Error | null}
                platformHealthy={healthQuery.data === true}
                clusterSummary={clusterQuery.data}
                clusterLoading={clusterQuery.isLoading}
                stgSmoke={stgSmokeQuery.data}
                stgSmokeLoading={stgSmokeQuery.isLoading}
                onOpenRuntimeMap={openRuntimeMap}
                onOpenProgram={openProgram}
                onOpenDelivery={openDelivery}
                onOpenCluster={openCluster}
                onOpenAudit={openAudit}
                onOpenBriefing={openBriefing}
                onOpenAgentDesk={(opts) => openAgentDesk(opts)}
                onOpenPlatformRelease={() => setViewTab('platform-release')}
              />
            </Suspense>
          </>
        )}

        {viewTab === 'audit' && (
          <>
            <PageHeader
              title={VIEW_TITLES.audit}
              description="Canonical actuation history for platform-api — GitOps sync, cluster operations, and other operator actions."
            />
            <AuditPage records={auditRecords} isLoading={auditQuery.isLoading} />
          </>
        )}

        {viewTab === 'defects' && <DefectsPage />}

        {viewTab === 'runtime-map' && (
          <>
            <PageHeader
              title={VIEW_TITLES['runtime-map']}
              description="Hardware topology and SCOPE stack — per-environment drill-down, gap analysis, and runtime-scoped Agent packs."
            />
            {envFilter === 'all' && (
              <p className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Runtime Map uses a single environment — showing Production. Select Dev or Prod.
              </p>
            )}
            <RuntimeMapPage
              topology={topologyQuery.data}
              matrix={runtimeMatrix}
              context={contextQuery.data}
              clusterSummary={clusterQuery.data}
              isLoading={runtimeLoading}
              error={runtimeError}
              initialFocus={runtimeMapFocus}
              onInitialFocusConsumed={clearRuntimeMapFocus}
              onOpenCluster={openCluster}
            />
          </>
        )}

        {viewTab === 'cluster' && (
          <>
            <PageHeader
              title={VIEW_TITLES.cluster}
              description="K3s nodes, namespaces, workloads, and platform-api actuation (join, power, rollout)."
            />
            <ClusterPage
              onOpenStandards={openStandards}
              onOpenEnvironments={openEnvironments}
              onOpenAudit={openAudit}
              onOpenServerConsole={() => setViewTab('console')}
              onOpenAgentDesk={openAgentDesk}
            />
          </>
        )}

        {viewTab === 'placement' && (
          <>
            <PageHeader
              title={VIEW_TITLES.placement}
              description="Node pools, scheduling policy, and CI readiness — live cluster vs planned topology."
            />
            <PlacementPage onOpenDelivery={openDelivery} />
          </>
        )}

        {viewTab === 'delivery' && (
          <>
            <PageHeader
              title={VIEW_TITLES.delivery}
              description="STG CI/CD — Operate (deliver), Observe (health & history), Blueprint (workflow & gates)."
            />
            <DeliveryPage
              context={contextQuery.data}
              matrices={pulseMatrices}
              clusterSummary={clusterQuery.data}
              gitops={gitopsQuery.data}
              gitopsLoading={gitopsQuery.isLoading}
              gitopsError={gitopsQuery.error instanceof Error ? gitopsQuery.error.message : null}
              stack={stackQuery.data}
              stackLoading={stackQuery.isLoading}
              stackError={stackQuery.error instanceof Error ? stackQuery.error.message : null}
              pipelines={pipelinesQuery.data}
              pipelinesLoading={pipelinesQuery.isLoading}
              pipelinesError={
                pipelinesQuery.error instanceof Error ? pipelinesQuery.error.message : null
              }
              stgSmoke={stgSmokeQuery.data}
              stgSmokeLoading={stgSmokeQuery.isLoading}
              stgSmokeFetching={stgSmokeQuery.isFetching}
              stgSmokeError={
                stgSmokeQuery.error instanceof Error ? stgSmokeQuery.error.message : null
              }
              lastDeliverSucceeded={lastDeliverSucceeded}
              stgGate={releaseGateStgQuery.data}
              tierB={tierBQuery.data}
              tierBLoading={tierBQuery.isLoading}
              onRefreshStgSmoke={() => void stgSmokeQuery.refetch()}
              isLoading={contextQuery.isLoading || matrixForPulse.isLoading}
              onOpenMilestones={openProgram}
              onOpenPromote={openPromote}
              onOpenAudit={openAudit}
              onOpenPlacement={openPlacement}
              onOpenDeployMainline={openDeployMainline}
            />
          </>
        )}

        {viewTab === 'program' && (
          <>
            <PageHeader
              title={VIEW_TITLES.program}
              description="NOW — Live progress: active milestones, owner decisions, and deployment status from ops-context spine."
            />
            <ProgramPage
              context={contextQuery.data}
              isLoading={contextQuery.isLoading}
              error={contextQuery.error as Error | null}
              onOpenBlueprint={openBlueprint}
            />
          </>
        )}

        {viewTab === 'promote' && (
          <>
            <PageHeader
              title={VIEW_TITLES.promote}
              description="STG release gate + Tier B vs Prod cutover gate — flywheel A/B checklists."
            />
            <PromotePage
              context={contextQuery.data}
              matrices={pulseMatrices}
              stgGate={releaseGateStgQuery.data}
              stgGateLoading={releaseGateStgQuery.isLoading}
              stgGateError={
                releaseGateStgQuery.error instanceof Error ? releaseGateStgQuery.error.message : null
              }
              prodGate={releaseGateProdQuery.data}
              prodGateLoading={releaseGateProdQuery.isLoading}
              prodGateError={
                releaseGateProdQuery.error instanceof Error ? releaseGateProdQuery.error.message : null
              }
              stgSmoke={stgSmokeQuery.data}
              lastDeliverSucceeded={lastDeliverSucceeded}
              tierB={tierBQuery.data}
              isLoading={contextQuery.isLoading || matrixForPulse.isLoading}
              onOpenProgram={openProgram}
              onOpenDelivery={openDelivery}
              onOpenDeployMainline={openDeployMainline}
            />
          </>
        )}

        {viewTab === 'deploy-mainline' && (
          <>
            <PageHeader
              title={VIEW_TITLES['deploy-mainline']}
              description="Local Prod Final → K3s → Compose → Legacy retirement — deployment decision chain and sign-off gates."
            />
            <DeployMainlinePage />
          </>
        )}

        {viewTab === 'platform-release' && (
          <>
            <PageHeader
              title={VIEW_TITLES['platform-release']}
              description="End-to-end Platform CI/CD — follow the flow: Staging deploy → gate → Production deploy → gate."
            />
            <PlatformReleasePage />
          </>
        )}

        {viewTab === 'console' && <ServerConsolePage />}

        {isGovernanceTab && (
          <div className="flex items-center justify-between gap-3">
            <PageHeader
              title={VIEW_TITLES[viewTab]}
              description={
                viewTab === 'flywheel-vision' ? 'WHERE — Ultimate destination: Trade + Ops converge into unified AI-native experience via three-layer Agents.'
                  : viewTab === 'blueprint' ? 'HOW — Architectural principles, control-plane strategy, authorization model, and design rules toward the Vision.'
                  : viewTab === 'roadmap' ? 'WHEN — Phased execution plan: hardware roles, K3s stages, GitOps migration, AI ops timeline.'
                  : viewTab === 'environments' ? 'WHAT — Concrete probe targets, IPs, ports, and connectivity for each environment.'
                  : viewTab === 'k3s-architecture' ? 'Target K3s topology, CNPG, GitOps, AI-native ops, and living checkpoints.'
                  : viewTab === 'k3s-bootstrap' ? 'First-node deployment runbook, verification checklist, node join steps, and sign-off.'
                  : viewTab === 'cicd-bootstrap' ? 'L0/L1/L2 self-hosting bootstrap model — CI/CD rules, recovery paths, and P6 gap tracking.'
                  : viewTab === 'data-layer' ? 'Redis, PostgreSQL, MinIO — stateful service architecture, HA topology, and data responsibility split.'
                  : viewTab === 'network-upgrade' ? 'Home network backbone upgrade — VLAN redesign, UniFi migration plan, hardware BOM, and research checklist.'
                  : viewTab === 'platform-standards' ? 'Trade stack probe contract, cluster actuation phases, and API route inventory.'
                  : viewTab === 'agent-protocol' ? 'Agent interaction modes, three-layer architecture, context pack layers, and forbidden actions.'
                  : viewTab === 'mcp-contract'
                    ? 'MCP tool catalog, Cursor setup, and governance contract (permissions, deny-list).'
                  : 'Dense UI layer stack, mandatory mapping, business semantic colors, and primitives inventory.'
              }
            />
            <Button
              variant="ghost"
              size="xs"
              className="shrink-0"
              onClick={() => void handleCopyAllGovernance()}
            >
              {govCopyState === 'copied' ? 'All copied!' : govCopyState === 'error' ? 'Copy failed' : 'Copy All for LLM'}
            </Button>
          </div>
        )}

        {viewTab === 'blueprint' && <BlueprintPage context={contextQuery.data} />}

        {viewTab === 'flywheel-vision' && <DualFlywheelVisionPage />}

        {viewTab === 'environments' && (
          <EnvironmentsPage
            context={contextQuery.data}
            onOpenRuntimeMap={openRuntimeMap}
            onOpenDelivery={openDelivery}
          />
        )}

        {viewTab === 'roadmap' && <RoadmapPage />}

        {viewTab === 'k3s-architecture' && <K3sArchitecturePage onOpenPlacement={openPlacement} />}

        {viewTab === 'k3s-bootstrap' && <K3sBootstrapPage />}

        {viewTab === 'cicd-bootstrap' && <CicdBootstrapPage />}

        {viewTab === 'data-layer' && <DataLayerPage />}

        {viewTab === 'network-upgrade' && <NetworkUpgradePage />}

        {viewTab === 'platform-standards' && <StandardsPage />}

        {viewTab === 'agent-protocol' && <AgentProtocolPage />}

        {viewTab === 'mcp-contract' && <McpContractPage />}

        {viewTab === 'design-system' && <DesignSystemPage />}
      </PageShell>
      </SidebarInset>
    </SidebarProvider>
    </TooltipProvider>
  )
}
