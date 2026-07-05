import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, PageHeader, PageShell, SidebarInset, SidebarProvider, TooltipProvider } from '@bifrost/ui'
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import type { MatrixResponse, RemediationJob } from '@/api/types'
import { AgentJobBanner } from '@/components/agent/AgentJobBanner'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import {
  fetchAudit,
  fetchCluster,
  fetchContext,
  fetchEnvironments,
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
import { BriefingReconciliationPage } from '@/pages/BriefingReconciliationPage'
import { AgentSystemPage } from '@/pages/AgentSystemPage'
import { AuditPage } from '@/pages/AuditPage'
import { BlueprintPage } from '@/pages/BlueprintPage'
import { BriefingPage } from '@/pages/BriefingPage'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import { writeBriefingUrlState } from '@/lib/briefing/briefingUrlState'
import { ClusterPage } from '@/pages/ClusterPage'
import { ObservabilityPage } from '@/pages/ObservabilityPage'
import { ComputePage } from '@/pages/ComputePage'
import { DeliveryBoardPage } from '@/pages/DeliveryBoardPage'
import { NetworkPage } from '@/pages/NetworkPage'
import { PluginGalleryPage } from '@/pages/PluginGalleryPage'
import { SatelliteApiHealthPage } from '@/pages/SatelliteApiHealthPage'
import { SatelliteBusPage } from '@/pages/SatelliteBusPage'
import { SatelliteTelemetryPage } from '@/pages/SatelliteTelemetryPage'
import { PlacementPage } from '@/pages/PlacementPage'
import { PlatformReleasePage } from '@/pages/PlatformReleasePage'
import { TradeReleasePage } from '@/pages/TradeReleasePage'
import { RuntimeMapPage } from '@/pages/RuntimeMapPage'
import { ServerConsolePage } from '@/pages/ServerConsolePage'
import { DesignSystemPage } from '@/pages/DesignSystemPage'
import { RoadmapPage } from '@/pages/RoadmapPage'
import { DualFlywheelVisionPage } from '@/pages/DualFlywheelVisionPage'
import { McpContractPage } from '@/pages/McpContractPage'
import { AiComputeStrategyPage } from '@/pages/AiComputeStrategyPage'
import { OperatorPlanePage } from '@/pages/OperatorPlanePage'
import { AutonomousSkillsPage } from '@/pages/AutonomousSkillsPage'
import { ExecutionLogPage } from '@/pages/ExecutionLogPage'
import { AgentGovernancePage } from '@/pages/AgentGovernancePage'
import { DefectsPage } from '@/pages/DefectsPage'
import { DevAgentPage } from '@/pages/DevAgentPage'
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
  'agent-system': 'Agent System',
  'operator-plane': 'Operator Plane',
  'control-room': 'Control Room',
  audit: 'Audit',
  'runtime-map': 'Runtime Map',
  cluster: 'Cluster',
  observability: 'Observability',
  placement: 'Placement',
  'trade-release': 'Trade Release',
  'delivery-board': 'Delivery Board',
  blueprint: 'Blueprint',
  'flywheel-vision': 'Vision',
  roadmap: 'Roadmap',
  'platform-release': 'Platform Release',
  'platform-standards': 'Platform',
  'agent-protocol': 'Agent Protocol',
  'briefing-reconciliation': 'Briefing Reconciliation',
  'mcp-contract': 'MCP Contract',
  'design-system': 'Design System',
  'ai-compute': 'AI Compute Strategy',
  'dev-agent': 'Dev Agent',
  console: 'Server console',
  network: 'Network',
  compute: 'Compute',
  'satellite-bus': 'Bus Status',
  'satellite-telemetry': 'Telemetry',
  'satellite-api': 'API Health',
  'plugin-gallery': 'Plugin Gallery',
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
  'network',
  'compute',
  'satellite-bus',
  'satellite-telemetry',
  'satellite-api',
  'plugin-gallery',
  'control-room',
  'delivery-board',
  'trade-release',
  'platform-release',
  'cluster',
  'placement',
  'runtime-map',
  'observability',
]

