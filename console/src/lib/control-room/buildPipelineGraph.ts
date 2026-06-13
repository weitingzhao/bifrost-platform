import type { OpsContextMilestone, OpsContextResponse } from '@/api/types'
import type { Edge, Node } from '@xyflow/react'

export type MilestoneNodeData = {
  milestone: OpsContextMilestone
  selected: boolean
  isDecision?: boolean
  decisionLabel?: string
}

const X_STEP = 200
const Y_MAIN = 0
const Y_PARALLEL = 130
const Y_DECISION = 70

function normalizeId(id: string): string {
  return id.replace(/[-_]/g, '').toLowerCase()
}

function isMilestoneFocused(m: OpsContextMilestone, context: OpsContextResponse): boolean {
  const headline = context.focus.headline.toLowerCase()
  const track = normalizeId(context.deployment.active_track)
  const mid = normalizeId(m.id)
  if (headline.includes(m.id) || headline.includes(mid)) return true
  if (track === mid) return true
  if (m.status === 'IN_PROGRESS') return true
  return false
}

export function buildPipelineGraph(
  context: OpsContextResponse,
  selectionId?: string | null,
): { nodes: Node<MilestoneNodeData>[]; edges: Edge[] } {
  const milestones = context.milestones
  const mainMilestones = milestones.filter(m => m.pipeline_lane !== 'parallel')
  const parallelMilestones = milestones.filter(m => m.pipeline_lane === 'parallel')

  const nodes: Node<MilestoneNodeData>[] = []
  const edges: Edge[] = []
  const decisionIds = new Set<string>()

  mainMilestones.forEach((m, i) => {
    const focused = selectionId != null ? selectionId === m.id : isMilestoneFocused(m, context)
    nodes.push({
      id: m.id,
      type: 'milestoneNode',
      position: { x: i * X_STEP, y: Y_MAIN },
      data: { milestone: m, selected: focused },
      selectable: false,
      draggable: false,
    })

    if (m.status === 'BLOCKED_ON' && m.blocker != null && m.blocker !== '') {
      const decisionId = `decision:${m.blocker.replace(/^decision:/, '')}`
      if (!decisionIds.has(decisionId)) {
        decisionIds.add(decisionId)
        const decisionKey = m.blocker.replace(/^decision:/, '')
        const decision = context.decisions.find(d => d.id === decisionKey)
        nodes.push({
          id: decisionId,
          type: 'milestoneNode',
          position: { x: i * X_STEP, y: Y_DECISION },
          data: {
            milestone: {
              id: decisionId,
              label: decision?.topic ?? m.blocker,
              status: 'BLOCKED_ON',
            },
            selected: selectionId === decisionId,
            isDecision: true,
            decisionLabel: decisionKey,
          },
          selectable: false,
          draggable: false,
        })
        edges.push({
          id: `${m.id}-blocked-${decisionId}`,
          source: m.id,
          target: decisionId,
          type: 'smoothstep',
          animated: true,
          style: { stroke: 'var(--color-lamp-red)' },
        })
      }
    }

    if (i > 0) {
      const prev = mainMilestones[i - 1]
      edges.push({
        id: `${prev.id}-to-${m.id}`,
        source: prev.id,
        target: m.id,
        type: 'smoothstep',
      })
    }
  })

  for (const pm of parallelMilestones) {
    const afterIdx = mainMilestones.findIndex(m => m.id === pm.pipeline_after)
    const x = afterIdx >= 0 ? afterIdx * X_STEP + X_STEP * 0.5 : mainMilestones.length * X_STEP
    const focused = selectionId != null ? selectionId === pm.id : isMilestoneFocused(pm, context)
    nodes.push({
      id: pm.id,
      type: 'milestoneNode',
      position: { x, y: Y_PARALLEL },
      data: { milestone: pm, selected: focused },
      selectable: false,
      draggable: false,
    })
    if (pm.pipeline_after != null) {
      edges.push({
        id: `${pm.pipeline_after}-parallel-${pm.id}`,
        source: pm.pipeline_after,
        target: pm.id,
        type: 'smoothstep',
        label: 'parallel',
        style: { strokeDasharray: '4 4' },
      })
    }
  }

  return { nodes, edges }
}
