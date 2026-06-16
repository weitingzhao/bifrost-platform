import { DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
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
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Name</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Roles</DenseTableHead>
            <DenseTableHead>CPU alloc</DenseTableHead>
            <DenseTableHead>Mem alloc</DenseTableHead>
            <DenseTableHead>Storage</DenseTableHead>
            <DenseTableHead>CPU %</DenseTableHead>
            <DenseTableHead>Mem %</DenseTableHead>
            <DenseTableHead>Version</DenseTableHead>
            <DenseTableHead>Internal IP</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {nodes.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={10} className="text-[var(--muted-foreground)]">
                {isLoading ? 'Loading…' : 'No nodes (cluster unreachable or empty)'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            nodes.map(node => (
              <DenseTableRow key={node.name}>
                <DenseTableCell className="font-mono-tabular">{node.name}</DenseTableCell>
                <DenseTableCell>
                  <StatusLamp value={node.reachability} kind="reach" />{' '}
                  <span className="font-mono-tabular">{node.status}</span>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.roles}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.cpu_allocatable ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.memory_allocatable ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.storage_allocatable ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">
                  {node.cpu_usage_percent != null ? (
                    <>
                      <StatusLamp value={node.cpu_reachability ?? 'ok'} kind="reach" />{' '}
                      {pctCell(node.cpu_usage_percent)}
                    </>
                  ) : (
                    '—'
                  )}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">
                  {node.memory_usage_percent != null ? (
                    <>
                      <StatusLamp value={node.memory_reachability ?? 'ok'} kind="reach" />{' '}
                      {pctCell(node.memory_usage_percent)}
                    </>
                  ) : (
                    '—'
                  )}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">
                  {node.version}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.internal_ip || '—'}</DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </section>
  )
}
