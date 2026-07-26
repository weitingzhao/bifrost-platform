import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, PageShell, SidebarInset, SidebarProvider, TooltipProvider } from '@bifrost/ui'
import { lazy, Suspense, useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { RemediationJob } from '@/api/remediationTypes'
import {
  AgentExecutionDock,
  persistOperatorTool,
  readStoredTool,
  type OperatorToolId,
} from '@/components/agent/AgentExecutionDock'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useLaneCatalog } from '@/hooks/useLaneCatalog'
import { useAgentTaskCatalog } from '@/hooks/useAgentTaskCatalog'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import {
  isOpenAgentDeskFocusDecisionBriefs,
  isOpenAgentDeskFocusHandoff,
  isOpenAgentDeskPrefill,
  type OpenAgentDeskArg,
} from '@/lib/agent/openAgentDesk'
import { fetchAudit, fetchCluster } from '@/api/cluster'
import { fetchContext, fetchEnvironments, fetchMatrix, fetchPlatformHealth, fetchTopology, isAllMatrices } from '@/api/core'
import { fetchStgSmoke, fetchReleaseGate, fetchTierBStatus } from '@/api/promote'
import { fetchSupplyChain } from '@/api/delivery'
import { consoleNavPlane } from '@/lib/consoleNavConfig'
import { scrollToSection } from '@/lib/dom/scrollToSection'
import { BackToMissionLaunchButton } from '@/components/delivery/LaneDetailShell'
import { LANE_DETAIL_SUBTITLE } from '@/lib/delivery/laneDetailContext'
import { isPipelineRunSucceeded } from '@/lib/delivery/pipelineRunAskPack'
import type { OpenRuntimeMapFn, RuntimeMapNavigateOptions } from '@/lib/runtime-map/runtimeMapNavigation'
import { type EnvFilter } from '@/components/EnvironmentStrip'
import { ConsoleHeader } from '@/components/ConsoleHeader'
import { OpsContextStrip } from '@/components/OpsContextStrip'
import { ConsoleSidebar, type ConsoleViewTab } from '@/components/ConsoleSidebar'
import { GuidesSettingsNav } from '@/components/GuidesSettingsNav'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { buildFullArchitectureLlmPack } from '@/lib/architecture/buildArchitectureLlmPack'
import { AgentDeskPage } from '@/pages/AgentDeskPage'
import { AgentCapabilityPage } from '@/pages/AgentCapabilityPage'
import { AgentProtocolPage } from '@/pages/AgentProtocolPage'
import { BriefingReconciliationPage } from '@/pages/BriefingReconciliationPage'
import { AgentSystemPage } from '@/pages/AgentSystemPage'
import { AuditPage } from '@/pages/AuditPage'
import { BlueprintPage } from '@/pages/BlueprintPage'
import { BriefingPage } from '@/pages/BriefingPage'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import { writeBriefingUrlState } from '@/lib/briefing/briefingUrlState'
import { componentLineForTaskMode, trackTypeForTaskMode } from '@/lib/briefing/briefingViewTabs'
import { ClusterPage } from '@/pages/ClusterPage'
import { ComputePage } from '@/pages/ComputePage'
import { DeliveryBoardPage } from '@/pages/DeliveryBoardPage'
import { NetworkPage } from '@/pages/NetworkPage'
import { PluginGalleryPage } from '@/pages/PluginGalleryPage'
import { SatelliteApiHealthPage } from '@/pages/SatelliteApiHealthPage'
import { SatelliteBusPage } from '@/pages/SatelliteBusPage'
import { ObservabilityPage } from '@/pages/ObservabilityPage'
import { SatelliteTelemetryPage } from '@/pages/SatelliteTelemetryPage'
import { PlacementPage } from '@/pages/PlacementPage'
import { PlatformReleasePage } from '@/pages/PlatformReleasePage'
import { TradeReleasePage } from '@/pages/TradeReleasePage'
import { ControlRoomRuntimeMapSheet } from '@/components/control-room/ControlRoomRuntimeMapSheet'
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
import { TaskControlCenterPage } from '@/pages/TaskControlCenterPage'
import { TaskModeProvider, useTaskMode } from '@/lib/task-mode/TaskModeContext'
import { formatConsoleHash } from '@/lib/task-mode/taskModeUrl'
import type { TaskModeId } from '@/lib/task-mode/types'

