import { GOVERNANCE_REFERENCES } from './governanceReferences'

/** Five domains; former hashes remain compatible with the same entities and query parameters. */

export const SHELL_NAV = [
  { id: 'command', label: 'Command', question: 'What needs my decision, and what do I want done?', group: 'entry' },
  { id: 'crew', label: 'Crew', question: 'What can the crew do, and under which rules?', group: 'entry' },
  { id: 'work', label: 'Work', question: 'Is the work moving?', group: 'entry' },
  { id: 'services', label: 'Services', question: 'How are the applications and their data running?', group: 'entry' },
  { id: 'system', label: 'System', question: 'Is the machine fine?', group: 'entry' },
  { id: 'status', label: 'Status', question: 'Is everything okay right now?', group: 'detail' },
  { id: 'data', label: 'Data', question: "Has today's data arrived?", group: 'detail' },
  { id: 'ib', label: 'IB', question: 'Is the IB connection healthy?', group: 'detail' },
  {
    id: 'releases',
    label: 'Releases',
    question: 'What version is running in each environment?',
    group: 'detail',
  },
  {
    id: 'infrastructure',
    label: 'Infrastructure',
    question: 'How are the cluster, network, and machines?',
    group: 'detail',
  },
  {
    id: 'records',
    label: 'Records',
    question: 'History: closed requests, finished work, audit, and patrol runs.',
    group: 'detail',
  },
] as const

export type ShellRouteId = (typeof SHELL_NAV)[number]['id']

export type EntryRouteId = Extract<(typeof SHELL_NAV)[number], { group: 'entry' }>['id']

export const SHELL_ROUTE_IDS: readonly ShellRouteId[] = SHELL_NAV.map(item => item.id)

export const ENTRY_ROUTE_IDS: readonly EntryRouteId[] = ['command', 'crew', 'work', 'services', 'system']

export const DETAIL_ROUTE_IDS: readonly ShellRouteId[] = SHELL_NAV.filter(item => item.group === 'detail').map(item => item.id)

export const HOME_ROUTE: ShellRouteId = 'command'

/** Every waiting item, on Command. A request or question link without an id lands here. */
export const NEEDS_YOU_ROUTE: ShellRouteId = 'command'

/** The section of Command that lists every waiting item (what was the Needs you page). */
export const WAITING_SECTION = 'waiting'

/** The section of Work that holds the goal cards (what was the top of the Progress page). */
export const GOALS_SECTION = 'goals'

/** Every waiting item, in full: Command, at its waiting section. */
export const NEEDS_YOU_HREF = `#${NEEDS_YOU_ROUTE}?section=${WAITING_SECTION}`

/** Work, at the goal cards; with an id, that goal's card; with a task of it too, that task's details on the card (T36). */
export function goalHref(id?: string, task?: string): string {
  if (!id) return `#work?section=${GOALS_SECTION}`
  return `#work?goal=${encodeURIComponent(id)}${task ? `&task=${encodeURIComponent(task)}` : ''}`
}

/** The list of detail pages, for a phone or a tablet (a desktop lists them in the side nav). */
export const MORE_ROUTE = 'more'

/** The request page. Phone notifications link here (`#approvals?id=<id>`). */
export const APPROVAL_ROUTE = 'approvals'

/** The question page. Phone notifications link here (`#questions?id=q_…`). */
export const QUESTION_ROUTE = 'questions'

/** Server question ids. Anything else is not requested. */
export const QUESTION_ID_PATTERN = /^q_[0-9a-f]{16}$/

/** A business issue's read-only page (`#issue?id=BI-n`, W-31 BI-1 slice 2): the link on its Owner's cards. */
export const ISSUE_ROUTE = 'issue'

/** Link to one business issue's page. */
export function issueHref(id: string): string {
  return formatShellHash(ISSUE_ROUTE, new URLSearchParams({ id }).toString())
}

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
  'needs-you',
  'progress',
] as const

export type ConsoleLocation =
  | { kind: 'shell'; id: ShellRouteId }
  | { kind: 'approval' }
  | { kind: 'question' }
  | { kind: 'issue' }
  | { kind: 'more' }
  | { kind: 'dev-sessions' }

