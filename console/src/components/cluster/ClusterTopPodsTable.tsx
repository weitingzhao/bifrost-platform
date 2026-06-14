import type { ClusterMetricsResponse, ClusterPodMetric } from '@/api/types'

interface ClusterTopPodsTableProps {
  metrics: ClusterMetricsResponse | undefined
  isLoading: boolean
}

export function ClusterTopPodsTable({ metrics, isLoading }: ClusterTopPodsTableProps) {
  const pods: ClusterPodMetric[] = metrics?.top_pods ?? []
  const available = metrics?.metrics_server_available === true

  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="flex items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
        <h2 className="m-0 text-sm font-semibold">Top pods (Bifrost namespaces)</h2>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {isLoading ? '…' : `${pods.length} pods`}
        </span>
      </header>
      <div className="dense-table-scroll">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Namespace</th>
              <th>Pod</th>
              <th>CPU</th>
              <th>Memory</th>
            </tr>
          </thead>
          <tbody>
            {!available && !isLoading ? (
              <tr>
                <td colSpan={4} className="text-[var(--muted-foreground)]">
                  Install metrics-server to see live usage (kubectl top pods).
                </td>
              </tr>
            ) : pods.length === 0 ? (
              <tr>
                <td colSpan={4} className="text-[var(--muted-foreground)]">
                  {isLoading ? 'Loading…' : 'No pod metrics in Bifrost namespaces'}
                </td>
              </tr>
            ) : (
              pods.map(pod => (
                <tr key={`${pod.namespace}/${pod.name}`}>
                  <td className="font-mono-tabular">{pod.namespace}</td>
                  <td className="font-mono-tabular">{pod.name}</td>
                  <td className="font-mono-tabular">{pod.cpu}</td>
                  <td className="font-mono-tabular">{pod.memory}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
