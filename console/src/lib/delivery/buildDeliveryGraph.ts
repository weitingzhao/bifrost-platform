import type { GitOpsAppsResponse, OpsContextResponse, StackAddonsResponse } from '@/api/types'
import type { Edge, Node } from '@xyflow/react'

export type DeliveryNodeStatus = 'live' | 'planned' | 'blocked' | 'degraded'

export type DeliveryNodeData = {
  id: string
  label: string
  subtitle?: string
  status: DeliveryNodeStatus
  lane: 'now' | 'target' | 'decision'
  selected?: boolean
}

const X_STEP = 172
const Y_NOW = 0
const Y_TARGET = 200
const Y_DECISION = 108

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

function argocdNodeStatus(gitops: GitOpsAppsResponse | undefined): DeliveryNodeStatus {
  if (gitops == null) return 'planned'
  if (gitops.argocd_status === 'not_installed') return 'planned'
  if (gitops.argocd_status === 'unavailable' || gitops.reachability === 'fail') return 'blocked'
  if (gitops.argocd_status === 'installed' && gitops.reachability === 'ok') return 'live'
  if (gitops.argocd_status === 'installed') return 'degraded'
  return 'planned'
}

function k3sBifrostNodeStatus(
  gitops: GitOpsAppsResponse | undefined,
  clusterReachOk?: boolean,
): DeliveryNodeStatus {
  if (gitops != null && gitops.apps.length > 0) {
    const allHealthy = gitops.apps.every(
      a => a.sync_status.toLowerCase() === 'synced' && a.health_status.toLowerCase() === 'healthy',
    )
    return allHealthy ? 'live' : 'degraded'
  }
  if (gitops?.argocd_status === 'installed' && gitops.reachability === 'ok') {
    return 'degraded'
  }
  if (clusterReachOk === true) return 'planned'
  return 'planned'
}

function statusForTargetNodeItem(
  id: string,
  gitops: GitOpsAppsResponse | undefined,
  stack: StackAddonsResponse | undefined,
  clusterReachOk?: boolean,
): DeliveryNodeStatus {
  switch (id) {
    case 'argocd':
      return argocdNodeStatus(gitops)
    case 'k3s-bifrost':
      return k3sBifrostNodeStatus(gitops, clusterReachOk)
    case 'gitea':
    case 'tekton':
    case 'registry': {
      const addon = stack?.addons.find(a => a.id === id)
      if (addon == null) return 'planned'
      if (addon.status === 'installed' && addon.reachability === 'ok') return 'live'
      if (addon.status === 'not_installed') return 'planned'
      if (addon.reachability === 'fail') return 'blocked'
      if (addon.status === 'degraded' || addon.reachability === 'degraded') return 'degraded'
      return 'planned'
    }
    default:
      return 'planned'
  }
}

function targetSubtitle(
  id: string,
  def: { subtitle?: string },
  gitops: GitOpsAppsResponse | undefined,
  stack: StackAddonsResponse | undefined,
): string | undefined {
  if (id === 'gitea' || id === 'tekton' || id === 'registry') {
    const addon = stack?.addons.find(a => a.id === id)
    if (addon?.name != null && addon.ready != null) {
      return `${addon.name} · ${addon.ready}`
    }
    if (addon?.status === 'not_installed') {
      return 'Planned · P3 install'
    }
    return addon?.detail?.slice(0, 48) ?? def.subtitle
  }
  if (id === 'argocd' && gitops != null) {
    if (gitops.argocd_status === 'installed' && gitops.server != null) {
      return `${gitops.server.name} · ${gitops.server.ready}`
    }
    if (gitops.argocd_status === 'not_installed') {
      return 'Planned · P3 probe'
    }
    return gitops.detail.slice(0, 48) || def.subtitle
  }
  if (id === 'k3s-bifrost' && gitops != null && gitops.apps.length > 0) {
    return `${gitops.apps.length} app(s) tracked`
  }
  return def.subtitle
}

function deliveryStatusClass(status: DeliveryNodeStatus): string {
  if (status === 'live') return 'delivery-node--live'
  if (status === 'blocked') return 'delivery-node--blocked'
  if (status === 'degraded') return 'delivery-node--degraded'
  return 'delivery-node--planned'
}

export { deliveryStatusClass }

export function buildDeliveryGraph(
  context: OpsContextResponse,
  selectionId?: string | null,
  clusterReachOk?: boolean,
  gitops?: GitOpsAppsResponse,
  stack?: StackAddonsResponse,
): { nodes: Node<DeliveryNodeData>[]; edges: Edge[] } {
  const nodes: Node<DeliveryNodeData>[] = []
  const edges: Edge[] = []
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
    const nodeStatus = statusForTargetNodeItem(def.id, gitops, stack, clusterReachOk)
    nodes.push({
      id: def.id,
      type: 'deliveryNode',
      position: { x: i * X_STEP, y: Y_TARGET },
      data: {
        id: def.id,
        label: def.label,
        subtitle: targetSubtitle(def.id, def, gitops, stack),
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
