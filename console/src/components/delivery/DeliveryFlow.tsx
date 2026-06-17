import { memo, useMemo } from 'react'
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  type NodeProps,
  Handle,
  Position,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { DenseTag, type DenseTagVariant } from '@bifrost/ui'
import type { GitOpsAppsResponse, OpsContextResponse } from '@/api/types'
import {
  buildDeliveryGraph,
  deliveryStatusClass,
  type DeliveryNodeData,
} from '@/lib/delivery/buildDeliveryGraph'

function statusBadgeLabel(status: DeliveryNodeData['status']): string {
  if (status === 'live') return 'live'
  if (status === 'blocked') return 'blocked'
  if (status === 'degraded') return 'degraded'
  return 'planned'
}

function statusBadgeVariant(status: DeliveryNodeData['status']): DenseTagVariant {
  if (status === 'live') return 'success'
  if (status === 'blocked') return 'danger'
  if (status === 'degraded') return 'warning'
  return 'neutral'
}

function DeliveryNode({ data }: NodeProps) {
  const d = data as DeliveryNodeData
  return (
    <div
      className={[
        'pipeline-node delivery-node',
        deliveryStatusClass(d.status),
        d.lane === 'decision' ? 'delivery-node--decision' : '',
        d.selected ? 'pipeline-node--selected' : '',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      <Handle type="target" position={Position.Left} className="pipeline-handle" />
      <div className="pipeline-node-id font-mono-tabular">{d.id}</div>
      <div className="pipeline-node-label">{d.label}</div>
      {d.subtitle != null && (
        <div className="delivery-node-subtitle text-[var(--muted-foreground)]">{d.subtitle}</div>
      )}
      <DenseTag variant={statusBadgeVariant(d.status)} className={`delivery-node-badge delivery-node-badge--${d.status}`}>
        {statusBadgeLabel(d.status)}
      </DenseTag>
      <Handle type="source" position={Position.Right} className="pipeline-handle" />
    </div>
  )
}

const nodeTypes = { deliveryNode: memo(DeliveryNode) }

interface DeliveryFlowProps {
  context: OpsContextResponse
  selectionId?: string | null
  clusterReachOk?: boolean
  gitops?: GitOpsAppsResponse
  onSelectNode?: (id: string) => void
}

export function DeliveryFlow({
  context,
  selectionId,
  clusterReachOk,
  gitops,
  onSelectNode,
}: DeliveryFlowProps) {
  const { nodes, edges } = useMemo(
    () => buildDeliveryGraph(context, selectionId, clusterReachOk, gitops),
    [context, selectionId, clusterReachOk, gitops],
  )

  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="border-b border-[var(--border)] px-3 py-2">
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          CI/CD dual track
        </h3>
        <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Near-term Mac runner vs target GitOps on K3s. Target lane uses live{' '}
          <span className="font-mono-tabular">GET /api/v1/gitops/apps</span> for Argo CD.
        </p>
      </header>
      <div className="delivery-flow-host pipeline-flow-host min-h-[360px]">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.25 }}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={false}
          panOnScroll
          zoomOnScroll
          onNodeClick={onSelectNode != null ? (_, node) => onSelectNode(node.id) : undefined}
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={16} size={1} color="var(--border)" />
          <Controls showInteractive={false} className="pipeline-controls" />
          <MiniMap
            className="pipeline-minimap"
            nodeColor={n => {
              const st = (n.data as DeliveryNodeData).status
              if (st === 'blocked') return 'var(--color-lamp-red)'
              if (st === 'live') return 'var(--color-lamp-green)'
              if (st === 'degraded') return 'var(--color-lamp-yellow)'
              return 'var(--color-lamp-gray)'
            }}
            maskColor="color-mix(in srgb, var(--background) 75%, transparent)"
          />
        </ReactFlow>
      </div>
    </section>
  )
}
