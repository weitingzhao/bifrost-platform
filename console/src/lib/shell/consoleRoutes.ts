/**
 * Ops Console shell routes (Owner 2026-10-16, design system v2 §10): four entries — For you
 * (home), Chief of staff, Work, System — then the detail pages, which hang off an entry and are
 * listed under "Detail pages" (a desktop's side nav, a phone's More). Old bookmarks resolve through
 * LEGACY_HASH_REDIRECTS; `#owner?tab=` lands on the matching entry. `#approvals?id=` is the request
 * page; it has no nav row. Needs you and Progress are part of For you and Work (W-31 2.2b, the
 * navigation's second slice): their hashes land on that entry, at the same section.
 */

export const SHELL_NAV = [
  { id: 'you', label: 'For you', question: 'Is anything mine?', group: 'entry' },
  { id: 'chief', label: 'Chief of staff', question: 'What do I ask the chief of staff, and what came back?', group: 'entry' },
  { id: 'work', label: 'Work', question: 'Is the work moving?', group: 'entry' },
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
    question: 'History: closed requests, audit, and patrol runs.',
    group: 'detail',
  },
] as const

export type ShellRouteId = (typeof SHELL_NAV)[number]['id']

export type EntryRouteId = Extract<(typeof SHELL_NAV)[number], { group: 'entry' }>['id']

export const SHELL_ROUTE_IDS: readonly ShellRouteId[] = SHELL_NAV.map(item => item.id)

export const ENTRY_ROUTE_IDS: readonly EntryRouteId[] = ['you', 'chief', 'work', 'system']

export const DETAIL_ROUTE_IDS: readonly ShellRouteId[] = SHELL_NAV.filter(item => item.group === 'detail').map(item => item.id)

export const HOME_ROUTE: ShellRouteId = 'you'

/** Every waiting item, on For you. A request or question link without an id lands here. */
export const NEEDS_YOU_ROUTE: ShellRouteId = 'you'

/** The section of For you that lists every waiting item (what was the Needs you page). */
export const WAITING_SECTION = 'waiting'

/** The section of Work that holds the goal cards (what was the top of the Progress page). */
export const GOALS_SECTION = 'goals'

/** Every waiting item, in full: For you, at its waiting section. */
export const NEEDS_YOU_HREF = `#${NEEDS_YOU_ROUTE}?section=${WAITING_SECTION}`

/** Work, at the goal cards; with an id, that goal's card. */
export function goalHref(id?: string): string {
  return id ? `#work?goal=${encodeURIComponent(id)}` : `#work?section=${GOALS_SECTION}`
}

/** The list of detail pages, for a phone or a tablet (a desktop lists them in the side nav). */
export const MORE_ROUTE = 'more'

/** The request page. Phone notifications link here (`#approvals?id=<id>`). */
export const APPROVAL_ROUTE = 'approvals'

/** The question page. Phone notifications link here (`#questions?id=q_…`). */
export const QUESTION_ROUTE = 'questions'

/** Server question ids. Anything else is not requested. */
export const QUESTION_ID_PATTERN = /^q_[0-9a-f]{16}$/

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
  | { kind: 'more' }
  | { kind: 'dev-sessions' }

/**
 * Old hash → new hash. Choices that the destination table left open:
 * - maintenance → For you's waiting items (the old approvals home; `#maintenance?id=` opens the request)
 * - queue / agent-desk → For you's waiting items (operator inbox)
 * - defects, audit, execution-log, insight-log → records audit tab
 * - autonomous-skills, agent-governance → records patrol tab (trust policy for patrol)
 * - agent-capability → infrastructure (runner readiness sat with the mini card)
 * - analysis-workspace / hermes-status → infrastructure (已退役 former tab)
 * - eight Guides + agent-system → Work (project direction, not a live system)
 * - console → infrastructure (the old hash opened Network)
 */
export const LEGACY_HASH_REDIRECTS: Record<string, ShellRouteId> = {
  'needs-you': 'you',
  progress: 'work',
  agents: 'work',
  maintenance: 'you',
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
  queue: 'you',
  'agent-capability': 'infrastructure',
  'agent-governance': 'records',
  'analysis-workspace': 'infrastructure',
  'insight-log': 'records',
  'hermes-status': 'infrastructure', // 已退役 former tab hash
  'flywheel-vision': 'work',
  blueprint: 'work',
  roadmap: 'work',
  'platform-standards': 'work',
  'agent-system': 'work',
  'mcp-contract': 'work',
  'design-system': 'work',
  'ai-compute': 'work',
  console: 'infrastructure',
  'agent-desk': 'you',
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
  all: 'you',
  you: 'you',
  chief: 'chief',
  work: 'work',
  system: 'system',
  data: 'system',
}

/** The section or Records tab a legacy hash lands on. */
export const LEGACY_HASH_QUERY: Record<string, string> = {
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
 * question under For you, every detail page under System. More belongs to none.
 */
export function entryOf(location: ConsoleLocation): EntryRouteId | null {
  if (location.kind === 'more' || location.kind === 'dev-sessions') return null
  if (location.kind === 'approval' || location.kind === 'question') return 'you'
  if (isEntryRoute(location.id)) return location.id
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
  if (tab === MORE_ROUTE) return { location: { kind: 'more' }, legacy: false }
  if (isShellRouteId(tab)) return { location: { kind: 'shell', id: tab }, legacy: false }
  const target = LEGACY_HASH_REDIRECTS[tab]
  if (target != null) return { location: { kind: 'shell', id: target }, legacy: true }
  return { location: { kind: 'shell', id: HOME_ROUTE }, legacy: tab !== '' }
}

/**
 * Where a raw hash (without `#`) lands, and the hash the address bar should show.
 * `#approvals?id=` and `#maintenance?id=` open the request; either without an id
 * lands on For you's waiting items. `#questions?id=` opens the question; `#questions` with no
 * id lands on For you's waiting items. A malformed id still opens the question page, which
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
    return { location: { kind: 'shell', id: entry }, canonical: formatShellHash(entry) }
  }
  const resolved = resolveHashTab(tab)
  const id = locationId(resolved.location)
  if (MERGED_PAGES.has(tab) && query !== '') {
    return { location: resolved.location, canonical: formatShellHash(id, query) }
  }
  if (resolved.legacy || tab === '') {
    return { location: resolved.location, canonical: formatShellHash(id, LEGACY_HASH_QUERY[tab] ?? '') }
  }
  return { location: resolved.location, canonical: formatShellHash(id, query) }
}

export function locationId(location: ConsoleLocation): string {
  if (location.kind === 'dev-sessions') return 'dev-sessions'
  if (location.kind === 'approval') return APPROVAL_ROUTE
  if (location.kind === 'question') return QUESTION_ROUTE
  if (location.kind === 'more') return MORE_ROUTE
  return location.id
}
