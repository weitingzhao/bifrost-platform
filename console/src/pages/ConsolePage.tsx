import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, PageHeader, PageShell, SidebarInset, SidebarProvider, TooltipProvider } from '@bifrost/ui'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
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
  fetchTopology,
  isAllMatrices,
} from '@/api/platform'
import { DOC_LINKS } from '@/lib/docsLinks'
import { EnvironmentStrip, type EnvFilter } from '@/components/EnvironmentStrip'
import { FocusStrip } from '@/components/FocusStrip'
import { PlatformAuthBar } from '@/components/PlatformAuthBar'
import { ConsoleHeader, OpsContextBar } from '@/components/ConsoleHeader'
import { ConsoleSidebar, type ConsoleViewTab } from '@/components/ConsoleSidebar'
import { buildFullArchitectureLlmPack } from '@/lib/architecture/buildArchitectureLlmPack'
import { AgentProtocolPage } from '@/pages/AgentProtocolPage'
import { AuditPage } from '@/pages/AuditPage'
import { BlueprintPage } from '@/pages/BlueprintPage'
import { BriefingPage } from '@/pages/BriefingPage'
import { ClusterPage } from '@/pages/ClusterPage'
import { DeliveryPage } from '@/pages/DeliveryPage'
import { DeployMainlinePage } from '@/pages/DeployMainlinePage'
import { EnvironmentsPage } from '@/pages/EnvironmentsPage'
import { ProgramPage } from '@/pages/ProgramPage'
import { PromotePage } from '@/pages/PromotePage'
import { PulsePage } from '@/pages/PulsePage'
import { RuntimeMapPage } from '@/pages/RuntimeMapPage'
import { ServerConsolePage } from '@/pages/ServerConsolePage'
import { DesignSystemPage } from '@/pages/DesignSystemPage'
import { K3sArchitecturePage } from '@/pages/K3sArchitecturePage'
import { K3sBootstrapPage } from '@/pages/K3sBootstrapPage'
import { RoadmapPage } from '@/pages/RoadmapPage'
import { StandardsPage } from '@/pages/StandardsPage'

const ControlRoomPage = lazy(() =>
  import('@/pages/ControlRoomPage').then(m => ({ default: m.ControlRoomPage })),
)

const VIEW_TITLES: Record<ConsoleViewTab, string> = {
  briefing: 'Agent Briefing',
  'control-room': 'Control Room',
  pulse: 'Pulse',
  audit: 'Audit',
  'runtime-map': 'Runtime Map',
  cluster: 'Cluster',
  delivery: 'Delivery',
  program: 'Milestones',
  promote: 'Promote',
  blueprint: 'Blueprint',
  environments: 'Environments',
  roadmap: 'Platform Roadmap',
  'k3s-architecture': 'K3s Architecture',
  'k3s-bootstrap': 'K3s Bootstrap',
  'deploy-mainline': 'Deploy Mainline',
  'platform-standards': 'Platform',
  'agent-protocol': 'Agent Protocol',
  'design-system': 'Design System',
  console: 'Server console',
}

const OPS_CONTEXT_TABS: ConsoleViewTab[] = [
  'briefing',
  'control-room',
  'pulse',
  'promote',
  'delivery',
  'cluster',
  'runtime-map',
  'program',
  'deploy-mainline',
]

const LEGACY_RUNTIME_HASHES: Record<string, ConsoleViewTab> = {
  topology: 'runtime-map',
  matrix: 'runtime-map',
}

