import { useCallback, useMemo, useState } from 'react'
import type { ClusterSummary, MatrixResponse, OpsContextResponse, TopologyResponse } from '@/api/types'
import { RuntimeAgentStrip } from '@/components/runtime-map/RuntimeAgentStrip'
import { RuntimeHardwarePanel } from '@/components/runtime-map/RuntimeHardwarePanel'
import { RuntimeHealthStrip } from '@/components/runtime-map/RuntimeHealthStrip'
import { RuntimeMapDrawer } from '@/components/runtime-map/RuntimeMapDrawer'
import { RuntimeSoftwarePanel } from '@/components/runtime-map/RuntimeSoftwarePanel'
import { buildGapOverview } from '@/lib/runtime-map/gapAnalysis'
import { chipIdsMatchingTarget } from '@/lib/runtime-map/infraVisualRegistry'
import type { StackChipModel } from '@/lib/runtime-map/roleComponentRegistry'
import {
  filterEdgesByTarget,
  filterNodesByTarget,
  getEdge,
  type RuntimeMapSelection,
  type ScopeTag,
} from '@/lib/runtime-map/runtimeMapRegistry'

interface RuntimeMapPageProps {
  topology: TopologyResponse | undefined
  matrix: MatrixResponse | undefined
  context: OpsContextResponse | undefined
  clusterSummary?: ClusterSummary
  isLoading: boolean
  error: Error | null
  onOpenCluster?: () => void
}

export function RuntimeMapPage({
  topology,
  matrix,
  context,
  clusterSummary,
  isLoading,
  error,
  onOpenCluster,
}: RuntimeMapPageProps) {
  const [selection, setSelection] = useState<RuntimeMapSelection>(null)
  const [showFullMatrix, setShowFullMatrix] = useState(false)
  const [selectedChipId, setSelectedChipId] = useState<string | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [userDismissedDrawer, setUserDismissedDrawer] = useState(false)

  const gapOverview = useMemo(
    () => buildGapOverview(topology, matrix, 'compose'),
    [topology, matrix],
  )

  const applySelection = useCallback(
    (next: RuntimeMapSelection, chipId?: string | null) => {
      setShowFullMatrix(false)
      setSelection(next)
      setSelectedChipId(chipId ?? null)
      if (!userDismissedDrawer && next != null) {
        setDrawerOpen(true)
      }
    },
    [userDismissedDrawer],
  )

  const selectedNodeId = selection?.kind === 'node' ? selection.id : null
  const selectedEdgeId = selection?.kind === 'edge' ? selection.id : null

  const highlightNodeIds = useMemo(() => {
    if (!topology) return []
    if (selection?.kind === 'target') return filterNodesByTarget(selection.id, topology)
    if (selection?.kind === 'edge') {
      const edge = getEdge(topology, selection.id)
      if (!edge) return []
      return edge.from === edge.to ? [edge.from] : [edge.from, edge.to]
    }
    if (selection?.kind === 'node') return [selection.id]
    return []
  }, [topology, selection])

  const highlightEdgeIds = useMemo(() => {
    if (!topology) return []
    if (selection?.kind === 'target') return filterEdgesByTarget(selection.id, topology)
    return []
  }, [topology, selection])

  const highlightChipIds = useMemo(() => {
    if (!topology) return []
    if (selection?.kind === 'target') return chipIdsMatchingTarget(topology, selection.id)
    if (selection?.kind === 'edge') {
      const edge = getEdge(topology, selection.id)
      if (edge?.matrix_target) return chipIdsMatchingTarget(topology, edge.matrix_target)
    }
    return []
  }, [topology, selection])

  const handleSelectChip = (chip: StackChipModel, nodeId: string) => {
    if (chip.matrixTargetId) {
      applySelection({ kind: 'target', id: chip.matrixTargetId }, chip.chipId)
      return
    }
    if (chip.scopeTag) {
      applySelection({ kind: 'scope', tag: chip.scopeTag }, chip.chipId)
      return
    }
    applySelection({ kind: 'node', id: nodeId }, chip.chipId)
  }

  if (isLoading) {
    return <p className="text-[var(--muted-foreground)]">Loading runtime map…</p>
  }

  if (error) {
    return <p className="lamp-fail">Failed to load runtime map: {error.message}</p>
  }

  if (!topology) {
    return <p className="text-[var(--muted-foreground)]">Topology data unavailable.</p>
  }

  const clusterLive = clusterSummary?.reachability === 'ok'

  const drawerSelection = drawerOpen ? selection : null

  return (
    <div className="runtime-map-layout flex w-full min-w-0 flex-col gap-4">
      <RuntimeHealthStrip
        topology={topology}
        matrix={matrix}
        gapOverview={gapOverview}
        onSelectTarget={id => applySelection({ kind: 'target', id })}
        onSelectNode={id => applySelection({ kind: 'node', id })}
      />

      <div className="runtime-map-split grid gap-4 lg:grid-cols-[11fr_9fr] min-h-0">
        <RuntimeHardwarePanel
          data={topology}
          clusterLive={clusterLive}
          onOpenCluster={onOpenCluster}
          selectedNodeId={selectedNodeId}
          selectedEdgeId={selectedEdgeId}
          highlightNodeIds={highlightNodeIds}
          highlightEdgeIds={highlightEdgeIds}
          highlightChipIds={highlightChipIds}
          selectedChipId={selectedChipId}
          onSelectNode={id => applySelection({ kind: 'node', id })}
          onSelectEdge={id => applySelection({ kind: 'edge', id })}
          onSelectChip={handleSelectChip}
        />
        <RuntimeSoftwarePanel
          matrix={matrix}
          context={context}
          topology={topology}
          selection={selection}
          gapOverview={gapOverview}
          onSelectTarget={id => applySelection({ kind: 'target', id })}
          onSelectScope={tag => applySelection({ kind: 'scope', tag })}
        />
      </div>

      <RuntimeAgentStrip
        topology={topology}
        matrix={matrix}
        context={context}
        selection={selection}
        gapOverview={gapOverview}
      />

      <RuntimeMapDrawer
        selection={drawerSelection}
        topology={topology}
        matrix={matrix}
        context={context}
        showFullMatrix={showFullMatrix}
        onClose={() => {
          setShowFullMatrix(false)
          setSelection(null)
          setSelectedChipId(null)
          setDrawerOpen(false)
          setUserDismissedDrawer(true)
        }}
        onToggleFullMatrix={() => setShowFullMatrix(v => !v)}
      />
    </div>
  )
}

export type { RuntimeMapSelection, ScopeTag }
