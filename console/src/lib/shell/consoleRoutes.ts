/**
 * Ops Console shell routes — one layer: Needs you first (home), the health and
 * delivery questions, then Records. Old bookmarks resolve through
 * LEGACY_HASH_REDIRECTS. `#approvals?id=` is the request page; it has no nav row.
 */

export const SHELL_NAV = [
  { id: 'needs-you', label: 'Needs you', question: 'What do I need to do right now?' },
  { id: 'status', label: 'Status', question: 'Is everything okay right now?' },
  { id: 'data', label: 'Data', question: "Has today's data arrived?" },
  { id: 'ib', label: 'IB', question: 'Is the IB connection healthy?' },
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
  {
    id: 'records',
    label: 'Records',
    question: 'History: closed requests, audit, and patrol runs.',
  },
] as const

export type ShellRouteId = (typeof SHELL_NAV)[number]['id']

export const SHELL_ROUTE_IDS: readonly ShellRouteId[] = SHELL_NAV.map(item => item.id)

export const HOME_ROUTE: ShellRouteId = 'needs-you'

/** The request page. Phone notifications link here (`#approvals?id=<id>`). */
export const APPROVAL_ROUTE = 'approvals'

/** Tabs that used to be real pages. `dev-sessions` stays a local-only route. */
export const FORMER_CONSOLE_TABS = [
  'maintenance',
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
  | { kind: 'approval' }
  | { kind: 'dev-sessions' }

/**
 * Old hash → new hash. Choices that the destination table left open:
 * - maintenance → needs-you (the old approvals home; `#maintenance?id=` opens the request)
 * - queue / agent-desk → needs-you (operator inbox)
 * - defects, audit, execution-log, insight-log → records audit tab
 * - autonomous-skills, agent-governance → records patrol tab (trust policy for patrol)
 * - agent-capability → infrastructure (runner readiness sat with the mini card)
 * - analysis-workspace / hermes-status → infrastructure (已退役 former tab)
 * - eight Guides + agent-system → progress (project direction, not a live system)
 * - console → infrastructure (the old hash opened Network)
 */
export const LEGACY_HASH_REDIRECTS: Record<string, ShellRouteId> = {
  maintenance: 'needs-you',
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
  defects: 'records',
  audit: 'records',
  'autonomous-skills': 'records',
  'execution-log': 'records',
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
  queue: 'needs-you',
  'agent-capability': 'infrastructure',
  'agent-governance': 'records',
  'analysis-workspace': 'infrastructure',
  'insight-log': 'records',
  'hermes-status': 'infrastructure', // 已退役 former tab hash
  'flywheel-vision': 'progress',
  blueprint: 'progress',
  roadmap: 'progress',
  'platform-standards': 'progress',
  'agent-system': 'progress',
  'mcp-contract': 'progress',
  'design-system': 'progress',
  'ai-compute': 'progress',
  console: 'infrastructure',
  'agent-desk': 'needs-you',
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

/** Records tab a legacy hash lands on. */
export const LEGACY_HASH_QUERY: Record<string, string> = {
  defects: 'tab=audit',
  audit: 'tab=audit',
  'execution-log': 'tab=audit',
  'insight-log': 'tab=audit',
  'autonomous-skills': 'tab=patrol',
  'agent-governance': 'tab=patrol',
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

/** `id=` from a hash query, trimmed; null when absent or blank. */
export function hashQueryParam(query: string, key: string): string | null {
  const value = new URLSearchParams(query).get(key)?.trim() ?? ''
  return value === '' ? null : value
}

/** Link to one request's page. */
export function approvalHref(id: string): string {
  return formatShellHash(APPROVAL_ROUTE, new URLSearchParams({ id }).toString())
}

export function resolveHashTab(tab: string): { location: ConsoleLocation; legacy: boolean } {
  if (tab === 'dev-sessions') return { location: { kind: 'dev-sessions' }, legacy: false }
  if (tab === APPROVAL_ROUTE) return { location: { kind: 'approval' }, legacy: false }
  if (isShellRouteId(tab)) return { location: { kind: 'shell', id: tab }, legacy: false }
  const target = LEGACY_HASH_REDIRECTS[tab]
  if (target != null) return { location: { kind: 'shell', id: target }, legacy: true }
  return { location: { kind: 'shell', id: HOME_ROUTE }, legacy: tab !== '' }
}

/**
 * Where a raw hash (without `#`) lands, and the hash the address bar should show.
 * `#approvals?id=` and `#maintenance?id=` open the request; either without an id
 * lands on Needs you.
 */
export function resolveConsoleHash(raw: string): { location: ConsoleLocation; canonical: string } {
  const tab = raw.split('?')[0] ?? ''
  const query = hashQueryWithoutTaskMode(raw)
  const approvalId = hashQueryParam(query, 'id')
  if (tab === APPROVAL_ROUTE || tab === 'maintenance') {
    if (approvalId != null) {
      return { location: { kind: 'approval' }, canonical: formatShellHash(APPROVAL_ROUTE, query) }
    }
    return { location: { kind: 'shell', id: HOME_ROUTE }, canonical: formatShellHash(HOME_ROUTE) }
  }
  const resolved = resolveHashTab(tab)
  const id = locationId(resolved.location)
  if (resolved.legacy || tab === '') {
    return { location: resolved.location, canonical: formatShellHash(id, LEGACY_HASH_QUERY[tab] ?? '') }
  }
  return { location: resolved.location, canonical: formatShellHash(id, query) }
}

export function locationId(location: ConsoleLocation): string {
  if (location.kind === 'dev-sessions') return 'dev-sessions'
  if (location.kind === 'approval') return APPROVAL_ROUTE
  return location.id
}
