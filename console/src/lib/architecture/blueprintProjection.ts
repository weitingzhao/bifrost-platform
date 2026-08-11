/**
 * Blueprint Projection layer — live API capability and actuation progress.
 * Constitution definitions live in blueprintCatalog.ts; this module holds
 * pointers and builders that consume platform-api at render/copy time.
 */

import type { McpToolsResponse, McpToolView } from '@/api/agentTypes'
import { ACTUATION_PHASES } from './blueprintCatalog'

export const PROJECTION_AUTHORITY = {
  apiCatalog: 'GET /api/v1/mcp/tools',
  apiSource: 'api/internal/mcp/catalog.go',
  configNote:
    'config/*.yaml mounted to platform-api — probe targets (environments), Spine (ops-context), topology, clusters. Probe targets in environments-catalog.ts / config/environments.yaml.',
} as const

export type ActuationPhaseProgress = {
  phase: string
  implemented: number
  total: number
}

export type UiMcpParityRow = {
  uiRoute: string
  uiSurface: string
  apiRoute: string
  mcpTool: string
  mcpServer: 'mcp-server-platform' | 'mcp-server-unifi' | 'mcp-server-prometheus'
  parity: 'matched' | 'partial' | 'ui-only' | 'mcp-only'
  notes?: string
}

/** Key Console routes vs MCP tools — Network actuation lives on independent mcp/unifi server. */
export const UI_MCP_PARITY_MATRIX: UiMcpParityRow[] = [
  {
    uiRoute: 'Ground Systems → Network',
    uiSurface: 'Firewall drift & apply',
    apiRoute: 'POST /api/v1/network/firewall/apply',
    mcpTool: 'apply_network_firewall',
    mcpServer: 'mcp-server-unifi',
    parity: 'matched',
    notes: 'L1 operator — not on mcp-server-platform; unifi-mcp-server proxies platform-api',
  },
  {
    uiRoute: 'Ground Systems → Network',
    uiSurface: 'Live probe + audit',
    apiRoute: 'GET /api/v1/network/status · /audit',
    mcpTool: 'get_network_status · audit_network_firewall',
    mcpServer: 'mcp-server-unifi',
    parity: 'matched',
  },
  {
    uiRoute: 'Mission Control → Observability',
    uiSurface: 'Telemetry hub',
    apiRoute: 'GET /api/v1/telemetry/overview · /alerts · /targets',
    mcpTool: 'get_telemetry_overview · get_telemetry_alerts · get_telemetry_targets',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
    notes: 'Also available via mcp-server-prometheus (query_prometheus · list_alerts · list_targets)',
  },
  {
    uiRoute: 'Satellite → Satellite Health',
    uiSurface: 'API performance metrics',
    apiRoute: 'GET /api/v1/telemetry/overview?ns=…',
    mcpTool: 'get_telemetry_overview',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
  },
  {
    uiRoute: 'Rocket → Mission Control',
    uiSurface: 'Connectivity matrix',
    apiRoute: 'GET /api/v1/matrix',
    mcpTool: 'get_connectivity_matrix',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
  },
  {
    uiRoute: 'Rocket → Delivery',
    uiSurface: 'GitOps sync / rollback',
    apiRoute: 'POST /api/v1/gitops/apps/{name}/sync',
    mcpTool: 'gitops_sync_app · gitops_rollback_app',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
  },
  {
    uiRoute: 'Ground Systems → Cluster',
    uiSurface: 'Rollout restart / scale',
    apiRoute: 'POST /api/v1/cluster/workloads/rollout-restart',
    mcpTool: 'rollout_restart_deployment · scale_deployment',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
  },
  {
    uiRoute: 'Ground Systems → Cluster',
    uiSurface: 'Node wake / join / poweroff + Layer A/B ensure',
    apiRoute:
      'POST /api/v1/cluster/nodes/{name}/wake · /join · /poweroff · addons/metrics-server|kube-prometheus-stack/ensure',
    mcpTool:
      'wake_compute_node · join_cluster_node · poweroff_compute_node · ensure_metrics_server · ensure_kube_prometheus_stack',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
    notes: 'Post-QA F2 — catalog-only gap closed',
  },
  {
    uiRoute: 'Satellite → Deploy Satellite',
    uiSurface: 'Delete terminal PipelineRun',
    apiRoute: 'DELETE /api/v1/delivery/runs/{id}',
    mcpTool: 'delete_pipeline_run',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
    notes: 'Post-QA F2 — catalog-only gap closed',
  },
  {
    uiRoute: 'Engineer → Briefing',
    uiSurface: 'Prepare IDE briefing pack',
    apiRoute: 'POST /api/v1/briefing/prepare',
    mcpTool: 'prepare_briefing',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
    notes: 'Post-QA F2 — catalog-only gap closed',
  },
  {
    uiRoute: 'Engineer → Agent Desk',
    uiSurface: 'Operate queue',
    apiRoute: 'GET /api/v1/operate/queue',
    mcpTool: 'get_operate_queue · close_operate_queue_item',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
  },
  {
    uiRoute: 'Task Mode → Daily Ops',
    uiSurface: 'Checklist signals',
    apiRoute: 'GET /api/v1/checklist/signals',
    mcpTool: 'get_checklist_signals · report_checklist_signals',
    mcpServer: 'mcp-server-platform',
    parity: 'matched',
  },
]

