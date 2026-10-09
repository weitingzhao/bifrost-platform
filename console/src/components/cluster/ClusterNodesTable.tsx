import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
  DenseTag,
} from '@bifrost/ui'
import { useIsFetching, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import type { ClusterNode } from '@/api/clusterTypes'
import { fetchPromQL } from '@/api/telemetry'
import { rebootPendingLabel } from '@/components/cluster/rebootPending'
import { ConsoleHostIpLabel } from '@/components/ConsoleHostIpLabel'
import { NodeArchLabel } from '@/components/cluster/NodeArchLabel'
import { NodeCapabilitiesCell } from '@/components/cluster/NodeCapabilitiesCell'
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

const NODE_COL_COUNT = 10

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
  const rebootRequiredQ = useQuery({
    queryKey: ['telemetry', 'promql', 'bifrost_node_reboot_required'],
    queryFn: () => fetchPromQL('bifrost_node_reboot_required == 1'),
    refetchInterval: 60_000,
    retry: false,
  })
  const rebootSinceQ = useQuery({
    queryKey: ['telemetry', 'promql', 'bifrost_node_reboot_required_since_seconds'],
    queryFn: () => fetchPromQL('bifrost_node_reboot_required_since_seconds'),
    refetchInterval: 60_000,
    retry: false,
  })
  const rebootSamples = useMemo(() => {
    if (rebootRequiredQ.isError || rebootSinceQ.isError) return null
    if (rebootRequiredQ.data == null || rebootSinceQ.data == null) return null
    return { required: rebootRequiredQ.data.points, since: rebootSinceQ.data.points }
  }, [rebootRequiredQ.isError, rebootRequiredQ.data, rebootSinceQ.isError, rebootSinceQ.data])

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
          <col style={{ width: '16%' }} />
          <col style={{ width: '14%' }} />
          <col style={{ width: '7%' }} />
          <col style={{ width: '7%' }} />
          <col style={{ width: '12%' }} />
          <col style={{ width: '7%' }} />
          <col style={{ width: '8%' }} />
          <col style={{ width: '8%' }} />
          <col style={{ width: '8%' }} />
          <col style={{ width: '10%' }} />
        </colgroup>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Name</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Arch</DenseTableHead>
            <DenseTableHead>Workload</DenseTableHead>
            <DenseTableHead title="Host prep derived from node labels (storage.nfs/client, workload=gpu, …)">
              Capabilities
            </DenseTableHead>
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
            nodes.map(node => {
              const reboot =
                rebootSamples == null
                  ? null
                  : rebootPendingLabel(node.name, rebootSamples.required, rebootSamples.since)
              return (
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
                    {/* Elastic standby + NotReady is expected off — force neutral lamp. */}
                    <StatusLamp
                      value={node.elastic_mode === 'standby' ? 'unknown' : node.reachability}
                      kind="reach"
                    />
                    <span
                      className={
                        node.elastic_mode === 'standby'
                          ? 'font-mono-tabular text-[var(--muted-foreground)]'
                          : 'font-mono-tabular'
                      }
                    >
                      {node.elastic_mode === 'standby' && node.status !== 'Ready'
                        ? 'NotReady'
                        : node.status}
                    </span>
                    {node.unschedulable ? (
                      <span className="text-dense-caption text-[var(--muted-foreground)]">cordoned</span>
                    ) : null}
                    {node.elastic_mode === 'standby' ? (
                      <DenseTag variant="neutral">standby</DenseTag>
                    ) : null}
                    {node.elastic_mode === 'degraded' ? (
                      <DenseTag variant="warning">wake failed</DenseTag>
                    ) : null}
                    {reboot != null ? (
                      <span className="text-dense-caption text-[var(--muted-foreground)]">{reboot}</span>
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
                <DenseTableCell className="!whitespace-normal">
                  <NodeCapabilitiesCell capabilities={node.capabilities} />
                </DenseTableCell>
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
              )
            })
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