export function ConsolePage() {
  const [envFilter, setEnvFilter] = useState<EnvFilter>('prod')
  const [viewTab, setViewTab] = useState<ConsoleViewTab>('control-room')
  const qc = useQueryClient()

  const envForRuntime = envFilter === 'all' ? 'prod' : envFilter

  useEffect(() => {
    const hash = window.location.hash.replace(/^#/, '')
    const legacy = LEGACY_RUNTIME_HASHES[hash]
    if (legacy != null) setViewTab(legacy)
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
      viewTab === 'pulse' ||
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
      viewTab === 'control-room' ||
      viewTab === 'pulse' ||
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
    enabled: viewTab === 'delivery',
  })

  const stgSmokeQuery = useQuery({
    queryKey: ['delivery', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 30_000,
    enabled: viewTab === 'delivery' || viewTab === 'pulse',
  })

  const releaseGateQuery = useQuery({
    queryKey: ['promote', 'release-gate'],
    queryFn: fetchReleaseGate,
    refetchInterval: 30_000,
    enabled: viewTab === 'promote',
  })

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

  const matrixUpdatedAt = useMemo(() => {
    if (viewTab === 'runtime-map') return runtimeMatrix?.generated_at ?? null
    if (
      viewTab === 'briefing' ||
      viewTab === 'control-room' ||
      viewTab === 'pulse' ||
      viewTab === 'promote' ||
      viewTab === 'delivery'
    ) {
      if (pulseMatrices.length === 0) return null
      return pulseMatrices[0]?.generated_at ?? null
    }
    return null
  }, [viewTab, pulseMatrices, runtimeMatrix])

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
  const openRuntimeMap = () => setViewTab('runtime-map')
  const openCluster = () => setViewTab('cluster')
  const openAudit = () => setViewTab('audit')
  const openBlueprint = () => setViewTab('blueprint')
  const openStandards = () => setViewTab('platform-standards')
  const openEnvironments = () => setViewTab('environments')

  const [govCopyState, setGovCopyState] = useState<'idle' | 'copied' | 'error'>('idle')
  const isArchTab =
    viewTab === 'blueprint' ||
    viewTab === 'environments' ||
    viewTab === 'roadmap' ||
    viewTab === 'k3s-architecture' ||
    viewTab === 'k3s-bootstrap'
  const isStdTab = viewTab === 'platform-standards' || viewTab === 'agent-protocol' || viewTab === 'design-system'
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

  const docLinks = [
    { id: 'docs-staging', label: 'Docs staging', href: DOC_LINKS.platformHome },
    { id: 'docs-staging-policy', label: 'Staging policy', href: DOC_LINKS.stagingPolicy },
    { id: 'docs-infra', label: 'Infra handbook', href: DOC_LINKS.infraHome },
  ]

  const showEnvStrip = viewTab === 'runtime-map'
  const showPageHeader = ![
    'briefing',
    'control-room',
    'pulse',
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
    'design-system',
    'console',
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
        docLinks={docLinks}
      />
      <SidebarInset>
        <div className="sticky top-0 z-20 bg-card">
          <ConsoleHeader
            title={VIEW_TITLES[viewTab]}
            healthy={healthQuery.data}
            onRefresh={refreshAll}
          >
            <PlatformAuthBar compact hideRefresh />
          </ConsoleHeader>
          {OPS_CONTEXT_TABS.includes(viewTab) && (
            <OpsContextBar>
              <FocusStrip
                context={contextQuery.data}
                isLoading={contextQuery.isLoading}
                matrixUpdatedAt={matrixUpdatedAt}
                clusterSummary={clusterQuery.data}
                clusterLoading={clusterQuery.isLoading}
                onOpenProgram={openProgram}
                onOpenDelivery={openDelivery}
                onOpenCluster={openCluster}
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
            />
          </>
        )}

        {viewTab === 'control-room' && (
          <>
            <PageHeader
              title={VIEW_TITLES['control-room']}
              description="Dual flywheel governance, program milestone spine, and Agent context packs."
            />
            <Suspense fallback={<p className="text-[var(--muted-foreground)]">Loading control room…</p>}>
              <ControlRoomPage
                context={contextQuery.data}
                contextLoading={contextQuery.isLoading}
                matrices={pulseMatrices}
                matrixLoading={matrixForPulse.isLoading}
                onOpenRuntimeMap={openRuntimeMap}
                onOpenProgram={openProgram}
                onOpenDelivery={openDelivery}
                onOpenAudit={openAudit}
              />
            </Suspense>
          </>
        )}

        {viewTab === 'pulse' && (
          <>
            <PageHeader
              title={VIEW_TITLES.pulse}
              description="Live reachability and program focus."
            />
            <PulsePage
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
              onOpenCluster={openCluster}
              onOpenDelivery={openDelivery}
            />
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

        {viewTab === 'runtime-map' && (
          <>
            <PageHeader
              title={VIEW_TITLES['runtime-map']}
              description="Hardware topology, software stack, and live matrix probes in one view."
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
              onOpenCluster={openCluster}
            />
          </>
        )}

        {viewTab === 'cluster' && (
          <>
            <PageHeader
              title={VIEW_TITLES.cluster}
              description="K3s cluster nodes, namespaces, and workloads — L0 read-only via platform-api."
            />
            <ClusterPage
              onOpenStandards={openStandards}
              onOpenEnvironments={openEnvironments}
              onOpenAudit={openAudit}
            />
          </>
        )}

        {viewTab === 'delivery' && (
          <>
            <PageHeader
              title={VIEW_TITLES.delivery}
              description="CI/CD dual track — near-term Mac runner vs target GitOps on K3s."
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
              stgSmokeError={
                stgSmokeQuery.error instanceof Error ? stgSmokeQuery.error.message : null
              }
              isLoading={contextQuery.isLoading || matrixForPulse.isLoading}
              onOpenMilestones={openProgram}
              onOpenPromote={openPromote}
              onOpenAudit={openAudit}
            />
          </>
        )}

        {viewTab === 'program' && (
          <>
            <PageHeader
              title={VIEW_TITLES.program}
              description="Milestones, owner decisions, and roadmap from ops-context spine."
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
              description="Read-only promotion readiness across flywheel A and B."
            />
            <PromotePage
              context={contextQuery.data}
              matrices={pulseMatrices}
              releaseGate={releaseGateQuery.data}
              releaseGateLoading={releaseGateQuery.isLoading}
              releaseGateError={
                releaseGateQuery.error instanceof Error ? releaseGateQuery.error.message : null
              }
              isLoading={contextQuery.isLoading || matrixForPulse.isLoading}
              onOpenProgram={openProgram}
              onOpenDelivery={openDelivery}
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

        {viewTab === 'console' && <ServerConsolePage />}

        {isGovernanceTab && (
          <div className="flex items-center justify-between gap-3">
            <PageHeader
              title={VIEW_TITLES[viewTab]}
              description={
                viewTab === 'blueprint' ? 'North Star, system architecture, control-plane layers, and design principles.'
                  : viewTab === 'environments' ? 'Flows, phases, and LLM catalog — hardware/scope live view is on Runtime Map.'
                  : viewTab === 'roadmap' ? 'Compose → K3s phased plan: hardware roles, 2C-B priority, GitOps migration, AI ops.'
                  : viewTab === 'k3s-architecture' ? 'Target K3s topology, CNPG, GitOps, AI-native ops, and living checkpoints.'
                  : viewTab === 'k3s-bootstrap' ? 'First-node deployment runbook, verification checklist, node join steps, and sign-off.'
                  : viewTab === 'platform-standards' ? 'Trade stack probe contract, cluster actuation phases, and API route inventory.'
                  : viewTab === 'agent-protocol' ? 'Agent interaction modes, context pack layers, forbidden actions, and session startup.'
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

        {viewTab === 'environments' && (
          <EnvironmentsPage
            context={contextQuery.data}
            onOpenRuntimeMap={openRuntimeMap}
            onOpenDelivery={openDelivery}
          />
        )}

        {viewTab === 'roadmap' && <RoadmapPage />}

        {viewTab === 'k3s-architecture' && <K3sArchitecturePage />}

        {viewTab === 'k3s-bootstrap' && <K3sBootstrapPage />}

        {viewTab === 'platform-standards' && <StandardsPage />}

        {viewTab === 'agent-protocol' && <AgentProtocolPage />}

        {viewTab === 'design-system' && <DesignSystemPage />}
      </PageShell>
      </SidebarInset>
    </SidebarProvider>
    </TooltipProvider>
  )
}