export function parityTagVariant(
  parity: UiMcpParityRow['parity'],
): 'success' | 'warning' | 'neutral' | 'info' {
  if (parity === 'matched') return 'success'
  if (parity === 'partial') return 'warning'
  if (parity === 'mcp-only') return 'info'
  return 'neutral'
}

/** Group MCP tools by phase label (P0–P5, Agent, …). */
export function actuationPhaseProgress(tools: McpToolView[]): ActuationPhaseProgress[] {
  const byPhase = new Map<string, { implemented: number; total: number }>()
  for (const t of tools) {
    const phase = t.phase?.trim() !== '' ? t.phase! : 'Other'
    const cur = byPhase.get(phase) ?? { implemented: 0, total: 0 }
    cur.total += 1
    if (t.implemented) cur.implemented += 1
    byPhase.set(phase, cur)
  }
  const order = ['P0', 'P1', 'P2', 'P3', 'P4', 'P5', 'P6', 'Agent', 'Other']
  return [...byPhase.entries()]
    .map(([phase, counts]) => ({ phase, ...counts }))
    .sort((a, b) => {
      const ai = order.indexOf(a.phase)
      const bi = order.indexOf(b.phase)
      return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi)
    })
}

/** P0–P5 constitution phases with live MCP implemented counts when tools are loaded. */
export function constitutionActuationWithProgress(
  tools: McpToolView[] | undefined,
): Array<{ phase: string; deliverables: string; eliminates: string; progress: string | null }> {
  const progressMap = tools != null ? new Map(actuationPhaseProgress(tools).map(p => [p.phase, p])) : null
  return ACTUATION_PHASES.map(row => {
    const key = row.phase
    const live = progressMap?.get(key)
    const progress =
      live != null && live.total > 0 ? `${live.implemented}/${live.total} MCP tools implemented` : null
    return { ...row, progress }
  })
}

export function buildBlueprintProjectionPack(tools?: McpToolsResponse): string {
  const lines: string[] = [
    '## Projection (live capability — fast-changing)',
    '',
    'Do not treat Constitution pack as API inventory. Authoritative endpoints:',
    `- ${PROJECTION_AUTHORITY.apiCatalog}`,
    `- Source: ${PROJECTION_AUTHORITY.apiSource}`,
    '',
    PROJECTION_AUTHORITY.configNote,
    '',
  ]

  if (tools != null) {
    lines.push(
      `### MCP tool catalog (${tools.implemented_count}/${tools.tools.length} implemented)`,
      `Generated: ${tools.generated_at}`,
      '',
    )
    for (const t of tools.tools) {
      const route = t.route != null && t.route !== '' ? ` ${t.method ?? 'GET'} ${t.route}` : ''
      lines.push(
        `- **${t.name}** [${t.level}] phase=${t.phase ?? '—'} implemented=${t.implemented}${route}`,
      )
    }
    lines.push('', '### Actuation phase progress (from MCP phase labels)')
    for (const p of actuationPhaseProgress(tools.tools)) {
      lines.push(`- **${p.phase}**: ${p.implemented}/${p.total} tools implemented`)
    }
    lines.push('', '### UI ↔ MCP parity (key routes)')
    for (const row of UI_MCP_PARITY_MATRIX) {
      lines.push(
        `- **${row.uiSurface}** (${row.uiRoute}) → ${row.apiRoute} · MCP \`${row.mcpTool}\` @ ${row.mcpServer} [${row.parity}]${row.notes != null ? ` — ${row.notes}` : ''}`,
      )
    }
  } else {
    lines.push(
      '### Actuation phase progress',
      'Fetch GET /api/v1/mcp/tools and group by phase field for live P0–P5 progress.',
    )
  }

  return lines.join('\n')
}