const ControlRoomPage = lazy(() =>
  import('@/pages/ControlRoomPage').then(m => ({ default: m.ControlRoomPage })),
)

const VIEW_TITLES: Record<ConsoleViewTab, string> = {
  'agent-desk': 'Agent Desk',
  'agent-capability': 'Agent Capability',
  briefing: 'Agent Briefing',
  'autonomous-skills': 'Skills & Schedules',
  'execution-log': 'Execution Log',
  'agent-governance': 'Trust & Autonomy',
  'agent-system': 'Agent System',
  'operator-plane': 'Operator Plane',
  'control-room': 'Control Room',
  observability: 'Observability',
  'task-cc': 'Task Control Center',
  audit: 'Audit',
  'runtime-map': 'Runtime Map',
  cluster: 'Cluster',
  placement: 'Placement',
  'trade-release': 'Deploy Satellite',
  'delivery-board': 'Delivery Board',
  blueprint: 'Blueprint',
  'flywheel-vision': 'Vision',
  roadmap: 'Roadmap',
  'platform-release': 'Launch Rocket',
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
  'satellite-telemetry': 'Satellite Runtime',
  'satellite-api': 'API & Auth Probes',
  'plugin-gallery': 'Plugin Gallery',
  defects: 'Defects',
}

/** Page help — shown on breadcrumb ? tooltip (system-wide; no in-page PageHeader subtitle). */
const VIEW_DESCRIPTIONS: Partial<Record<ConsoleViewTab, string>> = {
  briefing: 'Plan and start work — pick scope and lane, open Session.',
  'delivery-board': 'Phased sign-off checklists — verify and close delivery programs.',
  'agent-desk': 'Operate and observe — run agent tasks, review remediation, close sessions.',
  'agent-capability':
    'Live capability readiness — which agent scopes are ready, running, awaiting approval, or failed.',
  'control-room':
    'Mission diagnosis — payload reachability, rocket/satellite launches, agent loop, and command intent. Topology opens as a drill-down sheet.',
  observability: 'Apollo-domain system health hub — Grafana is deep evidence, not a second control plane.',
  'task-cc': 'Task-mode cockpit — checklist, fleet, and launch evidence for the active lens.',
  audit:
    'Canonical actuation history for platform-api — GitOps, cluster, remediation/Agent lifecycle, and other operator writes.',
  defects:
    'Cross-job Agent remediation pattern analysis (history debt) — not live health or Launch GO|NO-GO.',
  cluster: 'K3s nodes, namespaces, workloads, and platform-api actuation (join, power, rollout).',
  placement:
    'Fleet facility constraints — node pools and scheduling policy for Rocket CI, Satellite STG, and shared infra.',
  'trade-release': LANE_DETAIL_SUBTITLE,
  'platform-release': LANE_DETAIL_SUBTITLE,
  console:
    'Legacy hash — opens shell Operator Dock Console slot (SSH). Prefer Operator Dock Agent | Console.',
  network: 'Ground floor LAN / UniFi — live UCG probe, firewall drift audit, devices, and clients.',
  compute:
    'Physical ground systems — K3s nodes, Wake-on-LAN, join profiles, cordon/drain. Workload detail lives on Rocket → Cluster.',
  'satellite-bus':
    'Bus health for the selected Trade namespace — shared dependencies (Platform IB Gateway → redis-ib).',
  'satellite-telemetry':
    'Trade-namespace golden signals via platform-api preset PromQL proxy. System-wide health → Observability.',
  'satellite-api':
    'Per-environment matrix probes for Trade satellite endpoints — HTTP reachability, ops auth, and D10 blocked writes.',
  'plugin-gallery':
    'External subcontractor plugins — live L0 probes and L1 actuation for platform-managed integrations.',
  'operator-plane':
    'Out-of-band recovery layer — AI Remediation Runners outside K8s on dual Mac Minis (fate isolation D7 / L-1).',
  'flywheel-vision':
    'WHERE — Ultimate destination: Trade + Ops converge into unified AI-native experience via three-layer Agents.',
  blueprint:
    'HOW — Architectural principles, control-plane strategy, authorization model, and design rules toward the Vision.',
  roadmap: 'WHEN — Phased execution plan: hardware roles, K3s stages, GitOps migration, AI ops timeline.',
  'ai-compute':
    'AI compute layer — tiered model sourcing, inference hardware trade-offs, quantization sweet spots, and demand-driven purchase signals.',
  'platform-standards': 'Trade stack probe contract, cluster actuation phases, and API route inventory.',
  'agent-system':
    'Single runtime, capability domains, task chains, and registry — the map before Agent Protocol and MCP Contract.',
  'agent-protocol':
    'Agent interaction modes, three-layer architecture, context pack layers, and forbidden actions.',
  'briefing-reconciliation':
    'Spine projection discipline — source of truth layers, reconcile gate (BRIEFING_STALE), Sync vs Health, drift layer map.',
  'mcp-contract': 'MCP tool catalog, Cursor setup, and governance contract (permissions, deny-list).',
  'design-system':
    'Dense UI layer stack, mandatory mapping, business semantic colors, and primitives inventory.',
  'autonomous-skills':
    'Hermes Skills and Schedules — trigger types, actuation levels, and cron/webhook inventory.',
  'execution-log':
    'Hermes autonomous execution history — every Skill run, trigger, duration, and outcome.',
  'agent-governance':
    'Trust matrix and autonomy policy — which skills may auto-act vs require confirmation.',
  'runtime-map':
    'Live topology of rocket, satellite, and agent paths — hardware, software stack, and gap analysis.',
  'dev-agent':
    'Guided multi-phase Dev Agent jobs — select program, run phases, approve or reject with feedback.',
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
  'observability',
  'task-cc',
  'delivery-board',
  'trade-release',
  'platform-release',
  'cluster',
  'placement',
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
  'cluster-observability': 'cluster',
  telemetry: 'satellite-telemetry',
}

