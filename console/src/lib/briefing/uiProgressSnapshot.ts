/** Static snapshot of Ops Console UI implementation — update when shipping new tabs/features. */

export type UiItemStatus = 'done' | 'partial' | 'planned'

export interface UiProgressItem {
  area: string
  item: string
  status: UiItemStatus
  notes: string
}

export const CONSOLE_UI_PROGRESS: UiProgressItem[] = [
  {
    area: 'Ops',
    item: 'Control Room',
    status: 'done',
    notes: 'Live KPI strip + matrix summary, work tracks strip (build/migrate/operate), dual flywheel bays, milestone spine, Agent focus dock',
  },
  {
    area: 'Ops',
    item: 'Agent Briefing',
    status: 'done',
    notes: 'Work tracks + lane queues, session pack + delta (incl. agent tasks on NAS), nightly DriftProposalPanel, Align Briefing meta pack',
  },
  {
    area: 'Runtime',
    item: 'Runtime Map',
    status: 'done',
    notes: 'Topology SVG, SCOPE stack, matrix probes, runtime LLM pack',
  },
  {
    area: 'Runtime',
    item: 'Cluster',
    status: 'partial',
    notes: 'P2 node wizard (maintain/compute-off/join flows); node drawer; cordon/drain/uncordon/join + compute WOL; Layer A metrics; P1 workload actuation',
  },
  {
    area: 'Runtime',
    item: 'Placement',
    status: 'done',
    notes: 'Node pools + policy matrix + violations; GET /cluster/placement; CI preflight; Delivery Run gate; workloadPlacementCatalog',
  },
  {
    area: 'Program',
    item: 'Delivery',
    status: 'partial',
    notes: 'Stack install wizard (Registry→Gitea→Tekton) + GitOps sync/rollback + Tekton pipeline runs on Operate tab; deliver-stg v2',
  },
  {
    area: 'Program',
    item: 'Milestones / Program',
    status: 'done',
    notes: 'ops-context spine: milestones, decisions D1–D6, north_star, platform_phases',
  },
  {
    area: 'Program',
    item: 'Promote',
    status: 'done',
    notes: 'Flywheel checklist + POST /promote/release-gate + gate checks; prod cutover SIGNED 2026-06-19',
  },
  {
    area: 'Catalog',
    item: 'Data Layer',
    status: 'partial',
    notes: 'dataLayerCatalog + k8s/data CNPG manifests + make k8s-install-data-layer-phase0',
  },
  {
    area: 'Catalog',
    item: 'Trade K8s-native + IB Edge',
    status: 'partial',
    notes: 'tradeK8sNativeCatalog.ts — W0 ✓ signed; W1 Traefik Ingress DELIVERED (awaiting sign-off); TRADE_GATEWAY_INGRESS hosts in catalog appendix',
  },
  {
    area: 'Catalog',
    item: 'Dual Flywheel Vision',
    status: 'partial',
    notes: 'V1–V5 gate panels complete; convergenceLoopCatalog; Dual Flywheel vision line SIGNED at V5',
  },
  {
    area: 'Catalog',
    item: 'Environments catalog',
    status: 'done',
    notes: 'Static flows/scope/hardware + Copy for LLM',
  },
  {
    area: 'Tools',
    item: 'Server console',
    status: 'done',
    notes: 'SSH/WebSocket terminal (topology allowlist)',
  },
  {
    area: 'Platform API',
    item: 'L0 probes',
    status: 'done',
    notes: 'matrix, topology, context, cluster read/placement, gitops/apps, delivery/stg/smoke + preflight, audit, auth/capabilities',
  },
  {
    area: 'Platform API',
    item: 'L1 cluster actuation',
    status: 'partial',
    notes: 'ensure namespaces, rollout restart, scale, delete pod, cordon/uncordon/drain/join — operator/admin token + audit',
  },
  {
    area: 'Platform API',
    item: 'P3 GitOps + CI actuation',
    status: 'done',
    notes: 'POST gitops/apps/{name}/sync (operator), rollback (admin), delivery/pipelines/{name}/runs — audit logged',
  },
  {
    area: 'Platform API',
    item: 'P4 stack actuation',
    status: 'done',
    notes: 'POST stack/addons/{name}/install + upgrade (admin) — infra k3s scripts via PLATFORM_STACK_INSTALL_ENABLED',
  },
  {
    area: 'Future',
    item: 'Node agent / MCP',
    status: 'partial',
    notes: 'mcp-server-platform (stdio) proxies platform-api; Architecture → MCP Contract shows tool catalog + P1–P5 UI acceptance checklist',
  },
  {
    area: 'Platform API',
    item: 'P5 MCP catalog',
    status: 'done',
    notes: 'GET /api/v1/mcp/tools + /mcp/status — tool catalog mirrors actuation routes for Agent parity',
  },
]

export function formatUiProgressSection(): string {
  const lines = ['## Ops Console UI progress (snapshot)', '']
  let lastArea = ''
  for (const row of CONSOLE_UI_PROGRESS) {
    if (row.area !== lastArea) {
      lines.push(`### ${row.area}`)
      lastArea = row.area
    }
    lines.push(`- [${row.status.toUpperCase()}] ${row.item} — ${row.notes}`)
  }
  return lines.join('\n')
}

/** Optional live placement violations — append to briefing when placement API is available. */
export function formatPlacementViolationsSummary(
  violations: { severity: string; message: string }[] | undefined,
): string {
  if (violations == null || violations.length === 0) {
    return '## Placement violations\n\n0 critical (GET /api/v1/cluster/placement)'
  }
  const lines = ['## Placement violations', '']
  for (const v of violations) {
    lines.push(`- [${v.severity}] ${v.message}`)
  }
  return lines.join('\n')
}
