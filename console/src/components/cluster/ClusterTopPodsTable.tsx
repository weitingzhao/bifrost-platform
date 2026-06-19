import { DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import type { ClusterMetricsResponse, ClusterPodMetric } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'

interface ClusterTopPodsTableProps {
  metrics: ClusterMetricsResponse | undefined
  isLoading: boolean
}

export function ClusterTopPodsTable({ metrics, isLoading }: ClusterTopPodsTableProps) {
  const pods: ClusterPodMetric[] = metrics?.top_pods ?? []
  const available = metrics?.metrics_server_available === true
  const qc = useQueryClient()
  const metricsFetching = useIsFetching({ queryKey: ['cluster', 'metrics'] }) > 0

  return (
    <OpsSection
      title="Top pods (Bifrost namespaces)"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {isLoading ? '…' : `${pods.length} pods`}
          </span>
          <SectionRefreshButton
            isFetching={metricsFetching || isLoading}
            onClick={() => void qc.invalidateQueries({ queryKey: ['cluster', 'metrics'] })}
          />
        </div>
      }
      bodyPadding="none"
      overflow="hidden"
    >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Namespace</DenseTableHead>
            <DenseTableHead>Pod</DenseTableHead>
            <DenseTableHead>CPU</DenseTableHead>
            <DenseTableHead>Memory</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {!available && !isLoading ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                Install metrics-server to see live usage (kubectl top pods).
              </DenseTableCell>
            </DenseTableRow>
          ) : pods.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                {isLoading ? 'Loading…' : 'No pod metrics in Bifrost namespaces'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            pods.map(pod => (
              <DenseTableRow key={`${pod.namespace}/${pod.name}`}>
                <DenseTableCell className="font-mono-tabular">{pod.namespace}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{pod.name}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{pod.cpu}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{pod.memory}</DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