const LEGACY_RUNTIME_HASHES: Record<string, ConsoleViewTab> = {
  topology: 'runtime-map',
  matrix: 'runtime-map',
  pulse: 'control-room',
  delivery: 'trade-release',
  promote: 'trade-release',
  program: 'control-room',
  'deploy-mainline': 'control-room',
  environments: 'control-room',
  'k3s-architecture': 'control-room',
  'k3s-bootstrap': 'control-room',
  'cicd-bootstrap': 'control-room',
  'data-layer': 'control-room',
  'network-upgrade': 'network',
  'network-api': 'network',
  'ib-gateway-plugin': 'control-room',
  'trade-ib-client-migration': 'control-room',
  'cluster-observability': 'observability',
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
  /** Shell-level ambient agent job — survives tab switches. */
  const [ambientJob, setAmbientJob] = useState<AmbientAgentJob | null>(null)
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
      viewTab === 'trade-release' ||
      viewTab === 'satellite-bus' ||
      viewTab === 'satellite-telemetry' ||
      viewTab === 'satellite-api',
  })

  const clusterQuery = useQuery({
    queryKey: ['cluster', 'summary'],
    queryFn: fetchCluster,
    refetchInterval: 30_000,
    enabled:
      viewTab === 'briefing' ||
      viewTab === 'cluster' ||
      viewTab === 'compute' ||
      viewTab === 'placement' ||
      viewTab === 'control-room' ||
      viewTab === 'runtime-map' ||
      viewTab === 'satellite-bus',
  })

  const stgSmokeQuery = useQuery({
    queryKey: ['delivery', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 30_000,
    enabled: viewTab === 'trade-release' || viewTab === 'control-room',
  })

  const releaseGateStgQuery = useQuery({
    queryKey: ['promote', 'release-gate', 'stg'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 30_000,
    enabled: viewTab === 'control-room',
  })

  const supplyChainQuery = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 30_000,
    enabled: viewTab === 'control-room',
  })

  const tierBQuery = useQuery({
    queryKey: ['promote', 'tier-b'],
    queryFn: fetchTierBStatus,
    refetchInterval: 30_000,
    enabled: viewTab === 'control-room',
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

  const openTradeRelease = () => setViewTab('trade-release')
  const openDelivery = openTradeRelease
  const openPromote = openTradeRelease
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
  const openObservability = () => setViewTab('observability')
  const openNetwork = () => setViewTab('network')
  const openSatelliteApi = () => setViewTab('satellite-api')
  const openSatelliteTelemetry = () => setViewTab('satellite-telemetry')
  const openPluginGallery = () => setViewTab('plugin-gallery')
  const openPlacement = () => setViewTab('placement')
  const openAudit = () => setViewTab('audit')
  const openBriefing = useCallback((opts?: BriefingUrlState) => {
    if (opts != null) {
      if (opts.track != null && opts.lane == null) {
        writeBriefingUrlState({ track: opts.track, lane: undefined, intent: undefined })
      } else {
        writeBriefingUrlState(opts)
      }
    }
    setViewTab('briefing')
  }, [])
  const openOperatorPlane = () => setViewTab('operator-plane')
  const openAgentDesk = useCallback((jobIdOrOpts?: string | { prefill: string }) => {
    if (typeof jobIdOrOpts === 'string') {
      setAgentDeskJobId(jobIdOrOpts)
    } else if (jobIdOrOpts != null && 'prefill' in jobIdOrOpts) {
      setAgentDeskPrefill(jobIdOrOpts.prefill)
    }
    setViewTab('agent-desk')
  }, [])

  const startAmbientAgentJob = useCallback((job: AmbientAgentJob) => {
    setAmbientJob(job)
  }, [])

  const handleAmbientJobComplete = useCallback(
    (_job: RemediationJob) => {
      void qc.invalidateQueries({ queryKey: ['agent', 'bridge'] })
      void qc.invalidateQueries({ queryKey: ['promote', 'release-state'] })
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs'] })
      void qc.invalidateQueries({ queryKey: ['promote', 'release-gate'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'self-health'] })
    },
    [qc],
  )
  const openStandards = () => setViewTab('platform-standards')

  const [govCopyState, setGovCopyState] = useState<'idle' | 'copied' | 'error'>('idle')
  const isGovernanceTab = consoleNavPlane(viewTab) === 'Governance'
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
    'dev-agent',
    'control-room',
    'runtime-map',
    'cluster',
    'observability',
    'trade-release',
    'blueprint',
    'platform-standards',
    'agent-protocol',
    'briefing-reconciliation',
    'mcp-contract',
    'design-system',
    'console',
    'network',
    'compute',
    'satellite-bus',
    'satellite-telemetry',
    'satellite-api',
    'plugin-gallery',
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
        <div className="console-shell-chrome sticky top-0 z-20 bg-card">
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
          {ambientJob != null && (
            <div className="console-shell-chrome__ambient-agent" role="region" aria-label="Active agent task">
              <AgentJobBanner
                jobId={ambientJob.id}
                taskLabel={ambientJob.label}
                onDismiss={() => setAmbientJob(null)}
                onOpenAgentDesk={id => openAgentDesk(id)}
                onComplete={handleAmbientJobComplete}
              />
            </div>
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
            onOpenAgentSystem={() => setViewTab('agent-system')}
            onOpenOperatorPlane={openOperatorPlane}
          />
        )}

        {viewTab === 'operator-plane' && (
          <OperatorPlanePage
            onOpenMcpContract={() => setViewTab('mcp-contract')}
            onOpenBriefing={openBriefing}
            ambientJobId={ambientJob?.id ?? null}
            onStartAgentJob={startAmbientAgentJob}
          />
        )}

        {viewTab === 'autonomous-skills' && <AutonomousSkillsPage />}

        {viewTab === 'dev-agent' && <DevAgentPage />}

        {viewTab === 'execution-log' && <ExecutionLogPage />}

        {viewTab === 'agent-governance' && <AgentGovernancePage />}

        {viewTab === 'briefing' && (
          <>
            <PageHeader
              title={VIEW_TITLES.briefing}
              description="Pick track/lane, copy pack into Cursor IDE (primary). Verify Phases 1–4 below — Phase 4 closes the roadmap program."
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
              onOpenAudit={openAudit}
              onOpenTrustAutonomy={() => setViewTab('agent-governance')}
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
                stgGate={releaseGateStgQuery.data}
                lastDeliverSucceeded={lastDeliverSucceeded}
                tierB={tierBQuery.data}
                onOpenRuntimeMap={openRuntimeMap}
                onOpenDelivery={openDelivery}
                onOpenCluster={openCluster}
                onOpenAudit={openAudit}
                onOpenBriefing={openBriefing}
                onOpenAgentDesk={(opts) => openAgentDesk(opts)}
                ambientJobId={ambientJob?.id ?? null}
                onStartAgentJob={startAmbientAgentJob}
                onOpenPlatformRelease={() => setViewTab('platform-release')}
                onOpenPromote={openPromote}
                onOpenAgentProtocol={() => setViewTab('agent-protocol')}
                onOpenNetwork={openNetwork}
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
              onOpenRuntimeMap={() => openRuntimeMap()}
              onOpenObservability={openObservability}
              onOpenAudit={openAudit}
              onOpenServerConsole={() => setViewTab('console')}
              onOpenAgentDesk={openAgentDesk}
            />
          </>
        )}

        {viewTab === 'observability' && (
          <ObservabilityPage
            onOpenCluster={openCluster}
            onOpenStandards={openStandards}
            onOpenRuntimeMap={() => openRuntimeMap()}
          />
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

        {viewTab === 'delivery-board' && (
          <>
            <PageHeader
              title={VIEW_TITLES['delivery-board']}
              description="Console delivery programs — phased sign-off checklists consolidated from functional pages."
            />
            <DeliveryBoardPage />
          </>
        )}

        {viewTab === 'trade-release' && (
          <>
            <PageHeader
              title={VIEW_TITLES['trade-release']}
              description="End-to-end Trade CI/CD — STG deploy → gate → PROD deploy → gate."
            />
            <TradeReleasePage
              context={contextQuery.data}
              isLoading={contextQuery.isLoading}
              onOpenPlacement={openPlacement}
            />
          </>
        )}

        {viewTab === 'platform-release' && (
          <>
            <PageHeader
              title={VIEW_TITLES['platform-release']}
              description="End-to-end Platform CI/CD — follow the flow: Staging deploy → gate → Production deploy → gate."
            />
            <PlatformReleasePage
              ambientJobId={ambientJob?.id ?? null}
              onStartAgentJob={startAmbientAgentJob}
            />
          </>
        )}

        {viewTab === 'console' && <ServerConsolePage />}

        {viewTab === 'network' && (
          <NetworkPage
            context={contextQuery.data}
            onOpenAgentProtocol={() => setViewTab('agent-protocol')}
          />
        )}

        {viewTab === 'compute' && (
          <ComputePage onOpenCluster={openCluster} onOpenAudit={openAudit} />
        )}

        {viewTab === 'satellite-bus' && (
          <SatelliteBusPage
            onOpenObservability={openObservability}
            onOpenTelemetry={openSatelliteTelemetry}
            onOpenPluginGallery={openPluginGallery}
            onOpenApiHealth={openSatelliteApi}
          />
        )}

        {viewTab === 'satellite-api' && <SatelliteApiHealthPage />}

        {viewTab === 'satellite-telemetry' && (
          <SatelliteTelemetryPage onOpenObservability={openObservability} />
        )}

        {viewTab === 'plugin-gallery' && <PluginGalleryPage />}

        {isGovernanceTab && (
          <div className="flex items-center justify-between gap-3">
            <PageHeader
              title={VIEW_TITLES[viewTab]}
              description={
                viewTab === 'flywheel-vision' ? 'WHERE — Ultimate destination: Trade + Ops converge into unified AI-native experience via three-layer Agents.'
                  : viewTab === 'blueprint' ? 'HOW — Architectural principles, control-plane strategy, authorization model, and design rules toward the Vision.'
                  : viewTab === 'roadmap' ? 'WHEN — Phased execution plan: hardware roles, K3s stages, GitOps migration, AI ops timeline.'
                  : viewTab === 'ai-compute' ? 'AI compute layer — tiered model sourcing, inference hardware trade-offs, quantization sweet spots, and demand-driven purchase signals.'
                  : viewTab === 'platform-standards' ? 'Trade stack probe contract, cluster actuation phases, and API route inventory.'
                  : viewTab === 'agent-system'
                    ? 'Single runtime, capability domains, task chains, and registry — the map before Agent Protocol and MCP Contract.'
                  : viewTab === 'agent-protocol' ? 'Agent interaction modes, three-layer architecture, context pack layers, and forbidden actions.'
                  : viewTab === 'briefing-reconciliation'
                    ? 'Spine projection discipline — source of truth layers, reconcile gate (BRIEFING_STALE), Sync vs Health, drift layer map.'
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

        {viewTab === 'roadmap' && <RoadmapPage />}

        {viewTab === 'ai-compute' && <AiComputeStrategyPage />}

        {viewTab === 'platform-standards' && <StandardsPage />}

        {viewTab === 'agent-system' && (
          <AgentSystemPage
            onOpenDoctrine={tab => setViewTab(tab === 'mcp-contract' ? 'mcp-contract' : 'agent-protocol')}
          />
        )}

        {viewTab === 'agent-protocol' && <AgentProtocolPage />}

        {viewTab === 'briefing-reconciliation' && (
          <BriefingReconciliationPage context={contextQuery.data} />
        )}

        {viewTab === 'mcp-contract' && <McpContractPage />}

        {viewTab === 'design-system' && <DesignSystemPage />}
      </PageShell>
      </SidebarInset>
    </SidebarProvider>
    </TooltipProvider>
  )
}
