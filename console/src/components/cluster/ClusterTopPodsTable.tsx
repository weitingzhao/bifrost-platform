import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
} from '@bifrost/ui'
import type { ClusterMetricsResponse, ClusterPodMetric } from '@/api/clusterTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { cn } from '@/lib/cn'

interface ClusterTopPodsTableProps {
  metrics: ClusterMetricsResponse | undefined
  isLoading: boolean
  selectedPodKey?: string | null
  onSelectPod?: (pod: ClusterPodMetric) => void
}

/** Dense display: Gi/Mi/Ki → G/M/K (CPU millicores unchanged). */
function formatDenseResource(raw: string): string {
  return raw
    .replace(/([0-9.]+)Gi\b/gi, '$1G')
    .replace(/([0-9.]+)Mi\b/gi, '$1M')
    .replace(/([0-9.]+)Ki\b/gi, '$1K')
}

function podKey(pod: ClusterPodMetric): string {
  return `${pod.namespace}/${pod.name}`
}

export function ClusterTopPodsTable({
  metrics,
  isLoading,
  selectedPodKey = null,
  onSelectPod,
}: ClusterTopPodsTableProps) {
  const pods: ClusterPodMetric[] = metrics?.top_pods ?? []
  const available = metrics?.metrics_server_available === true
  const qc = useQueryClient()
  const metricsFetching = useIsFetching({ queryKey: ['cluster', 'metrics'] }) > 0

  return (
    <OpsSection
      className="cluster-global-top-pods__section"
      title="Top pods by usage"
      description="Global · all Bifrost namespaces"
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
      <DenseDataTable tableClassName="cluster-top-pods-table" scrollX={false}>
        <colgroup>
          <col className="cluster-top-pods-table__col-ns" />
          <col className="cluster-top-pods-table__col-pod" />
          <col className="cluster-top-pods-table__col-cpu" />
          <col className="cluster-top-pods-table__col-mem" />
        </colgroup>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>NS</DenseTableHead>
            <DenseTableHead>Pod</DenseTableHead>
            <DenseTableHead className="text-right">CPU</DenseTableHead>
            <DenseTableHead className="text-right">Mem</DenseTableHead>
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
            pods.map(pod => {
              const key = podKey(pod)
              const selected = selectedPodKey === key
              return (
                <DenseTableRow
                  key={key}
                  className={cn(selected && 'cluster-top-pods-table__row--selected')}
                  data-selected={selected ? 'true' : undefined}
                >
                  <DenseTableCell className="font-mono-tabular whitespace-nowrap" title={pod.namespace}>
                    {pod.namespace}
                  </DenseTableCell>
                  <DenseTableCell
                    className="cluster-top-pods-table__pod-cell font-mono-tabular"
                    title={onSelectPod ? `${pod.name} — open details` : pod.name}
                    role={onSelectPod ? 'button' : undefined}
                    tabIndex={onSelectPod ? 0 : undefined}
                    onClick={
                      onSelectPod
                        ? e => {
                            e.stopPropagation()
                            onSelectPod(pod)
                          }
                        : undefined
                    }
                    onKeyDown={
                      onSelectPod
                        ? e => {
                            if (e.key === 'Enter' || e.key === ' ') {
                              e.preventDefault()
                              onSelectPod(pod)
                            }
                          }
                        : undefined
                    }
                  >
                    {pod.name}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular whitespace-nowrap text-right">
                    {formatDenseResource(pod.cpu)}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular whitespace-nowrap text-right">
                    {formatDenseResource(pod.memory)}
                  </DenseTableCell>
                </DenseTableRow>
              )
            })
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
