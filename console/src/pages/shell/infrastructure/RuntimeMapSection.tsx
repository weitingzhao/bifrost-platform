import { useQuery } from '@tanstack/react-query'
import { fetchCluster } from '@/api/cluster'
import { fetchContext, fetchMatrix, fetchSelfHealth, fetchTopology, isAllMatrices } from '@/api/core'
import type { MatrixResponse } from '@/api/matrixTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { RuntimeMapPage } from '@/pages/RuntimeMapPage'

function matrixForViewer(
  data: MatrixResponse | { matrices: MatrixResponse[] } | undefined,
  viewerEnv: string,
): MatrixResponse | undefined {
  if (data == null) return undefined
  if (isAllMatrices(data)) return data.matrices.find(row => row.environment === viewerEnv)
  return data
}

/** Hardware and software topology for the console seat. No environment picker. */
export function RuntimeMapSection() {
  const healthQ = useQuery({
    queryKey: ['platform', 'self-health', 'runtime-map'],
    queryFn: fetchSelfHealth,
    refetchInterval: 60_000,
  })
  const viewerEnv = healthQ.data?.viewer_env?.trim() ?? ''
  const topologyQ = useQuery({
    queryKey: ['topology', 'viewer', viewerEnv],
    queryFn: () => fetchTopology(viewerEnv),
    enabled: viewerEnv !== '',
    refetchInterval: 60_000,
  })
  const matrixQ = useQuery({
    queryKey: ['matrix', 'viewer', viewerEnv],
    queryFn: () => fetchMatrix(viewerEnv),
    enabled: viewerEnv !== '',
    refetchInterval: 60_000,
  })
  const contextQ = useQuery({
    queryKey: ['ops-context'],
    queryFn: fetchContext,
    staleTime: 60_000,
  })
  const clusterQ = useQuery({
    queryKey: ['cluster', 'summary', 'runtime-map'],
    queryFn: fetchCluster,
    refetchInterval: 60_000,
  })

  const error =
    (topologyQ.error instanceof Error ? topologyQ.error : null) ??
    (matrixQ.error instanceof Error ? matrixQ.error : null)

  return (
    <OpsSection
      title="Runtime map"
      description={
        viewerEnv !== ''
          ? `Hardware and software topology for viewer_env ${viewerEnv}.`
          : 'Hardware and software topology for the console seat.'
      }
      bodyPadding="default"
      overflow="visible"
    >
      {viewerEnv === '' ? (
        <p className="m-0 text-sm text-muted-foreground">Waiting for viewer_env from self-health.</p>
      ) : (
        <RuntimeMapPage
          topology={topologyQ.data}
          matrix={matrixForViewer(matrixQ.data, viewerEnv)}
          context={contextQ.data}
          clusterSummary={clusterQ.data}
          isLoading={topologyQ.isLoading || matrixQ.isLoading || contextQ.isLoading}
          error={error}
        />
      )}
    </OpsSection>
  )
}
