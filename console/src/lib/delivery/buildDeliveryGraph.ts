import type { OpsContextResponse } from '@/api/types'
import type { Edge, Node } from '@xyflow/react'

export type DeliveryNodeStatus = 'live' | 'planned' | 'blocked'

export type DeliveryNodeData = {
  id: string
  label: string
  subtitle?: string
  status: DeliveryNodeStatus
  lane: 'now' | 'target' | 'decision'
  selected?: boolean
}

const X_STEP = 180
const Y_NOW = 0
const Y_TARGET = 140
const Y_DECISION = 70

const NOW_NODES: Array<{ id: string; label: string; subtitle?: string }> = [
  { id: 'dev-push', label: 'Dev / PR push', subtitle: 'Feature branches' },
  { id: 'mac-mini-2-ci', label: 'Mac Mini #2 CI', subtitle: 'lint · build · pytest' },
  { id: 'release-gate', label: 'Release gate', subtitle: 'release_gate.sh' },
  { id: 'compose-prod-70', label: 'Compose prod', subtitle: 'mini-pc-a · .70' },
]

const TARGET_NODES: Array<{ id: string; label: string; subtitle?: string }> = [
  { id: 'gitea', label: 'Gitea', subtitle: 'cicd @ mini-pc-a' },
  { id: 'tekton', label: 'Tekton', subtitle: 'Build + test' },
  { id: 'registry', label: 'Registry', subtitle: 'Internal images' },
  { id: 'argocd', label: 'ArgoCD', subtitle: 'GitOps sync' },
  { id: 'k3s-bifrost', label: 'K3s bifrost ns', subtitle: 'Rolling deploy' },
]

function statusForNowNode(id: string, context: OpsContextResponse): DeliveryNodeStatus {
  if (id === 'release-gate') {
    return context.promotion.last_gate.result != null ? 'live' : 'planned'
  }
  if (id === 'compose-prod-70') {
    const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
    if (cutover?.status === 'BLOCKED_ON') return 'blocked'
    if (cutover?.status === 'SIGNED' || cutover?.status === 'DEPLOYED') return 'live'
    return 'planned'
  }
  return 'live'
}

function statusForTargetNode(
  context: OpsContextResponse,
  clusterReachOk?: boolean,
): DeliveryNodeStatus {
  const phase = context.deployment.phase
  if (phase === 'k3s_partial' || phase === 'k3s_ha') {
    if (clusterReachOk === true) return 'live'
    return 'planned'
  }
  return 'planned'
}

function statusForTargetNodeItem(
  id: string,
  context: OpsContextResponse,
  defaultStatus: DeliveryNodeStatus,
  clusterReachOk?: boolean,
): DeliveryNodeStatus {
  if (id === 'k3s-bifrost') {
    const phase = context.deployment.phase
    if ((phase === 'k3s_partial' || phase === 'k3s_ha') && clusterReachOk === true) {
      return 'live'
    }
    return 'planned'
  }
  return defaultStatus
}

function deliveryStatusClass(status: DeliveryNodeStatus): string {
  if (status === 'live') return 'delivery-node--live'
  if (status === 'blocked') return 'delivery-node--blocked'
  return 'delivery-node--planned'
}

export { deliveryStatusClass }

export function buildDeliveryGraph(
  context: OpsContextResponse,
  selectionId?: string | null,
  clusterReachOk?: boolean,
): { nodes: Node<DeliveryNodeData>[]; edges: Edge[] } {
  const nodes: Node<DeliveryNodeData>[] = []
  const edges: Edge[] = []
  const targetStatus = statusForTargetNode(context, clusterReachOk)
  const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
  const showDecision =
    cutover?.status === 'BLOCKED_ON' && cutover.blocker != null && cutover.blocker !== ''

  NOW_NODES.forEach((def, i) => {
    const status = statusForNowNode(def.id, context)
    nodes.push({
      id: def.id,
      type: 'deliveryNode',
      position: { x: i * X_STEP, y: Y_NOW },
      data: {
        id: def.id,
        label: def.label,
        subtitle: def.subtitle,
        status,
        lane: 'now',
        selected: selectionId === def.id,
      },
      selectable: false,
      draggable: false,
    })
    if (i > 0) {
      const prev = NOW_NODES[i - 1]
      edges.push({
        id: `now-${prev.id}-to-${def.id}`,
        source: prev.id,
        target: def.id,
        type: 'smoothstep',
        label: i === 1 ? 'Now' : undefined,
      })
    }
  })

  TARGET_NODES.forEach((def, i) => {
    const nodeStatus = statusForTargetNodeItem(def.id, context, targetStatus, clusterReachOk)
    nodes.push({
      id: def.id,
      type: 'deliveryNode',
      position: { x: i * X_STEP, y: Y_TARGET },
      data: {
        id: def.id,
        label: def.label,
        subtitle: def.subtitle,
        status: nodeStatus,
        lane: 'target',
        selected: selectionId === def.id,
      },
      selectable: false,
      draggable: false,
    })
    if (i > 0) {
      const prev = TARGET_NODES[i - 1]
      edges.push({
        id: `target-${prev.id}-to-${def.id}`,
        source: prev.id,
        target: def.id,
        type: 'smoothstep',
        style: { strokeDasharray: '4 4' },
        label: i === 1 ? 'Target' : undefined,
      })
    }
  })

  if (showDecision && cutover?.blocker != null) {
    const decisionKey = cutover.blocker.replace(/^decision:/, '')
    const decision = context.decisions.find(d => d.id === decisionKey)
    const decisionId = `decision:${decisionKey}`
    nodes.push({
      id: decisionId,
      type: 'deliveryNode',
      position: { x: 3 * X_STEP, y: Y_DECISION },
      data: {
        id: decisionId,
        label: decision?.topic ?? decisionKey,
        subtitle: decisionKey,
        status: 'blocked',
        lane: 'decision',
        selected: selectionId === decisionId,
      },
      selectable: false,
      draggable: false,
    })
    edges.push({
      id: `compose-prod-to-${decisionId}`,
      source: 'compose-prod-70',
      target: decisionId,
      type: 'smoothstep',
      animated: true,
      style: { stroke: 'var(--color-lamp-red)', strokeDasharray: '4 4' },
    })
  }

  return { nodes, edges }
}
