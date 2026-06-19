import { DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import type { ClusterNode } from '@/api/types'
import { NodeArchLabel } from '@/components/cluster/NodeArchLabel'
import { StatusLamp } from '@/components/StatusLamp'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'

interface ClusterNodesTableProps {
  nodes: ClusterNode[]
  isLoading: boolean
  isFetching?: boolean
  metricsAvailable?: boolean
  selectedNode?: string | null
  onSelectNode?: (node: ClusterNode) => void
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
  selectedNode = null,
  onSelectNode,
}: ClusterNodesTableProps) {
  const qc = useQueryClient()
  const nodesFetching = useIsFetching({ queryKey: ['cluster', 'nodes'] }) > 0

  const refreshNodes = () => {
    void qc.invalidateQueries({ queryKey: ['cluster', 'nodes'] })
    void qc.invalidateQueries({ queryKey: ['cluster', 'metrics'] })
  }

  return (
    <OpsSection
      title="Nodes"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {isLoading ? '…' : `${nodes.length} nodes`}
            {isFetching && !isLoading ? ' · updating…' : ''}
            {metricsAvailable === false ? ' · usage n/a' : ''}
          </span>
          <SectionRefreshButton
            isFetching={nodesFetching || (isFetching && !isLoading)}
            onClick={refreshNodes}
          />
        </div>
      }
      bodyPadding="none"
      overflow="hidden"
    >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Name</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Arch</DenseTableHead>
            <DenseTableHead>Workload</DenseTableHead>
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
              <DenseTableCell colSpan={12} className="text-[var(--muted-foreground)]">
                {isLoading ? 'Loading…' : 'No nodes (cluster unreachable or empty)'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            nodes.map(node => (
              <DenseTableRow
                key={node.name}
                className="cursor-pointer hover:bg-[var(--secondary)]/60"
                onClick={onSelectNode != null ? () => onSelectNode(node) : undefined}
              >
                <DenseTableCell className="font-mono-tabular">
                  <button
                    type="button"
                    className="text-left font-mono-tabular text-[var(--primary)] underline-offset-2 hover:underline"
                    onClick={event => {
                      event.stopPropagation()
                      onSelectNode?.(node)
                    }}
                  >
                    {node.name}
                    {selectedNode === node.name ? ' ·' : ''}
                  </button>
                </DenseTableCell>
                <DenseTableCell>
                  <StatusLamp value={node.reachability} kind="reach" />{' '}
                  <span className="font-mono-tabular">{node.status}</span>
                  {node.unschedulable ? (
                    <span className="ml-1 text-dense-caption text-[var(--muted-foreground)]">
                      cordoned
                    </span>
                  ) : null}
                </DenseTableCell>
                <DenseTableCell>
                  <NodeArchLabel arch={node.architecture} />
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.workload_label || '—'}</DenseTableCell>
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
    </OpsSection>
  )
}
