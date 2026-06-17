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
    notes: 'Dual flywheel bays, milestone spine, Agent focus dock (mode packs)',
  },
  {
    area: 'Ops',
    item: 'Pulse',
    status: 'done',
    notes: 'Platform health, matrix summary, spine focus, cluster KPI strip',
  },
  {
    area: 'Ops',
    item: 'Agent Briefing',
    status: 'done',
    notes: 'Work-intent picker + session pack + Align Briefing meta pack',
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
    notes: 'L0 summary/nodes/NS/workloads; Layer A metrics; Layer B observability probe; P1 actuation (restart/scale/delete pod)',
  },
  {
    area: 'Program',
    item: 'Delivery',
    status: 'partial',
    notes: 'GitOps + stack/addons + Pipeline Run; Phase B stg v1 — nginx :30880 + 9 Trade APIs + PG/Redis + frontend (worker/socket deferred); Gitea Kaniko deliver-stg',
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
    status: 'partial',
    notes: 'Flywheel checklist + POST /promote/release-gate + gate checks table (S6); cutover still BLOCKED_ON D1',
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
    notes: 'matrix, topology, context, cluster read, gitops/apps, delivery/stg/smoke (S5), audit, auth/capabilities',
  },
  {
    area: 'Platform API',
    item: 'L1 cluster actuation',
    status: 'partial',
    notes: 'ensure namespaces, rollout restart, scale, delete pod — operator token required',
  },
  {
    area: 'Future',
    item: 'Node agent / MCP',
    status: 'planned',
    notes: 'agent/, mcp/ repos — Phase B/P2',
  },
  {
    area: 'Future',
    item: 'GitOps actuation',
    status: 'partial',
    notes: 'GitOps sync + deliver-stg pipeline + stg smoke + release gate API (S3–S6)',
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