/**
 * Old hash → new hash. Choices that the destination table left open:
 * - maintenance → Command's waiting items (the old approvals home; `#maintenance?id=` opens the request)
 * - queue / agent-desk → Command's waiting items (operator inbox)
 * - defects, audit, execution-log, insight-log → records audit tab
 * - autonomous-skills, agent-governance → records patrol tab (trust policy for patrol)
 * - agent-capability → infrastructure (runner readiness sat with the mini card)
 * - analysis-workspace / hermes-status → infrastructure (已退役 former tab)
 * - eight Guides + agent-system → Crew (project direction, not a live system)
 * - console → infrastructure (the old hash opened Network)
 */
export const LEGACY_HASH_REDIRECTS: Record<string, ShellRouteId> = {
  you: 'command',
  chief: 'command',
  'needs-you': 'command',
  progress: 'work',
  agents: 'work',
  maintenance: 'command',
  'control-room': 'status',
  observability: 'status',
  'rocket-health': 'status',
  'satellite-health': 'status',
  'satellite-telemetry': 'status',
  'satellite-api': 'status',
  telemetry: 'status',
  'task-cc': 'status',
  'runtime-map': 'infrastructure',
  'code-health': 'work',
  'commit-lineage': 'work',
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
  queue: 'command',
  'agent-capability': 'infrastructure',
  'agent-governance': 'records',
  'analysis-workspace': 'infrastructure',
  'insight-log': 'records',
  'hermes-status': 'infrastructure', // 已退役 former tab hash
  'flywheel-vision': 'crew',
  blueprint: 'crew',
  roadmap: 'crew',
  'platform-standards': 'crew',
  'agent-system': 'crew',
  'agent-protocol': 'crew',
  'mcp-contract': 'crew',
  'design-system': 'crew',
  'ai-compute': 'crew',
  console: 'infrastructure',
  'agent-desk': 'command',
  'dev-agent': 'status',
  briefing: 'status',
  'active-session': 'status',
  'delivery-board': 'status',
  'briefing-reconciliation': 'work',
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

/** The former one-page Owner Console's tabs (`#owner?tab=`) and the entry each became. */
export const OWNER_TAB_ENTRY: Record<string, EntryRouteId> = {
  all: 'command',
  you: 'command',
  chief: 'command',
  work: 'work',
  system: 'system',
  data: 'services',
}

/** The section or Records tab a legacy hash lands on. */
export const LEGACY_HASH_QUERY: Record<string, string> = {
  chief: 'section=owner-chief',
  ...Object.fromEntries(GOVERNANCE_REFERENCES.map(item => [item.id, `section=references&reference=${item.id}`])),
  'needs-you': `section=${WAITING_SECTION}`,
  progress: `section=${GOALS_SECTION}`,
  queue: `section=${WAITING_SECTION}`,
  'agent-desk': `section=${WAITING_SECTION}`,
  'commit-lineage': 'section=commit-lineage',
  'code-health': 'section=code-health',
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

export function isEntryRoute(value: string): value is EntryRouteId {
  return (ENTRY_ROUTE_IDS as readonly string[]).includes(value)
}

/**
 * The entry a place belongs to, lit in the tab bar or the rail while it is open: a request or a
 * question under Command, detail pages under their owning domain. More belongs to none.
 */
export function entryOf(location: ConsoleLocation, query = ''): EntryRouteId | null {
  if (location.kind === 'more') return null
  if (location.kind === 'dev-sessions') return 'system'
  if (location.kind === 'approval' || location.kind === 'question' || location.kind === 'issue') return 'command'
  if (isEntryRoute(location.id)) return location.id
  if (location.id === 'data' || location.id === 'ib') return 'services'
  if (location.id === 'releases') return 'work'
  if (location.id === 'records') {
    const tab = hashQueryParam(query, 'tab')
    if (tab === 'closed' || tab == null) return 'command'
    if (tab === 'work') return 'work'
  }
  return 'system'
}

/** Hashes of pages now part of an entry: what they carried (`goal=`, `section=`, `id=`) still applies there. */
const MERGED_PAGES: ReadonlySet<string> = new Set(['needs-you', 'progress'])

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

export function isQuestionId(id: string): boolean {
  return QUESTION_ID_PATTERN.test(id)
}

/** Link to one question's page. */
export function questionHref(id: string): string {
  return formatShellHash(QUESTION_ROUTE, new URLSearchParams({ id }).toString())
}

export function resolveHashTab(tab: string): { location: ConsoleLocation; legacy: boolean } {
  if (tab === 'dev-sessions') return { location: { kind: 'dev-sessions' }, legacy: false }
  if (tab === APPROVAL_ROUTE) return { location: { kind: 'approval' }, legacy: false }
  if (tab === QUESTION_ROUTE) return { location: { kind: 'question' }, legacy: false }
  if (tab === ISSUE_ROUTE) return { location: { kind: 'issue' }, legacy: false }
  if (tab === MORE_ROUTE) return { location: { kind: 'more' }, legacy: false }
  if (isShellRouteId(tab)) return { location: { kind: 'shell', id: tab }, legacy: false }
  const target = Object.hasOwn(LEGACY_HASH_REDIRECTS, tab) ? LEGACY_HASH_REDIRECTS[tab] : undefined
  if (target != null) return { location: { kind: 'shell', id: target }, legacy: true }
  return { location: { kind: 'shell', id: HOME_ROUTE }, legacy: tab !== '' }
}

/**
 * Where a raw hash (without `#`) lands, and the hash the address bar should show.
 * `#approvals?id=` and `#maintenance?id=` open the request; either without an id
 * lands on Command's waiting items. `#questions?id=` opens the question; `#questions` with no
 * id lands on Command's waiting items. A malformed id still opens the question page, which
 * does not request it.
 */
export function resolveConsoleHash(raw: string): { location: ConsoleLocation; canonical: string } {
  const tab = raw.split('?')[0] ?? ''
  const query = hashQueryWithoutTaskMode(raw)
  const approvalId = hashQueryParam(query, 'id')
  if (tab === QUESTION_ROUTE) {
    const questionId = hashQueryParam(query, 'id')
    if (questionId == null) {
      return { location: { kind: 'shell', id: NEEDS_YOU_ROUTE }, canonical: NEEDS_YOU_HREF }
    }
    return { location: { kind: 'question' }, canonical: formatShellHash(QUESTION_ROUTE, query) }
  }
  if (tab === ISSUE_ROUTE) {
    if (hashQueryParam(query, 'id') == null) {
      return { location: { kind: 'shell', id: NEEDS_YOU_ROUTE }, canonical: NEEDS_YOU_HREF }
    }
    return { location: { kind: 'issue' }, canonical: formatShellHash(ISSUE_ROUTE, query) }
  }
  if (tab === APPROVAL_ROUTE || tab === 'maintenance') {
    if (approvalId != null) {
      return { location: { kind: 'approval' }, canonical: formatShellHash(APPROVAL_ROUTE, query) }
    }
    return { location: { kind: 'shell', id: NEEDS_YOU_ROUTE }, canonical: NEEDS_YOU_HREF }
  }
  if (tab === 'owner') {
    const tab = hashQueryParam(query, 'tab') ?? ''
    // Own keys only: `constructor` or `__proto__` would otherwise read a property every object inherits (review 226 C-226-4).
    const entry = Object.hasOwn(OWNER_TAB_ENTRY, tab) ? OWNER_TAB_ENTRY[tab] : HOME_ROUTE
    const params = new URLSearchParams(query)
    params.delete('tab')
    return { location: { kind: 'shell', id: entry }, canonical: formatShellHash(entry, mergeLegacyQuery(tab === 'chief' ? 'section=owner-chief' : '', params.toString())) }
  }
  if (tab === 'work' && hashQueryParam(query, 'section') === 'references') {
    return { location: { kind: 'shell', id: 'crew' }, canonical: formatShellHash('crew', query) }
  }
  const resolved = resolveHashTab(tab)
  const id = locationId(resolved.location)
  if (MERGED_PAGES.has(tab) && query !== '') {
    return { location: resolved.location, canonical: formatShellHash(id, query) }
  }
  if (resolved.legacy || tab === '') {
    return { location: resolved.location, canonical: formatShellHash(id, mergeLegacyQuery(LEGACY_HASH_QUERY[tab] ?? '', query)) }
  }
  return { location: resolved.location, canonical: formatShellHash(id, query) }
}

export function locationId(location: ConsoleLocation): string {
  if (location.kind === 'dev-sessions') return 'dev-sessions'
  if (location.kind === 'approval') return APPROVAL_ROUTE
  if (location.kind === 'question') return QUESTION_ROUTE
  if (location.kind === 'issue') return ISSUE_ROUTE
  if (location.kind === 'more') return MORE_ROUTE
  return location.id
}

function mergeLegacyQuery(defaults: string, incoming: string): string {
  const params = new URLSearchParams(defaults)
  for (const [key, value] of new URLSearchParams(incoming)) params.set(key, value)
  return params.toString()
}
