import type { MatrixResponse, OpsContextResponse, TopologyResponse } from '@/api/types'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'
import {
  filterTargetsForNode,
  getEdge,
  getNode,
  getScopeLayer,
  getTarget,
  nodeToHardwareMeta,
  buildScopeLayers,
  type RuntimeMapSelection,
  type ScopeTag,
} from '@/lib/runtime-map/runtimeMapRegistry'

export function buildRuntimeLlmPack(
  topology: TopologyResponse | undefined,
  matrix: MatrixResponse | undefined,
  context: OpsContextResponse | undefined,
  selection: RuntimeMapSelection,
): string {
  const lines: string[] = ['Mode: Ops', '', '## Runtime Map context']

  if (topology != null) {
    lines.push(`- environment: ${topology.environment} (${topology.label})`)
    lines.push(`- deployment_phase: ${topology.deployment_phase}`)
    lines.push(`- generated_at: ${topology.generated_at}`)
  }

  if (matrix != null) {
    const s = summarizeMatrix(matrix)
    lines.push(`- matrix: ok=${s.ok} fail=${s.fail} degraded=${s.degraded} worst=${s.worstReach}`)
    const fails = matrix.targets.filter(t => t.reachability === 'fail').map(t => t.id)
    if (fails.length > 0) lines.push(`- failing_targets: ${fails.join(', ')}`)
  }

  if (context != null) {
    lines.push(`- spine_phase: ${context.deployment.phase}`)
    lines.push(`- active_track: ${context.deployment.active_track}`)
    lines.push(`- focus: ${context.focus.headline}`)
  }

  lines.push('')
  lines.push(selectionSection(selection, topology, matrix))

  lines.push('')
  lines.push('## Suggested Agent question')
  lines.push(suggestedQuestion(topology, matrix, selection))

  lines.push('')
  lines.push('## Session discipline')
  lines.push('- Reply in Chinese for dialogue; English for UI strings and code identifiers.')
  lines.push('- L0 read-only: diagnose probes before proposing infra writes.')

  return lines.join('\n')
}

function selectionSection(
  selection: RuntimeMapSelection,
  topology: TopologyResponse | undefined,
  matrix: MatrixResponse | undefined,
): string {
  if (selection == null) {
    return '## Selection\n(none — full environment view)'
  }

  const layers = buildScopeLayers(matrix)

  if (selection.kind === 'node' && topology != null) {
    const node = getNode(topology, selection.id)
    if (!node) return `## Selection\nnode: ${selection.id} (not found)`
    const hw = nodeToHardwareMeta(node.id)
    const targets = filterTargetsForNode(node, matrix)
    const out = [
      `## Selected hardware node: ${node.id}`,
      `- label: ${node.label}`,
      `- host: ${node.host ?? '(none)'}`,
      `- status: ${node.status}`,
      `- detail: ${node.detail}`,
      `- compose_roles: ${node.compose_roles.join(', ')}`,
      `- k3s_roles: ${node.k3s_roles.join(', ')}`,
    ]
    if (hw) {
      out.push(`- hardware_compose: ${hw.roleCompose}`)
      out.push(`- hardware_k3s: ${hw.roleK3s}`)
    }
    if (targets.length > 0) {
      out.push('', '### Matrix services on node')
      for (const t of targets) {
        out.push(`- ${t.id}: ${t.reachability} — ${t.detail}`)
      }
    }
    return out.join('\n')
  }

  if (selection.kind === 'target' && matrix != null) {
    const t = getTarget(matrix, selection.id)
    const tag = layers.find(l => l.targetIds.includes(selection.id))?.tag
    return [
      `## Selected matrix target: ${selection.id}`,
      t != null ? `- reachability: ${t.reachability}` : '- (no probe row)',
      t != null ? `- detail: ${t.detail}` : '',
      t != null ? `- category: ${t.category}` : '',
      tag != null ? `- scope_tag: ${tag}` : '',
    ]
      .filter(Boolean)
      .join('\n')
  }

  if (selection.kind === 'scope') {
    const layer = getScopeLayer(layers, selection.tag as ScopeTag)
    if (!layer) return `## Selection\nscope: ${selection.tag}`
    return [
      `## Selected scope layer: ${layer.tag}`,
      `- component: ${layer.component}`,
      `- technology: ${layer.technology}`,
      `- notes: ${layer.notes}`,
      layer.targetIds.length > 0 ? `- targets: ${layer.targetIds.join(', ')}` : '- targets: (planned only)',
    ].join('\n')
  }

  if (selection.kind === 'edge' && topology != null) {
    const edge = getEdge(topology, selection.id)
    if (!edge) return `## Selection\nedge: ${selection.id} (not found)`
    const from = getNode(topology, edge.from)
    const to = getNode(topology, edge.to)
    return [
      `## Selected topology edge: ${edge.id}`,
      `- label: ${edge.label}`,
      `- from: ${from?.label ?? edge.from}`,
      `- to: ${to?.label ?? edge.to}`,
      `- kind: ${edge.kind}`,
      `- status: ${edge.status}`,
      edge.matrix_target ? `- matrix_target: ${edge.matrix_target}` : '',
      edge.detail ? `- detail: ${edge.detail}` : '',
    ]
      .filter(Boolean)
      .join('\n')
  }

  return '## Selection\n(unknown)'
}

function suggestedQuestion(
  topology: TopologyResponse | undefined,
  matrix: MatrixResponse | undefined,
  selection: RuntimeMapSelection,
): string {
  const fails = matrix?.targets.filter(t => t.reachability === 'fail') ?? []

  if (selection?.kind === 'edge') {
    return `Topology edge ${selection.id} shows ${topology?.environment ?? 'env'} link status. What does the matrix probe imply and which host should we inspect first?`
  }

  if (selection?.kind === 'target' && selection.id.startsWith('api-')) {
    return `Matrix target ${selection.id} is failing in ${topology?.environment ?? 'env'}. List likely root causes (compose/nginx/container) and a read-only verification checklist.`
  }

  if (selection?.kind === 'node') {
    return `Analyze hardware node ${selection.id} in ${topology?.environment ?? 'env'}: which matrix services are failing and what is the smallest L0 diagnostic step?`
  }

  if (fails.length > 0 && fails.every(t => t.id.startsWith('api-'))) {
    return 'All or most Trade API matrix targets are failing on prod. Propose ordered root-cause checks (nginx, docker compose, LAN) without write actions.'
  }

  if (topology?.deployment_phase === 'compose') {
    return 'We are in compose phase with a parallel K3s track. What runtime work is safe on flywheel B while prod cutover is blocked on D1?'
  }

  return 'Summarize runtime health from matrix + topology and propose one single-variable next Ops task.'
}
