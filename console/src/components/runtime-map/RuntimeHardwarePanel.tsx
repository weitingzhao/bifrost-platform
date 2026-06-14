import { useState } from 'react'
import type { TopologyResponse } from '@/api/types'
import { InfraMapCanvas } from '@/components/runtime-map/InfraMapCanvas'
import type { LayoutMode } from '@/lib/runtime-map/infraMapLayout'
import type { RoleView, StackChipModel } from '@/lib/runtime-map/roleComponentRegistry'

interface RuntimeHardwarePanelProps {
  data: TopologyResponse
  clusterLive?: boolean
  onOpenCluster?: () => void
  selectedNodeId: string | null
  selectedEdgeId: string | null
  highlightNodeIds: string[]
  highlightEdgeIds?: string[]
  highlightChipIds?: string[]
  selectedChipId?: string | null
  onSelectNode: (nodeId: string) => void
  onSelectEdge: (edgeId: string) => void
  onSelectChip: (chip: StackChipModel, nodeId: string) => void
}

export function RuntimeHardwarePanel({
  data,
  clusterLive,
  onOpenCluster,
  selectedNodeId,
  selectedEdgeId,
  highlightNodeIds,
  highlightEdgeIds,
  highlightChipIds,
  selectedChipId,
  onSelectNode,
  onSelectEdge,
  onSelectChip,
}: RuntimeHardwarePanelProps) {
  const [roleView, setRoleView] = useState<RoleView>('compose')
  const [showGhostOverlay, setShowGhostOverlay] = useState(false)
  const [layoutMode, setLayoutMode] = useState<LayoutMode>('dataPath')

  return (
    <section className="page-section panel-elevated p-3 flex flex-col gap-3 runtime-hardware-panel">
      <InfraMapCanvas
        data={data}
        clusterLive={clusterLive}
        onOpenCluster={onOpenCluster}
        roleView={roleView}
        onRoleViewChange={setRoleView}
        layoutMode={layoutMode}
        onLayoutModeChange={setLayoutMode}
        showGhostOverlay={showGhostOverlay}
        onToggleGhostOverlay={() => setShowGhostOverlay(v => !v)}
        selectedNodeId={selectedNodeId}
        selectedEdgeId={selectedEdgeId}
        highlightNodeIds={highlightNodeIds}
        highlightEdgeIds={highlightEdgeIds}
        highlightChipIds={highlightChipIds}
        selectedChipId={selectedChipId}
        onSelectNode={onSelectNode}
        onSelectEdge={onSelectEdge}
        onSelectChip={onSelectChip}
      />
    </section>
  )
}
