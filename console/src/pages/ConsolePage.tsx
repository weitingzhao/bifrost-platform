import { useQuery, useQueryClient } from '@tanstack/react-query'
import { MonitoringShell, PageHeader, PageShell, SegmentControl, StatusLamp } from '@bifrost/ui'
import { useMemo, useState } from 'react'
import type { MatrixResponse } from '@/api/types'
import {
  fetchEnvironments,
  fetchMatrix,
  fetchPlatformHealth,
  fetchTopology,
  isAllMatrices,
} from '@/api/platform'
import { EnvironmentStrip, type EnvFilter } from '@/components/EnvironmentStrip'
import { MatrixTable } from '@/components/MatrixTable'
import { TopologyDiagram } from '@/components/TopologyDiagram'
import { EnvironmentsPage } from '@/pages/EnvironmentsPage'
import { ServerConsolePage } from '@/pages/ServerConsolePage'

type ViewTab = 'topology' | 'matrix' | 'environments' | 'console'

const VIEW_TITLES: Record<ViewTab, string> = {
  topology: 'Network topology',
  matrix: 'Connectivity matrix',
  environments: 'Environments',
  console: 'Server console',
}

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

export function ConsolePage() {
  const [envFilter, setEnvFilter] = useState<EnvFilter>('prod')
  const [viewTab, setViewTab] = useState<ViewTab>('topology')
  const qc = useQueryClient()

  const envForTopology = envFilter === 'all' ? 'prod' : envFilter

  const envQuery = useQuery({
    queryKey: ['environments'],
    queryFn: fetchEnvironments,
  })

  const healthQuery = useQuery({
    queryKey: ['platform-health'],
    queryFn: fetchPlatformHealth,
    refetchInterval: 15_000,
  })

  const matrixQuery = useQuery({
    queryKey: ['matrix', envFilter],
    queryFn: () => fetchMatrix(envFilter === 'all' ? undefined : envFilter),
    refetchInterval: 30_000,
    enabled: viewTab === 'matrix' || envFilter !== 'all',
  })

  const topologyQuery = useQuery({
    queryKey: ['topology', envForTopology],
    queryFn: () => fetchTopology(envForTopology),
    refetchInterval: 30_000,
    enabled: viewTab === 'topology',
  })

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQuery.data
    if (!data) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixQuery.data])

  function refreshAll() {
    void qc.invalidateQueries({ queryKey: ['matrix'] })
    void qc.invalidateQueries({ queryKey: ['topology'] })
    void qc.invalidateQueries({ queryKey: ['platform-health'] })
  }

  const navGroups = [
    {
      label: 'Console',
      items: (
        [
          { id: 'topology', label: 'Topology', shortLabel: 'T' },
          { id: 'matrix', label: 'Matrix', shortLabel: 'M' },
          { id: 'environments', label: 'Environments', shortLabel: 'E' },
          { id: 'console', label: 'Console', shortLabel: 'C' },
        ] as const
      ).map(item => ({
        id: item.id,
        label: item.label,
        shortLabel: item.shortLabel,
        href: `#${item.id}`,
        active: viewTab === item.id,
        onClick: () => setViewTab(item.id),
      })),
    },
  ]

  return (
    <MonitoringShell
      productName="Bifrost Platform"
      productBadge="Environment"
      productTagline="Connectivity & governance (L0 read-only)"
      navGroups={navGroups}
      peerApp={{
        label: 'Bifrost Trade Monitoring',
        href: TRADE_APP_URL,
        description: 'Business console · positions, daemon, market',
      }}
      headerActions={
        <>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Platform API{' '}
            <StatusLamp value={healthQuery.data ? 'ok' : 'fail'} kind="reach" />
          </span>
          <button type="button" className="btn-ui btn-ui-primary" onClick={refreshAll}>
            Refresh
          </button>
        </>
      }
    >
      <PageShell padding="compact" className="flex w-full min-w-0 flex-col gap-4">
        {viewTab !== 'environments' && viewTab !== 'console' && (
          <PageHeader
            title={VIEW_TITLES[viewTab]}
            description="L0 read-only probes — collapse the sidebar to use full width for tables and topology."
          />
        )}

        {envQuery.data && viewTab !== 'environments' && viewTab !== 'console' && (
          <EnvironmentStrip
            environments={envQuery.data}
            selected={envFilter}
            onSelect={id => {
              setEnvFilter(id)
              if (id === 'all') setViewTab('matrix')
            }}
          />
        )}

        {viewTab !== 'environments' && (
          <SegmentControl
            ariaLabel="Console view"
            value={viewTab}
            onChange={v => setViewTab(v as ViewTab)}
            options={[
              { value: 'topology', label: 'Topology' },
              { value: 'matrix', label: 'Matrix' },
              { value: 'environments', label: 'Environments' },
              { value: 'console', label: 'Console' },
            ]}
          />
        )}

        {viewTab === 'console' && <ServerConsolePage />}

        {viewTab === 'environments' && (
          <>
            <PageHeader
              title={VIEW_TITLES.environments}
              description="Hardware, CI/CD, K3s target, and trade Dev/Prod catalog."
            />
            <EnvironmentsPage />
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
