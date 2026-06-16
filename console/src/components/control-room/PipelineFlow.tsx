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
import { DenseTag } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
import { milestoneStatusVariant } from '@/components/FocusStrip'
import {
  buildPipelineGraph,
  type MilestoneNodeData,
} from '@/lib/control-room/buildPipelineGraph'

function statusBorderClass(status: string): string {
  if (status === 'BLOCKED_ON') return 'pipeline-node--blocked'
  if (status === 'IN_PROGRESS') return 'pipeline-node--active'
  if (status === 'SIGNED' || status === 'CLOSED' || status === 'DEPLOYED') return 'pipeline-node--done'
  return 'pipeline-node--pending'
}

function MilestoneNode({ data }: NodeProps) {
  const d = data as MilestoneNodeData
  const { milestone, selected, isDecision } = d
  const label = milestone.label ?? milestone.id
  const shortId = isDecision ? d.decisionLabel ?? milestone.id : milestone.id

  return (
    <div
      className={[
        'pipeline-node',
        statusBorderClass(milestone.status),
        selected ? 'pipeline-node--selected' : '',
        isDecision ? 'pipeline-node--decision' : '',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      <Handle type="target" position={Position.Left} className="pipeline-handle" />
      <div className="pipeline-node-id font-mono-tabular">{shortId}</div>
      <div className="pipeline-node-label">{label}</div>
      <DenseTag variant={milestoneStatusVariant(milestone.status)}>{milestone.status}</DenseTag>
      <Handle type="source" position={Position.Right} className="pipeline-handle" />
    </div>
  )
}

const nodeTypes = { milestoneNode: memo(MilestoneNode) }

interface PipelineFlowProps {
  context: OpsContextResponse
  selectionId?: string | null
  onSelectMilestone: (id: string) => void
}

export function PipelineFlow({ context, selectionId, onSelectMilestone }: PipelineFlowProps) {
  const { nodes, edges } = useMemo(
    () => buildPipelineGraph(context, selectionId),
    [context, selectionId],
  )

  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="border-b border-[var(--border)] px-3 py-2">
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Program milestone spine
        </h3>
        <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Migration and cutover milestones — not Tekton/CI build runs. See Delivery for CI/CD. Parallel
          lane shows K3s track (D5).
        </p>
      </header>
      <div className="pipeline-flow-host min-h-[320px]">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={false}
          panOnScroll
          zoomOnScroll
          onNodeClick={(_, node) => onSelectMilestone(node.id)}
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={16} size={1} color="var(--border)" />
          <Controls showInteractive={false} className="pipeline-controls" />
          <MiniMap
            className="pipeline-minimap"
            nodeColor={n => {
              const st = (n.data as MilestoneNodeData).milestone.status
              if (st === 'BLOCKED_ON') return 'var(--color-lamp-red)'
              if (st === 'IN_PROGRESS') return 'var(--primary)'
              if (st === 'SIGNED' || st === 'CLOSED') return 'var(--color-lamp-green)'
              return 'var(--color-lamp-gray)'
            }}
            maskColor="color-mix(in srgb, var(--background) 75%, transparent)"
          />
        </ReactFlow>
      </div>
    </section>
  )
}
