import { useQuery, useQueryClient } from '@tanstack/react-query'
import { MonitoringShell, PageHeader, PageShell, SegmentControl, StatusLamp } from '@bifrost/ui'
import { lazy, Suspense, useMemo, useState } from 'react'
import type { MatrixResponse } from '@/api/types'
import {
  fetchContext,
  fetchEnvironments,
  fetchMatrix,
  fetchPlatformHealth,
  fetchTopology,
  isAllMatrices,
} from '@/api/platform'
import { EnvironmentStrip, type EnvFilter } from '@/components/EnvironmentStrip'
import { FocusStrip } from '@/components/FocusStrip'
import { MatrixTable } from '@/components/MatrixTable'
import { TopologyDiagram } from '@/components/TopologyDiagram'
import { EnvironmentsPage } from '@/pages/EnvironmentsPage'
import { ProgramPage } from '@/pages/ProgramPage'
import { PromotePage } from '@/pages/PromotePage'
import { PulsePage } from '@/pages/PulsePage'
import { ServerConsolePage } from '@/pages/ServerConsolePage'

const ControlRoomPage = lazy(() =>
  import('@/pages/ControlRoomPage').then(m => ({ default: m.ControlRoomPage })),
)

type ViewTab =
  | 'control-room'
  | 'pulse'
  | 'topology'
  | 'matrix'
  | 'program'
  | 'promote'
  | 'environments'
  | 'console'

const VIEW_TITLES: Record<ViewTab, string> = {
  'control-room': 'Control Room',
  pulse: 'Pulse',
  topology: 'Network topology',
  matrix: 'Connectivity matrix',
  program: 'Program',
  promote: 'Promote',
  environments: 'Catalog',
  console: 'Server console',
}

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

const RUNTIME_TABS: ViewTab[] = ['topology', 'matrix']

