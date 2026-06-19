import { memo, useEffect, useMemo } from 'react'
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  useReactFlow,
  type NodeProps,
  Handle,
  Position,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { DenseTag, type DenseTagVariant } from '@bifrost/ui'
import type { GitOpsAppsResponse, OpsContextResponse, StackAddonsResponse } from '@/api/types'
import { DeliveryBrandIcon } from '@/components/delivery/DeliveryBrandIcon'
import { DeliveryBrandLabel } from '@/components/delivery/DeliveryBrandLabel'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  buildDeliveryGraph,
  deliveryStatusClass,
  type DeliveryNodeData,
} from '@/lib/delivery/buildDeliveryGraph'
import { hasDeliveryBrandIcon } from '@/lib/delivery/deliveryStackIcons'

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
  const showBrand = hasDeliveryBrandIcon(d.id)
  return (
    <div
      className={[
        'pipeline-node delivery-node',
        deliveryStatusClass(d.status),
        d.lane === 'decision' ? 'delivery-node--decision' : '',
        d.selected ? 'pipeline-node--selected' : '',
        showBrand ? 'delivery-node--branded' : '',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      <Handle type="target" position={Position.Left} className="pipeline-handle" />
      <div className="pipeline-node-id font-mono-tabular">{d.id}</div>
      <div className="pipeline-node-label delivery-node-label">
        {showBrand && <DeliveryBrandIcon id={d.id} variant="scope" />}
        <span>{d.label}</span>
      </div>
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

/** Re-fit when node layout changes (gitops probe, milestones). */
function FitViewOnChange({ nodeCount }: { nodeCount: number }) {
  const { fitView } = useReactFlow()
  useEffect(() => {
    const t = window.setTimeout(() => {
      void fitView({ padding: 0.18, minZoom: 0.45, maxZoom: 1.1, duration: 150 })
    }, 0)
    return () => window.clearTimeout(t)
  }, [nodeCount, fitView])
  return null
}

interface DeliveryFlowProps {
  context: OpsContextResponse
  selectionId?: string | null
  clusterReachOk?: boolean
  gitops?: GitOpsAppsResponse
  stack?: StackAddonsResponse
  onSelectNode?: (id: string) => void
}

export function DeliveryFlow({
  context,
  selectionId,
  clusterReachOk,
  gitops,
  stack,
  onSelectNode,
}: DeliveryFlowProps) {
  const { nodes, edges } = useMemo(
    () => buildDeliveryGraph(context, selectionId, clusterReachOk, gitops, stack),
    [context, selectionId, clusterReachOk, gitops, stack],
  )

  return (
    <OpsSection
      title="CI/CD dual track"
      description={
        <>
          Near-term Mac runner vs target GitOps on K3s. Target lane uses live Argo CD application status from the cluster.
        </>
      }
      headerExtra={
        <div className="mt-2 flex flex-col gap-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          <span>
            <strong className="text-[var(--foreground)]">Now</strong> — Mac CI → release gate → compose
          </span>
          <span className="delivery-lane-legend">
            <strong className="text-[var(--foreground)]">Target</strong>
            <span className="delivery-lane-legend__track">
              <DeliveryBrandLabel id="gitea">Gitea</DeliveryBrandLabel>
              <span aria-hidden>→</span>
              <DeliveryBrandLabel id="tekton">Tekton</DeliveryBrandLabel>
              <span aria-hidden>→</span>
              <DeliveryBrandLabel id="registry">Registry</DeliveryBrandLabel>
              <span aria-hidden>→</span>
              <DeliveryBrandLabel id="argocd">Argo CD</DeliveryBrandLabel>
              <span aria-hidden>→</span>
              <DeliveryBrandLabel id="k3s-bifrost">K3s</DeliveryBrandLabel>
            </span>
          </span>
        </div>
      }
      bodyPadding="none"
      overflow="visible"
      className="delivery-flow-section"
    >
      <div className="delivery-flow-host pipeline-flow-host">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.18, minZoom: 0.45, maxZoom: 1.1 }}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={false}
          panOnScroll
          zoomOnScroll
          minZoom={0.35}
          maxZoom={1.25}
          onNodeClick={onSelectNode != null ? (_, node) => onSelectNode(node.id) : undefined}
          proOptions={{ hideAttribution: true }}
        >
          <FitViewOnChange nodeCount={nodes.length} />
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
    </OpsSection>
  )
}
