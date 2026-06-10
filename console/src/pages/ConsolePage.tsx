import { useQuery, useQueryClient } from '@tanstack/react-query'
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
import { StatusLamp } from '@/components/StatusLamp'
import { TopologyDiagram } from '@/components/TopologyDiagram'

type ViewTab = 'topology' | 'matrix'

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

  return (
    <div className="min-h-screen flex flex-col">
      <header className="border-b border-[var(--border)] bg-[var(--card)] px-4 py-3">
        <div className="max-w-6xl mx-auto flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="m-0 text-base font-semibold tracking-tight">Bifrost Platform Console</h1>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Topology &amp; connectivity matrix (L0 read-only)
            </p>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Platform API{' '}
              <StatusLamp value={healthQuery.data ? 'ok' : 'fail'} kind="reach" />
            </span>
            <button type="button" className="btn btn-primary" onClick={refreshAll}>
              Refresh
            </button>
          </div>
        </div>
      </header>

      <main className="flex-1 px-4 py-4">
        <div className="max-w-6xl mx-auto flex flex-col gap-4">
          {envQuery.data && (
            <EnvironmentStrip
              environments={envQuery.data}
              selected={envFilter}
              onSelect={id => {
                setEnvFilter(id)
                if (id === 'all') setViewTab('matrix')
              }}
            />
          )}

          <div className="segment w-fit" role="tablist" aria-label="Console view">
            <button
              type="button"
              role="tab"
              data-active={viewTab === 'topology'}
              onClick={() => setViewTab('topology')}
            >
              Topology
            </button>
            <button
              type="button"
              role="tab"
              data-active={viewTab === 'matrix'}
              onClick={() => setViewTab('matrix')}
            >
              Matrix
            </button>
          </div>

          {viewTab === 'topology' && envFilter === 'all' && (
            <p className="text-[var(--muted-foreground)] text-[var(--text-dense-meta)]">
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
            <p className="lamp-fail">
              Failed to load matrix: {(matrixQuery.error as Error).message}
            </p>
          )}
          {viewTab === 'matrix' &&
            matrices.map(m => <MatrixTable key={m.environment} matrix={m} />)}
        </div>
      </main>
    </div>
  )
}
