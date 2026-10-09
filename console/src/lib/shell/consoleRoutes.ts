/**
 * Ops Console shell routes — one layer, seven questions.
 * Old bookmarks resolve through LEGACY_HASH_REDIRECTS. Page modules stay on
 * disk; only the hash and the nav move in this step.
 */

export const SHELL_NAV = [
  { id: 'status', label: 'Status', question: 'Is everything okay right now?' },
  { id: 'data', label: 'Data', question: "Has today's data arrived?" },
  { id: 'ib', label: 'IB', question: 'Is the IB connection healthy?' },
  {
    id: 'maintenance',
    label: 'Maintenance',
    question: 'What needs approval, and what are maintainers doing?',
  },
  {
    id: 'releases',
    label: 'Releases',
    question: 'What version is running in each environment?',
  },
  {
    id: 'infrastructure',
    label: 'Infrastructure',
    question: 'How are the cluster, network, and machines?',
  },
  { id: 'progress', label: 'Progress', question: 'How far along is the project?' },
] as const

export type ShellRouteId = (typeof SHELL_NAV)[number]['id']

export const SHELL_ROUTE_IDS: readonly ShellRouteId[] = SHELL_NAV.map(item => item.id)

/** Tabs that used to be real pages. `dev-sessions` stays a local-only route. */
export const FORMER_CONSOLE_TABS = [
  'queue',
  'analysis-workspace',
  'insight-log',
  'hermes-status', // 已退役 former tab hash
  'agent-capability',
  'commit-lineage',
  'autonomous-skills',
  'execution-log',
  'agent-governance',
  'agent-system',
  'operator-plane',
  'agent-release',
  'control-room',
  'observability',
  'approvals',
  'code-health',
  'task-cc',
  'audit',
  'runtime-map',
  'cluster',
  'rocket-health',
  'trade-release',
  'research-release',
  'blueprint',
  'flywheel-vision',
  'roadmap',
  'platform-release',
  'plugin-release',
  'platform-standards',
  'agent-protocol',
  'mcp-contract',
  'design-system',
  'ai-compute',
  'dev-sessions',
  'console',
  'network',
  'satellite-bus',
  'satellite-health',
  'satellite-telemetry',
  'satellite-api',
  'plugin-gallery',
  'ib-gateway-manage',
  'market-data-manage',
  'flex-query-manage',
  'research-engine',
  'defects',
] as const

export type ConsoleLocation =
  | { kind: 'shell'; id: ShellRouteId }
  | { kind: 'dev-sessions' }

/**
 * Old hash → new hash. Choices that the destination table left open:
 * - defects → maintenance (a retrospective log, next to Audit; not live health)
 * - queue → maintenance (operator inbox, not a health reading)
 * - agent-capability → infrastructure (runner readiness sits with the mini card)
 * - agent-governance → maintenance (trust policy for patrol, which lives here)
 * - analysis-workspace / hermes-status → infrastructure (已退役 former tab)
 * - insight-log → maintenance (history, same shelf as Audit)
 * - nine Guides + agent-system → progress (project direction, not a live system)
 * - console → infrastructure (the old hash opened Network)
 */
export const LEGACY_HASH_REDIRECTS: Record<string, ShellRouteId> = {
  approvals: 'maintenance',
  'control-room': 'status',
  observability: 'status',
  'rocket-health': 'status',
  'satellite-health': 'status',
  'satellite-telemetry': 'status',
  'satellite-api': 'status',
  telemetry: 'status',
  'task-cc': 'status',
  'runtime-map': 'infrastructure',
  'code-health': 'progress',
  'commit-lineage': 'progress',
  defects: 'maintenance',
  audit: 'maintenance',
  'autonomous-skills': 'maintenance',
  'execution-log': 'maintenance',
  'platform-release': 'releases',
  'trade-release': 'releases',
  'research-release': 'releases',
  'plugin-release': 'releases',
  'agent-release': 'infrastructure',
  'operator-plane': 'infrastructure',
  cluster: 'infrastructure',
  network: 'infrastructure',
  'satellite-bus': 'ib',
  'ib-gateway-manage': 'ib',
  'research-engine': 'data',
  'plugin-gallery': 'data',
  'market-data-manage': 'data',
  'flex-query-manage': 'data',
  queue: 'maintenance',
  'agent-capability': 'infrastructure',
  'agent-governance': 'maintenance',
  'analysis-workspace': 'infrastructure',
  'insight-log': 'maintenance',
  'hermes-status': 'infrastructure', // 已退役 former tab hash
  'flywheel-vision': 'progress',
  blueprint: 'progress',
  roadmap: 'progress',
  'platform-standards': 'progress',
  'agent-protocol': 'progress',
  'agent-system': 'progress',
  'mcp-contract': 'progress',
  'design-system': 'progress',
  'ai-compute': 'progress',
  console: 'infrastructure',
  'agent-desk': 'maintenance',
  'dev-agent': 'status',
  briefing: 'status',
  'active-session': 'status',
  'delivery-board': 'status',
  'briefing-reconciliation': 'progress',
  topology: 'infrastructure',
  matrix: 'infrastructure',
  pulse: 'status',
  delivery: 'releases',
  promote: 'releases',
  program: 'status',
  'deploy-mainline': 'status',
  environments: 'status',
  'k3s-architecture': 'status',
  'k3s-bootstrap': 'status',
  'cicd-bootstrap': 'status',
  'data-layer': 'status',
  'network-upgrade': 'infrastructure',
  'network-api': 'infrastructure',
  'ib-gateway-plugin': 'ib',
  'trade-ib-client-migration': 'status',
  'cluster-observability': 'infrastructure',
  compute: 'infrastructure',
  placement: 'infrastructure',
  'analytics-pipeline': 'data',
}

export function isShellRouteId(value: string): value is ShellRouteId {
  return (SHELL_ROUTE_IDS as readonly string[]).includes(value)
}

export function shellNavEntry(id: ShellRouteId) {
  const entry = SHELL_NAV.find(item => item.id === id)
  if (entry == null) throw new Error(`unknown shell route ${id}`)
  return entry
}

/** Drop the retired taskMode query. Keep everything else (approval id, …). */
export function hashQueryWithoutTaskMode(rawHash: string): string {
  const q = rawHash.indexOf('?')
  if (q < 0) return ''
  const params = new URLSearchParams(rawHash.slice(q + 1))
  params.delete('taskMode')
  return params.toString()
}

export function formatShellHash(id: string, query = ''): string {
  if (query === '') return `#${id}`
  return `#${id}?${query}`
}

export function resolveHashTab(tab: string): { location: ConsoleLocation; legacy: boolean } {
  if (tab === 'dev-sessions') return { location: { kind: 'dev-sessions' }, legacy: false }
  if (isShellRouteId(tab)) return { location: { kind: 'shell', id: tab }, legacy: false }
  const target = LEGACY_HASH_REDIRECTS[tab]
  if (target != null) return { location: { kind: 'shell', id: target }, legacy: true }
  return { location: { kind: 'shell', id: 'status' }, legacy: tab !== '' }
}

export function locationId(location: ConsoleLocation): string {
  return location.kind === 'dev-sessions' ? 'dev-sessions' : location.id
}
