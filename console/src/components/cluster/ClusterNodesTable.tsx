import type { ClusterNode } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'

interface ClusterNodesTableProps {
  nodes: ClusterNode[]
  isLoading: boolean
}

export function ClusterNodesTable({ nodes, isLoading }: ClusterNodesTableProps) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="flex items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
        <h2 className="m-0 text-sm font-semibold">Nodes</h2>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {isLoading ? '…' : `${nodes.length} nodes`}
        </span>
      </header>
      <div className="dense-table-scroll">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Status</th>
              <th>Roles</th>
              <th>Version</th>
              <th>Internal IP</th>
            </tr>
          </thead>
          <tbody>
            {nodes.length === 0 ? (
              <tr>
                <td colSpan={5} className="text-[var(--muted-foreground)]">
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
