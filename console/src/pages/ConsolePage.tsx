import { useQuery, useQueryClient } from '@tanstack/react-query'
import { MonitoringShell, PageHeader, PageShell, StatusLamp } from '@bifrost/ui'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import type { MatrixResponse } from '@/api/types'
import {
  fetchCluster,
  fetchContext,
  fetchEnvironments,
  fetchMatrix,
  fetchPlatformHealth,
  fetchTopology,
  isAllMatrices,
} from '@/api/platform'
import { DOC_LINKS } from '@/lib/docsLinks'
import { EnvironmentStrip, type EnvFilter } from '@/components/EnvironmentStrip'
import { FocusStrip } from '@/components/FocusStrip'
import { PlatformAuthBar } from '@/components/PlatformAuthBar'
import { buildFullArchitectureLlmPack } from '@/lib/architecture/buildArchitectureLlmPack'
import { AgentProtocolPage } from '@/pages/AgentProtocolPage'
import { BlueprintPage } from '@/pages/BlueprintPage'
import { BriefingPage } from '@/pages/BriefingPage'
import { ClusterPage } from '@/pages/ClusterPage'
import { DeliveryPage } from '@/pages/DeliveryPage'
import { EnvironmentsPage } from '@/pages/EnvironmentsPage'
import { ProgramPage } from '@/pages/ProgramPage'
import { PromotePage } from '@/pages/PromotePage'
import { PulsePage } from '@/pages/PulsePage'
import { RuntimeMapPage } from '@/pages/RuntimeMapPage'
import { ServerConsolePage } from '@/pages/ServerConsolePage'
import { DesignSystemPage } from '@/pages/DesignSystemPage'
import { StandardsPage } from '@/pages/StandardsPage'

const ControlRoomPage = lazy(() =>
  import('@/pages/ControlRoomPage').then(m => ({ default: m.ControlRoomPage })),
)

type ViewTab =
  | 'briefing'
  | 'control-room'
  | 'pulse'
  | 'runtime-map'
  | 'cluster'
  | 'delivery'
  | 'program'
  | 'promote'
  | 'blueprint'
  | 'environments'
  | 'platform-standards'
  | 'agent-protocol'
  | 'design-system'
  | 'console'

const VIEW_TITLES: Record<ViewTab, string> = {
  briefing: 'Agent Briefing',
  'control-room': 'Control Room',
  pulse: 'Pulse',
  'runtime-map': 'Runtime Map',
  cluster: 'Cluster',
  delivery: 'Delivery',
  program: 'Milestones',
  promote: 'Promote',
  blueprint: 'Blueprint',
  environments: 'Environments',
  'platform-standards': 'Platform',
  'agent-protocol': 'Agent Protocol',
  'design-system': 'Design System',
  console: 'Server console',
}

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

const LEGACY_RUNTIME_HASHES: Record<string, ViewTab> = {
  topology: 'runtime-map',
  matrix: 'runtime-map',
}

