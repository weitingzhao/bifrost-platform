import type {
  ClusterObservabilityResponse,
  ClusterSummary,
  MatrixResponse,
  OpsContextResponse,
} from '@/api/types'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'
import { summarizeCluster } from '@/lib/cluster/clusterHealth'
import { CATALOG_VERSION, PLATFORM_PORTS } from '@/lib/environments-catalog'

export interface BriefingSnapshotInput {
  context?: OpsContextResponse
  matrices: MatrixResponse[]
  clusterSummary?: ClusterSummary
  clusterObservability?: ClusterObservabilityResponse
  platformHealthy?: boolean
}

export function formatBriefingLiveStatus(input: BriefingSnapshotInput): string {
  const lines = ['## Live status (from platform-api)', '']
  lines.push(
    `- platform_api_health: ${input.platformHealthy === true ? 'ok' : input.platformHealthy === false ? 'fail' : 'unknown'}`,
  )
  lines.push(`- catalog_version: ${CATALOG_VERSION}`)
  lines.push(
    `- console: http://127.0.0.1:${PLATFORM_PORTS.platformConsole} · api: :${PLATFORM_PORTS.platformApi}`,
  )

  if (input.context != null) {
    lines.push(`- spine_focus: ${input.context.focus.headline}`)
    if (input.context.focus.blocker) lines.push(`- spine_blocker: ${input.context.focus.blocker}`)
    lines.push(`- deployment_phase: ${input.context.deployment.phase}`)
    lines.push(`- active_track: ${input.context.deployment.active_track}`)
    lines.push(`- flywheel_primary: ${input.context.focus.flywheel_primary}`)
  } else {
    lines.push('- spine: (not loaded)')
  }

  for (const m of input.matrices) {
    const s = summarizeMatrix(m)
    lines.push(
      `- matrix_${m.environment}: ok=${s.ok} fail=${s.fail} degraded=${s.degraded} worst=${s.worstReach}`,
    )
  }
  if (input.matrices.length === 0) lines.push('- matrix: (not loaded)')

  const cluster = summarizeCluster(input.clusterSummary)
  lines.push(`- cluster: ${cluster.label} (${cluster.reach})`)
  if (input.clusterObservability != null) {
    lines.push(
      `- layer_b: ${input.clusterObservability.layer_b_status} · ${input.clusterObservability.detail}`,
    )
  }

  return lines.join('\n')
}