function isConsoleViewTab(value: string): value is ConsoleViewTab {
  return Object.prototype.hasOwnProperty.call(VIEW_TITLES, value)
}

/** Resolve the active tab from the URL hash so refresh/deep-link stays put. */
function tabFromHash(): ConsoleViewTab | null {
  const raw = window.location.hash.replace(/^#/, '')
  if (!raw) return null
  const tabPart = raw.split('?')[0]
  if (isConsoleViewTab(tabPart)) return tabPart
  return LEGACY_RUNTIME_HASHES[tabPart] ?? null
}

function ConsolePageInner() {
  const [envFilter, setEnvFilter] = useState<EnvFilter>('prod')
  const [viewTab, setViewTabState] = useState<ConsoleViewTab>(() => tabFromHash() ?? 'control-room')
  const { modeId, setModeId } = useTaskMode()
  const [agentDeskJobId, setAgentDeskJobId] = useState<string | null>(null)
  const [agentDeskPrefill, setAgentDeskPrefill] = useState<string | null>(null)
  const [agentDeskFocusHandoffId, setAgentDeskFocusHandoffId] = useState<string | null>(null)
  const [agentDeskFocusDecisionBriefs, setAgentDeskFocusDecisionBriefs] = useState(false)
  /** Shell-level ambient agent job — survives tab switches. */
  const [ambientJob, setAmbientJob] = useState<AmbientAgentJob | null>(null)
  /** Operator Dock expanded (working/maximized); collapsed when false. */
  const [dockExpanded, setDockExpanded] = useState(false)
  /** Operator Dock tool slot — Agent | Console; restore last slot across refresh. */
  const [operatorToolId, setOperatorToolIdState] = useState<OperatorToolId>(readStoredTool)
  const setOperatorToolId = useCallback((tool: OperatorToolId) => {
    persistOperatorTool(tool)
    setOperatorToolIdState(tool)
  }, [])
  const [runtimeMapFocus, setRuntimeMapFocus] = useState<RuntimeMapNavigateOptions | null>(null)
  const [runtimeMapSheetOpen, setRuntimeMapSheetOpen] = useState(false)
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const { viewerEnv, viewerEnvLoading } = useFleetSnapshot()

  const envForRuntime = envFilter === 'all' ? 'prod' : envFilter

  const setViewTab = useCallback(
    (tab: ConsoleViewTab, opts?: { taskMode?: TaskModeId }) => {
      setViewTabState(tab)
      const taskMode =
        opts?.taskMode ?? (tab === 'task-cc' && modeId !== 'system' ? modeId : undefined)
      const nextHash = formatConsoleHash(tab, taskMode)
      if (window.location.hash !== nextHash) {
        window.history.replaceState(null, '', nextHash)
      }
    },
    [modeId],
  )

  useEffect(() => {
    const onHashChange = () => {
      const t = tabFromHash()
      if (t != null) setViewTabState(t)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  useEffect(() => {
    if (viewTab !== 'task-cc' || modeId === 'system') return
    const expected = formatConsoleHash('task-cc', modeId)
    if (window.location.hash !== expected) {
      window.history.replaceState(null, '', expected)
    }
  }, [viewTab, modeId])

  /** Legacy hash / deep-link `#runtime-map` → Control Room + topology sheet. */
  useEffect(() => {
    if (viewTab !== 'runtime-map') return
    setRuntimeMapSheetOpen(true)
    setViewTab('control-room')
  }, [viewTab, setViewTab])

  const contextQuery = useQuery({
    queryKey: ['context'],
    queryFn: fetchContext,
    staleTime: 60_000,
  })

  useLaneCatalog()
  useAgentTaskCatalog()

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
      viewTab === 'task-cc' ||
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
      viewTab === 'task-cc' ||
      runtimeMapSheetOpen ||
      viewTab === 'satellite-bus',
  })

  const stgSmokeQuery = useQuery({
    queryKey: ['delivery', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 30_000,
    enabled: viewTab === 'trade-release' || viewTab === 'control-room' || viewTab === 'task-cc',
  })

  const releaseGateStgQuery = useQuery({
    queryKey: ['promote', 'release-gate', 'stg'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 30_000,
    enabled: viewTab === 'control-room' || viewTab === 'task-cc',
  })

  const supplyChainQuery = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 30_000,
    enabled: viewTab === 'control-room' || viewTab === 'task-cc',
  })

  const tierBQuery = useQuery({
    queryKey: ['promote', 'tier-b'],
    queryFn: fetchTierBStatus,
    refetchInterval: 30_000,
    enabled: viewTab === 'control-room' || viewTab === 'task-cc',
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
    enabled: runtimeMapSheetOpen || viewTab === 'runtime-map',
  })

  const topologyQuery = useQuery({
    queryKey: ['topology', envForRuntime],
    queryFn: () => fetchTopology(envForRuntime),
    refetchInterval: 30_000,
    enabled: runtimeMapSheetOpen || viewTab === 'runtime-map',
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
    setRuntimeMapSheetOpen(true)
  }, [envFilter])
  const openCluster = () => setViewTab('cluster')
  const openNetwork = () => setViewTab('network')
  const openSatelliteApi = () => setViewTab('satellite-api')
  const openSatelliteTelemetry = () => setViewTab('satellite-telemetry')
  const openObservability = () => setViewTab('observability')
  const openPluginGallery = () => setViewTab('plugin-gallery')
  const openPlacement = () => setViewTab('placement')
  const openAudit = () => setViewTab('audit')
  const openBriefing = useCallback((opts?: BriefingUrlState) => {
    if (opts != null) {
      const modeId = opts.taskModeContext?.modeId
      const resolved = {
        ...opts,
        view: opts.view ?? (modeId != null ? componentLineForTaskMode(modeId) : undefined),
        trackType: opts.trackType ?? (modeId != null ? trackTypeForTaskMode(modeId) : undefined),
      }
      writeBriefingUrlState(resolved)
    }
    setViewTab('briefing')
  }, [setViewTab])
  const openOperatorPlane = useCallback(() => {
    setViewTab('operator-plane')
    // Defer until Operator Plane mounts so #agent-host-deploy exists.
    window.requestAnimationFrame(() => {
      window.setTimeout(() => scrollToSection('agent-host-deploy'), 80)
    })
  }, [setViewTab])
  const openAgentDesk = useCallback((jobIdOrOpts?: OpenAgentDeskArg) => {
    if (typeof jobIdOrOpts === 'string') {
      setAgentDeskJobId(jobIdOrOpts)
      setAgentDeskPrefill(null)
      setAgentDeskFocusHandoffId(null)
      setAgentDeskFocusDecisionBriefs(false)
    } else if (jobIdOrOpts != null && isOpenAgentDeskPrefill(jobIdOrOpts)) {
      setAgentDeskPrefill(jobIdOrOpts.prefill)
      setAgentDeskJobId(null)
      setAgentDeskFocusHandoffId(null)
      setAgentDeskFocusDecisionBriefs(false)
    } else if (jobIdOrOpts != null && isOpenAgentDeskFocusHandoff(jobIdOrOpts)) {
      setAgentDeskFocusHandoffId(jobIdOrOpts.focusHandoffId)
      setAgentDeskJobId(null)
      setAgentDeskPrefill(null)
      setAgentDeskFocusDecisionBriefs(false)
    } else if (jobIdOrOpts != null && isOpenAgentDeskFocusDecisionBriefs(jobIdOrOpts)) {
      setAgentDeskFocusDecisionBriefs(true)
      setAgentDeskJobId(null)
      setAgentDeskPrefill(null)
      setAgentDeskFocusHandoffId(null)
    } else {
      setAgentDeskFocusDecisionBriefs(false)
    }
    setViewTab('agent-desk')
  }, [setViewTab])

  const expandAgentDock = useCallback(() => {
    setDockExpanded(true)
  }, [])

  const openOperatorDock = useCallback((tool: OperatorToolId = 'agent') => {
    setOperatorToolId(tool)
    setDockExpanded(true)
  }, [setOperatorToolId])

  /** Legacy hash / deep-link `#console` → Operator Dock Console + Network (nav page removed). */
  useEffect(() => {
    if (viewTab !== 'console') return
    openOperatorDock('console')
    setViewTab('network')
  }, [viewTab, openOperatorDock, setViewTab])

  const startAmbientAgentJob = useCallback((job: AmbientAgentJob) => {
    setAmbientJob(job)
    setOperatorToolId('agent')
    setDockExpanded(true)
  }, [setOperatorToolId])

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
  const openDefects = () => setViewTab('defects')
  const openSatelliteBus = () => setViewTab('satellite-bus')
  const openCompute = () => setViewTab('compute')
  const openAgentDeskTab = () => setViewTab('agent-desk')

  const openLaunchView = useCallback(
    (taskMode: 'mission-launch') => {
      setModeId(taskMode)
      setViewTab('task-cc', { taskMode })
    },
    [setModeId, setViewTab],
  )

  const openDailyOpsFleet = useCallback(() => {
    setModeId('daily-ops')
    setViewTab('task-cc', { taskMode: 'daily-ops' })
  }, [setModeId, setViewTab])

  const handleTaskModeChange = useCallback(
    (landingTab: string, nextModeId: TaskModeId) => {
      if (isConsoleViewTab(landingTab)) {
        setViewTab(landingTab, {
          taskMode: nextModeId !== 'system' ? nextModeId : undefined,
        })
      }
    },
    [setViewTab],
  )

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
  const runtimeLoading = topologyQuery.isLoading || runtimeMatrixQuery.isLoading
  const runtimeError =
    (topologyQuery.error as Error | null) ?? (runtimeMatrixQuery.error as Error | null)

  return (
    <TooltipProvider>
    <div className="task-mode-chrome-root flex min-h-svh w-full" data-task-mode={modeId}>
    <SidebarProvider>
      <ConsoleSidebar
        activeTab={viewTab}
        onSelect={(id) => setViewTab(id as ConsoleViewTab)}
        onModeChange={handleTaskModeChange}
      />
      <SidebarInset
        className="min-w-0 overflow-x-hidden pb-[var(--agent-dock-reserve,2.75rem)]"
        style={
          {
            ['--agent-dock-reserve' as string]: dockExpanded
              ? 'min(42vh, 28rem)'
              : '2.75rem',
          } as CSSProperties
        }
      >
        <div className="console-shell-chrome sticky top-0 z-20 shrink-0 bg-card">
          <ConsoleHeader
            plane={consoleNavPlane(viewTab)}
            pageTitle={VIEW_TITLES[viewTab]}
            pageDescription={VIEW_DESCRIPTIONS[viewTab]}
            pageActions={
              isGovernanceTab ? (
                <Button
                  variant="ghost"
                  size="xs"
                  className="shrink-0"
                  onClick={() => void handleCopyAllGovernance()}
                >
                  {govCopyState === 'copied'
                    ? 'All copied!'
                    : govCopyState === 'error'
                      ? 'Copy failed'
                      : 'Copy All for LLM'}
                </Button>
              ) : viewTab === 'trade-release' || viewTab === 'platform-release' ? (
                <BackToMissionLaunchButton onClick={() => openLaunchView('mission-launch')} />
              ) : undefined
            }
            healthy={healthQuery.data}
            onRefresh={refreshAll}
            viewerEnv={viewerEnv}
            viewerEnvLoading={viewerEnvLoading}
            onSelectTab={tabId => {
              if (isConsoleViewTab(tabId)) setViewTab(tabId)
            }}
            ambientAgent={{
              label: ambientJob?.label ?? 'Agent Task',
              onOpen: expandAgentDock,
              expanded: dockExpanded,
              running: ambientJob != null,
            }}
            onModeChange={handleTaskModeChange}
          />
        </div>
      <PageShell padding="compact" className="flex w-full min-w-0 flex-col gap-4">
        {OPS_CONTEXT_TABS.includes(viewTab) && (
          <OpsContextStrip
            onNavigate={tab => setViewTab(tab as ConsoleViewTab)}
            onOpenAgentDeskWithPrefill={prefill => openAgentDesk({ prefill })}
            onOpenRuntimeMap={openRuntimeMap}
          />
        )}
        {viewTab === 'agent-desk' && (
          <AgentDeskPage
            context={contextQuery.data}
            matrices={pulseMatrices}
            clusterSummary={clusterQuery.data}
            platformHealthy={healthQuery.data}
            auditRecords={auditRecords}
            initialJobId={agentDeskJobId}
            prefillPrompt={agentDeskPrefill}
            focusHandoffId={agentDeskFocusHandoffId}
            focusDecisionBriefs={agentDeskFocusDecisionBriefs}
            onInitialJobConsumed={() => setAgentDeskJobId(null)}
            onPrefillConsumed={() => setAgentDeskPrefill(null)}
            onFocusHandoffConsumed={() => setAgentDeskFocusHandoffId(null)}
            onFocusDecisionBriefsConsumed={() => setAgentDeskFocusDecisionBriefs(false)}
            onOpenBriefing={openBriefing}
            onOpenCluster={openCluster}
            onOpenMcpContract={() => setViewTab('mcp-contract')}
            onOpenAgentProtocol={() => setViewTab('agent-protocol')}
            onOpenAgentSystem={() => setViewTab('agent-system')}
            onOpenOperatorPlane={openOperatorPlane}
            onOpenTrustAutonomy={() => setViewTab('agent-governance')}
            onOpenDeliveryBoard={() => setViewTab('delivery-board')}
            onOpenBriefingReconciliation={() => setViewTab('briefing-reconciliation')}
          />
        )}

        {viewTab === 'agent-capability' && (
          <AgentCapabilityPage onOpenAgentDesk={openAgentDesk} />
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
              onOpenAudit={openAudit}
            />
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
                onOpenTradeDeploy={() => setViewTab('trade-release')}
                onOpenPromote={openPromote}
                onOpenAgentProtocol={() => setViewTab('agent-protocol')}
                onOpenNetwork={openNetwork}
                onOpenSatelliteBus={openSatelliteBus}
                onOpenCompute={openCompute}
                onOpenDefects={openDefects}
                onOpenAgentDeskTab={openAgentDeskTab}
                onOpenLaunchView={openLaunchView}
                onOpenFleetVendor={openDailyOpsFleet}
              />
            </Suspense>
          </>
        )}

        {viewTab === 'task-cc' && (
          <TaskControlCenterPage
            context={contextQuery.data}
            matrices={pulseMatrices}
            clusterSummary={clusterQuery.data}
            platformHealthy={healthQuery.data}
            stgSmoke={stgSmokeQuery.data}
            stgGate={releaseGateStgQuery.data}
            lastDeliverSucceeded={lastDeliverSucceeded}
            tierB={tierBQuery.data}
            onNavigate={tab => setViewTab(tab as ConsoleViewTab)}
            onOpenBriefing={openBriefing}
            onOpenPromote={openPromote}
            onOpenDelivery={openDelivery}
            ambientJobId={ambientJob?.id ?? null}
            ambientJobScope={ambientJob?.scope ?? null}
            onStartAgentJob={startAmbientAgentJob}
            onOpenAgentDesk={openAgentDesk}
            onExpandAgentDock={expandAgentDock}
          />
        )}

        {viewTab === 'audit' && (
          <AuditPage records={auditRecords} isLoading={auditQuery.isLoading} />
        )}

        {viewTab === 'defects' && (
          <DefectsPage
            canOperate={canOperate}
            onStartAgentJob={startAmbientAgentJob}
          />
        )}

        {viewTab === 'cluster' && (
          <ClusterPage
              onOpenStandards={openStandards}
              onOpenRuntimeMap={() => openRuntimeMap()}
              onOpenAudit={openAudit}
              onOpenServerConsole={() => openOperatorDock('console')}
              onOpenAgentDesk={openAgentDesk}
              onOpenDefects={() => setViewTab('defects')}
              onOpenObservability={openObservability}
              ambientJobId={ambientJob?.id ?? null}
              onStartAgentJob={startAmbientAgentJob}
            />
        )}

        {viewTab === 'placement' && (
          <PlacementPage onOpenDelivery={openDelivery} onOpenCluster={openCluster} />
        )}

        {viewTab === 'delivery-board' && <DeliveryBoardPage onOpenBriefing={openBriefing} />}

        {viewTab === 'trade-release' && (
          <TradeReleasePage
              context={contextQuery.data}
              isLoading={contextQuery.isLoading}
              onOpenPlacement={openPlacement}
              onOpenSatelliteBus={openSatelliteBus}
              onOpenObservability={openObservability}
              onOpenApiHealth={openSatelliteApi}
              ambientJobId={ambientJob?.id ?? null}
              onStartAgentJob={startAmbientAgentJob}
            />
        )}

        {viewTab === 'platform-release' && (
          <PlatformReleasePage
              ambientJobId={ambientJob?.id ?? null}
              onStartAgentJob={startAmbientAgentJob}
            />
        )}

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
            onOpenCluster={openCluster}
            onOpenTelemetry={openSatelliteTelemetry}
            onOpenObservability={openObservability}
            onOpenPluginGallery={openPluginGallery}
            onOpenApiHealth={openSatelliteApi}
            ambientJobId={ambientJob?.id ?? null}
            onStartAgentJob={startAmbientAgentJob}
          />
        )}

        {viewTab === 'satellite-api' && (
          <SatelliteApiHealthPage onOpenObservability={openObservability} />
        )}

        {viewTab === 'satellite-telemetry' && (
          <SatelliteTelemetryPage
            onOpenCluster={openCluster}
            onOpenObservability={openObservability}
          />
        )}

        {viewTab === 'observability' && (
          <ObservabilityPage onNavigate={tab => setViewTab(tab as ConsoleViewTab)} />
        )}

        {viewTab === 'plugin-gallery' && <PluginGalleryPage />}

        {isGovernanceTab && (
          <div className="flex min-w-0 gap-4">
            <GuidesSettingsNav
              activeTab={viewTab}
              onSelect={tabId => {
                if (isConsoleViewTab(tabId)) setViewTab(tabId)
              }}
            />
            <div className="flex min-w-0 flex-1 flex-col gap-4">
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
              {viewTab === 'agent-protocol' && (
                <AgentProtocolPage
                  onOpenDeliveryBoard={() => setViewTab('delivery-board')}
                  onOpenAgentSystem={() => setViewTab('agent-system')}
                />
              )}
              {viewTab === 'briefing-reconciliation' && (
                <BriefingReconciliationPage context={contextQuery.data} onOpenAgentDesk={openAgentDesk} />
              )}
              {viewTab === 'mcp-contract' && <McpContractPage />}
              {viewTab === 'design-system' && <DesignSystemPage />}
            </div>
          </div>
        )}

        <ControlRoomRuntimeMapSheet
          open={runtimeMapSheetOpen}
          onOpenChange={setRuntimeMapSheetOpen}
          environments={envQuery.data}
          envFilter={envFilter}
          onEnvFilterChange={setEnvFilter}
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
      </PageShell>
      </SidebarInset>
      <AgentExecutionDock
        jobId={ambientJob?.id ?? null}
        label={ambientJob?.label}
        scope={ambientJob?.scope}
        expanded={dockExpanded}
        onExpandedChange={setDockExpanded}
        toolId={operatorToolId}
        onToolIdChange={setOperatorToolId}
        onDismiss={() => {
          setAmbientJob(null)
          setDockExpanded(true)
        }}
        onOpenAgentDesk={id => {
          if (id != null && id !== '') openAgentDesk(id)
          else openAgentDeskTab()
        }}
        onOpenOperatorPlane={openOperatorPlane}
        onComplete={handleAmbientJobComplete}
      />
    </SidebarProvider>
    </div>
    </TooltipProvider>
  )
}

export function ConsolePage() {
  return (
    <TaskModeProvider>
      <ConsolePageInner />
    </TaskModeProvider>
  )
}