export function ConsolePage() {
  const [envFilter, setEnvFilter] = useState<EnvFilter>('prod')
  const [viewTab, setViewTab] = useState<ViewTab>('control-room')
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
  }

  const openProgram = () => setViewTab('program')
  const openDelivery = () => setViewTab('delivery')
  const openPromote = () => setViewTab('promote')
  const openRuntimeMap = () => setViewTab('runtime-map')
  const openCluster = () => setViewTab('cluster')
  const openBlueprint = () => setViewTab('blueprint')
  const openStandards = () => setViewTab('platform-standards')
  const openEnvironments = () => setViewTab('environments')

  const [govCopyState, setGovCopyState] = useState<'idle' | 'copied' | 'error'>('idle')
  const isArchTab = viewTab === 'blueprint' || viewTab === 'environments'
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

  type NavItemDef = {
    id: string
    label: string
    shortLabel: string
    href?: string
    external?: boolean
  }

  const navGroups = [
    {
      label: 'Ops',
      items: [
        { id: 'briefing', label: 'Agent Briefing', shortLabel: 'B' },
        { id: 'control-room', label: 'Control Room', shortLabel: 'C' },
        { id: 'pulse', label: 'Pulse', shortLabel: 'P' },
      ],
    },
    {
      label: 'Runtime',
      items: [
        { id: 'runtime-map', label: 'Runtime Map', shortLabel: 'M' },
        { id: 'cluster', label: 'Cluster', shortLabel: 'K' },
      ],
    },
    {
      label: 'Program',
      items: [
        { id: 'delivery', label: 'Delivery', shortLabel: 'D' },
        { id: 'program', label: 'Milestones', shortLabel: 'G' },
        { id: 'promote', label: 'Promote', shortLabel: 'R' },
      ],
    },
    {
      label: 'Architecture',
      items: [
        { id: 'blueprint', label: 'Blueprint', shortLabel: 'A' },
        { id: 'environments', label: 'Environments', shortLabel: 'E' },
      ],
    },
    {
      label: 'Standards',
      items: [
        { id: 'platform-standards', label: 'Platform', shortLabel: 'T' },
        { id: 'agent-protocol', label: 'Agent Protocol', shortLabel: 'N' },
        { id: 'design-system', label: 'Design System', shortLabel: 'U' },
      ],
    },
    {
      label: 'Tools',
      items: [{ id: 'console', label: 'Server console', shortLabel: 'S' }],
    },
    {
      label: 'Docs',
      items: [
        {
          id: 'docs-staging',
          label: 'Docs staging',
          shortLabel: 'D',
          href: DOC_LINKS.platformHome,
          external: true,
        },
        {
          id: 'docs-staging-policy',
          label: 'Staging policy',
          shortLabel: 'S',
          href: DOC_LINKS.stagingPolicy,
          external: true,
        },
        {
          id: 'docs-infra',
          label: 'Infra handbook',
          shortLabel: 'I',
          href: DOC_LINKS.infraHome,
          external: true,
        },
      ],
    },
  ].map(group => ({
    label: group.label,
    items: (group.items as NavItemDef[]).map(item => ({
      id: item.id,
      label: item.label,
      shortLabel: item.shortLabel,
      href: item.href ?? `#${item.id}`,
      external: item.external,
      active: !item.external && viewTab === item.id,
      onClick:
        item.external != null && item.external
          ? undefined
          : () => setViewTab(item.id as ViewTab),
    })),
  }))

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
    <MonitoringShell
      productName="Bifrost Ops"
      productBadge="Ops"
      productTagline="Environment governance & release (L0 read-only)"
      navGroups={navGroups}
      peerApp={{
        label: 'Bifrost Trade Monitoring',
        href: TRADE_APP_URL,
        description: 'Business console · positions, daemon, market',
      }}
      headerActions={
        <>
          <div className="shell-header-focus">
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
          </div>
          <PlatformAuthBar />
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Ops API{' '}
            <StatusLamp value={healthQuery.data ? 'ok' : 'fail'} kind="reach" />
          </span>
          <button type="button" className="btn-ui btn-ui-primary" onClick={refreshAll}>
            Refresh
          </button>
        </>
      }
    >
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
              onOpenRuntimeMap={openRuntimeMap}
              onOpenProgram={openProgram}
              onOpenCluster={openCluster}
            />
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
            <ClusterPage onOpenStandards={openStandards} onOpenEnvironments={openEnvironments} />
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
              isLoading={contextQuery.isLoading || matrixForPulse.isLoading}
              onOpenMilestones={openProgram}
              onOpenPromote={openPromote}
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
              isLoading={contextQuery.isLoading || matrixForPulse.isLoading}
              onOpenProgram={openProgram}
              onOpenDelivery={openDelivery}
            />
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
                  : viewTab === 'platform-standards' ? 'Trade stack probe contract, cluster actuation phases, and API route inventory.'
                  : viewTab === 'agent-protocol' ? 'Agent interaction modes, context pack layers, forbidden actions, and session startup.'
                  : 'Dense UI layer stack, mandatory mapping, business semantic colors, and primitives inventory.'
              }
            />
            <button
              type="button"
              className="btn-ui btn-ui-ghost shrink-0 text-xs"
              onClick={() => void handleCopyAllGovernance()}
            >
              {govCopyState === 'copied' ? 'All copied!' : govCopyState === 'error' ? 'Copy failed' : 'Copy All for LLM'}
            </button>
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

        {viewTab === 'platform-standards' && <StandardsPage />}

        {viewTab === 'agent-protocol' && <AgentProtocolPage />}

        {viewTab === 'design-system' && <DesignSystemPage />}
      </PageShell>
    </MonitoringShell>
  )
}