export function ConsolePage() {
  const [envFilter, setEnvFilter] = useState<EnvFilter>('prod')
  const [viewTab, setViewTab] = useState<ViewTab>('control-room')
  const qc = useQueryClient()

  const envForTopology = envFilter === 'all' ? 'prod' : envFilter

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
    enabled: viewTab === 'control-room' || viewTab === 'pulse' || viewTab === 'promote',
  })

  const matrixQuery = useQuery({
    queryKey: ['matrix', envFilter],
    queryFn: () => fetchMatrix(envFilter === 'all' ? undefined : envFilter),
    refetchInterval: 30_000,
    enabled: viewTab === 'matrix',
  })

  const topologyQuery = useQuery({
    queryKey: ['topology', envForTopology],
    queryFn: () => fetchTopology(envForTopology),
    refetchInterval: 30_000,
    enabled: viewTab === 'topology',
  })

  const pulseMatrices = useMemo((): MatrixResponse[] => {
    const data = matrixForPulse.data
    if (!data) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixForPulse.data])

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQuery.data
    if (!data) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixQuery.data])

  const matrixUpdatedAt = useMemo(() => {
    const src =
      viewTab === 'control-room' || viewTab === 'pulse' || viewTab === 'promote'
        ? pulseMatrices
        : matrices
    if (src.length === 0) return null
    return src[0]?.generated_at ?? null
  }, [viewTab, pulseMatrices, matrices])

  function refreshAll() {
    void qc.invalidateQueries({ queryKey: ['matrix'] })
    void qc.invalidateQueries({ queryKey: ['topology'] })
    void qc.invalidateQueries({ queryKey: ['platform-health'] })
    void qc.invalidateQueries({ queryKey: ['context'] })
  }

  const openProgram = () => setViewTab('program')
  const openMatrix = () => setViewTab('matrix')

  const navGroups = [
    {
      label: 'Ops',
      items: [
        { id: 'control-room', label: 'Control Room', shortLabel: 'C' },
        { id: 'pulse', label: 'Pulse', shortLabel: 'P' },
      ],
    },
    {
      label: 'Runtime',
      items: [
        { id: 'topology', label: 'Topology', shortLabel: 'T' },
        { id: 'matrix', label: 'Matrix', shortLabel: 'M' },
      ],
    },
    {
      label: 'Program',
      items: [
        { id: 'program', label: 'Milestones', shortLabel: 'G' },
        { id: 'promote', label: 'Promote', shortLabel: 'R' },
      ],
    },
    {
      label: 'Catalog',
      items: [{ id: 'environments', label: 'Environments', shortLabel: 'E' }],
    },
    {
      label: 'Tools',
      items: [{ id: 'console', label: 'Server console', shortLabel: 'S' }],
    },
  ].map(group => ({
    label: group.label,
    items: group.items.map(item => ({
      id: item.id,
      label: item.label,
      shortLabel: item.shortLabel,
      href: `#${item.id}`,
      active: viewTab === item.id,
      onClick: () => setViewTab(item.id as ViewTab),
    })),
  }))

  const showEnvStrip = RUNTIME_TABS.includes(viewTab)
  const showRuntimeSegment = RUNTIME_TABS.includes(viewTab)
  const showPageHeader = ![
    'control-room',
    'pulse',
    'program',
    'promote',
    'environments',
    'console',
  ].includes(viewTab)

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
              onOpenProgram={openProgram}
            />
          </div>
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
            description="L0 read-only probes — collapse the sidebar to use full width for tables and topology."
          />
        )}

        {envQuery.data && showEnvStrip && (
          <EnvironmentStrip
            environments={envQuery.data}
            selected={envFilter}
            onSelect={id => {
              setEnvFilter(id)
              if (id === 'all') setViewTab('matrix')
            }}
          />
        )}

        {showRuntimeSegment && (
          <SegmentControl
            ariaLabel="Runtime view"
            value={viewTab}
            onChange={v => setViewTab(v as ViewTab)}
            options={[
              { value: 'topology', label: 'Topology' },
              { value: 'matrix', label: 'Matrix' },
            ]}
          />
        )}

        {viewTab === 'control-room' && (
          <>
            <PageHeader
              title={VIEW_TITLES['control-room']}
              description="Dual flywheel governance, release pipeline, and Agent context packs."
            />
            <Suspense fallback={<p className="text-[var(--muted-foreground)]">Loading control room…</p>}>
              <ControlRoomPage
                context={contextQuery.data}
                contextLoading={contextQuery.isLoading}
                matrices={pulseMatrices}
                matrixLoading={matrixForPulse.isLoading}
                onOpenMatrix={openMatrix}
                onOpenProgram={openProgram}
              />
            </Suspense>
          </>
        )}

        {viewTab === 'pulse' && (
          <>
            <PageHeader
              title={VIEW_TITLES.pulse}
              description="Live reachability and program focus — default Ops entry."
            />
            <PulsePage
              context={contextQuery.data}
              contextLoading={contextQuery.isLoading}
              matrices={pulseMatrices}
              matrixLoading={matrixForPulse.isLoading}
              matrixError={matrixForPulse.error as Error | null}
              platformHealthy={healthQuery.data === true}
              onOpenMatrix={openMatrix}
              onOpenProgram={openProgram}
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
            />
          </>
        )}

        {viewTab === 'console' && <ServerConsolePage />}

        {viewTab === 'environments' && (
          <>
            <PageHeader
              title={VIEW_TITLES.environments}
              description="Hardware, CI/CD, K3s target, and trade Dev/Prod catalog."
            />
            <EnvironmentsPage context={contextQuery.data} />
          </>
        )}

        {viewTab === 'topology' && envFilter === 'all' && (
          <p className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Topology view uses a single environment — showing Production. Select Dev or Prod for
            environment-specific links.
          </p>
        )}

        {viewTab === 'topology' && topologyQuery.isLoading && (
          <p className="text-[var(--muted-foreground)]">Loading topology…</p>
        )}
        {viewTab === 'topology' && topologyQuery.isError && (
          <p className="lamp-fail">
            Failed to load topology: {(topologyQuery.error as Error).message}
          </p>
        )}
        {viewTab === 'topology' && topologyQuery.data && (
          <TopologyDiagram data={topologyQuery.data} />
        )}

        {viewTab === 'matrix' && matrixQuery.isLoading && (
          <p className="text-[var(--muted-foreground)]">Probing targets…</p>
        )}
        {viewTab === 'matrix' && matrixQuery.isError && (
          <p className="lamp-fail">Failed to load matrix: {(matrixQuery.error as Error).message}</p>
        )}
        {viewTab === 'matrix' && matrices.map(m => <MatrixTable key={m.environment} matrix={m} />)}
      </PageShell>
    </MonitoringShell>
  )
}
