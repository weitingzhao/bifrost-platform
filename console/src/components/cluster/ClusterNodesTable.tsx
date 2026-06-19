import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
} from '@bifrost/ui'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import type { ClusterNode } from '@/api/types'
import { ConsoleHostIpLabel } from '@/components/ConsoleHostIpLabel'
import { NodeArchLabel } from '@/components/cluster/NodeArchLabel'
import { NodeResourceCell } from '@/components/cluster/NodeResourceCell'
import { NodeVersionInfo } from '@/components/cluster/NodeVersionInfo'
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

const NODE_COL_COUNT = 9

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
        <colgroup>
          <col style={{ width: '18%' }} />
          <col style={{ width: '16%' }} />
          <col style={{ width: '8%' }} />
          <col style={{ width: '8%' }} />
          <col style={{ width: '8%' }} />
          <col style={{ width: '9%' }} />
          <col style={{ width: '9%' }} />
          <col style={{ width: '9%' }} />
          <col style={{ width: '11%' }} />
        </colgroup>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Name</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Arch</DenseTableHead>
            <DenseTableHead>Workload</DenseTableHead>
            <DenseTableHead>Roles</DenseTableHead>
            <DenseTableHead title="Allocatable cores · usage % from metrics-server">CPU</DenseTableHead>
            <DenseTableHead title="Allocatable memory · usage % from metrics-server">MEM</DenseTableHead>
            <DenseTableHead title="Ephemeral-storage allocatable for pods; disk fill % is not exposed by metrics-server">
              Storage
            </DenseTableHead>
            <DenseTableHead>Internal IP</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {nodes.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={NODE_COL_COUNT} className="text-[var(--muted-foreground)]">
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
                <DenseTableCell className="font-mono-tabular !whitespace-normal">
                  <button
                    type="button"
                    className="block max-w-full truncate text-left font-mono-tabular text-[var(--primary)] underline-offset-2 hover:underline"
                    title={node.name}
                    onClick={event => {
                      event.stopPropagation()
                      onSelectNode?.(node)
                    }}
                  >
                    {node.name}
                    {selectedNode === node.name ? ' ·' : ''}
                  </button>
                </DenseTableCell>
                <DenseTableCell className="!whitespace-normal">
                  <span className="inline-flex flex-wrap items-center gap-x-1 gap-y-0.5">
                    <StatusLamp value={node.reachability} kind="reach" />
                    <span className="font-mono-tabular">{node.status}</span>
                    {node.unschedulable ? (
                      <span className="text-dense-caption text-[var(--muted-foreground)]">cordoned</span>
                    ) : null}
                  </span>
                </DenseTableCell>
                <DenseTableCell>
                  <span className="inline-flex items-center gap-0.5">
                    <NodeArchLabel arch={node.architecture} showTooltip={false} />
                    <NodeVersionInfo version={node.version} />
                  </span>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.workload_label || '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.roles}</DenseTableCell>
                <DenseTableCell>
                  <NodeResourceCell
                    alloc={node.cpu_allocatable}
                    pct={node.cpu_usage_percent}
                    reach={node.cpu_reachability}
                  />
                </DenseTableCell>
                <DenseTableCell>
                  <NodeResourceCell
                    alloc={node.memory_allocatable}
                    pct={node.memory_usage_percent}
                    reach={node.memory_reachability}
                  />
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{node.storage_allocatable ?? '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">
                  {node.internal_ip ? (
                    <ConsoleHostIpLabel ip={node.internal_ip} compact />
                  ) : (
                    '—'
                  )}
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
