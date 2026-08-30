/**
 * Ops Tool Rack — always-discoverable external UIs (Gitea / Grafana / Dagster).
 *
 * Console is the navigation hub: header Tools popover + contextual Open links.
 * Never iframe these apps into Console; open in a new tab.
 */

export const OPS_TOOL_RACK_VERSION = '2026-08-30'
export const OPS_TOOL_RACK_SOURCE =
  'console/src/lib/architecture/opsToolRackCatalog.ts'

export type OpsToolId = 'gitea' | 'grafana' | 'dagster'

export type OpsToolKind = 'external_ui'

export type OpsTool = {
  id: OpsToolId
  label: string
  purpose: string
  lanUrl: string
  kind: OpsToolKind
}

export const OPS_TOOL_RACK_RULES: string[] = [
  'External UIs (Gitea / Grafana / Dagster) open via Tool Rack or contextual deep-links — never iframe into Console.',
  'Header Tools popover is the always-on discovery surface; do not bury URLs only under Launch Rocket or Observability.',
  'LAN NodePort URLs are the operator path; port-forward is optional convenience only.',
]

export const OPS_TOOLS: readonly OpsTool[] = [
  {
    id: 'gitea',
    label: 'Gitea',
    purpose: 'Internal Git mirrors, PRs, and GitOps source',
    lanUrl: 'http://192.168.10.73:30300',
    kind: 'external_ui',
  },
  {
    id: 'grafana',
    label: 'Grafana',
    purpose: 'Metrics evidence and dashboards',
    lanUrl: 'http://192.168.10.73:30883',
    kind: 'external_ui',
  },
  {
    id: 'dagster',
    label: 'Dagster',
    purpose: 'Batch data husbandry schedule and materializations',
    lanUrl: 'http://192.168.10.73:30301',
    kind: 'external_ui',
  },
] as const

export function opsToolById(id: OpsToolId): OpsTool {
  const found = OPS_TOOLS.find(t => t.id === id)
  if (found == null) throw new Error(`Unknown ops tool: ${id}`)
  return found
}

/** Prefer live base when non-empty; otherwise catalog LAN URL. */
export function resolveOpsToolUrl(
  id: OpsToolId,
  liveBase?: string | null,
): string {
  const live = (liveBase ?? '').trim()
  if (live !== '') return live.replace(/\/$/, '')
  return opsToolById(id).lanUrl
}

export function buildOpsToolRackLlmPack(): string {
  const lines = [
    '## Ops Tool Rack (external UIs)',
    `Source: ${OPS_TOOL_RACK_SOURCE} · ${OPS_TOOL_RACK_VERSION}`,
    '',
    '### Tools',
    ...OPS_TOOLS.map(
      t => `- **${t.label}** [${t.id}]: ${t.purpose} · ${t.lanUrl}`,
    ),
    '',
    '### Rules',
    ...OPS_TOOL_RACK_RULES.map(r => `- ${r}`),
  ]
  return lines.join('\n')
}
