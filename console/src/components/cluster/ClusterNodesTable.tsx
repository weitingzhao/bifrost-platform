import type { ClusterNode } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'

interface ClusterNodesTableProps {
  nodes: ClusterNode[]
  isLoading: boolean
  isFetching?: boolean
  metricsAvailable?: boolean
}

function pctCell(value: number | undefined): string {
  if (value == null) return '—'
  return `${value.toFixed(1)}%`
}

export function ClusterNodesTable({
  nodes,
  isLoading,
  isFetching = false,
  metricsAvailable,
}: ClusterNodesTableProps) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="flex items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
        <h2 className="m-0 text-sm font-semibold">Nodes</h2>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {isLoading ? '…' : `${nodes.length} nodes`}
          {isFetching && !isLoading ? ' · updating…' : ''}
          {metricsAvailable === false ? ' · usage n/a' : ''}
        </span>
      </header>
      <div className="dense-table-scroll">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>Roles</th>
              <th>CPU alloc</th>
              <th>Mem alloc</th>
              <th>Storage</th>
              <th>CPU %</th>
              <th>Mem %</th>
              <th>Version</th>
              <th>Internal IP</th>
            </tr>
          </thead>
          <tbody>
            {nodes.length === 0 ? (
              <tr>
                <td colSpan={10} className="text-[var(--muted-foreground)]">
                  {isLoading ? 'Loading…' : 'No nodes (cluster unreachable or empty)'}
                </td>
              </tr>
            ) : (
              nodes.map(node => (
                <tr key={node.name}>
                  <td className="font-mono-tabular">{node.name}</td>
                  <td>
                    <StatusLamp value={node.reachability} kind="reach" />{' '}
                    <span className="font-mono-tabular">{node.status}</span>
                  </td>
                  <td className="font-mono-tabular">{node.roles}</td>
                  <td className="font-mono-tabular">{node.cpu_allocatable ?? '—'}</td>
                  <td className="font-mono-tabular">{node.memory_allocatable ?? '—'}</td>
                  <td className="font-mono-tabular">{node.storage_allocatable ?? '—'}</td>
                  <td className="font-mono-tabular">
                    {node.cpu_usage_percent != null ? (
                      <>
                        <StatusLamp value={node.cpu_reachability ?? 'ok'} kind="reach" />{' '}
                        {pctCell(node.cpu_usage_percent)}
                      </>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="font-mono-tabular">
                    {node.memory_usage_percent != null ? (
                      <>
                        <StatusLamp value={node.memory_reachability ?? 'ok'} kind="reach" />{' '}
                        {pctCell(node.memory_usage_percent)}
                      </>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="font-mono-tabular text-[var(--text-dense-meta)]">
                    {node.version}
                  </td>
                  <td className="font-mono-tabular">{node.internal_ip || '—'}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
