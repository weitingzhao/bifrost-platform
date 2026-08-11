import type { ShellNavGroup, ShellNavItem, ShellNavSubGroup } from '@bifrost/ui'
import {
  Activity,
  Archive,
  BookOpen,
  Bot,
  Boxes,
  BrainCircuit,
  CalendarClock,
  ClipboardList,
  Container,
  Cpu,
  Database,
  FileCode2,
  Gauge,
  HeartPulse,
  History,
  LifeBuoy,
  LineChart,
  ListTodo,
  MapPinned,
  Microscope,
  Network,
  Orbit,
  Plug,
  Rocket,
  Ruler,
  Satellite,
  Scale,
  Server,
  Shield,
  ShieldCheck,
  Sparkles,
  Workflow,
} from 'lucide-react'

/**
 * Ops Console sidebar — Command Hierarchy (Seat / Partner / Mission / Support).
 *
 * Mission Control and Engineer are no longer collapsible navGroups. They render
 * via ShellNavSidebar `seatContent` / `partnerContent` slots. Remaining groups
 * are Mission (Satellite + Rocket, pig) and Support (Ground + Subcontractors, chicken).
 *
 * System Domain SSOT (ids + purpose + scope→domain): systemDomainCatalog.ts
 *
 * | Zone     | Surface                         | Intent                                           |
 * |----------|---------------------------------|--------------------------------------------------|
 * | Seat     | Mission Control (fixed)         | Execute (TCC) → Control Room → Observability     |
 * | Seat     | Defects & Audit (collapsible)   | Retrospective records adjacent to Seat           |
 * | Partner  | Engineer strip (persona)        | Build Desk + Launch Desk; Ops Desk / Analysis Desk |
 * | Mission  | Satellite → Rocket → Plugin     | Payload + Ops Platform + plugins/Network         |
 * | Support  | (none — Plugin is peer Mission) |                                                  |
 *
 * Governance (Vision / Blueprint / Standards / …) lives in the shell User menu
 * — cross-domain reference library, not a daily-ops rail group.
 *
 * Engineer is fate-isolated from the system it services (bootstrap paradox, D7).
 * `CONSOLE_NAV_PLANE_BY_TAB` plane labels stay unchanged (decoupled from render).
 */

export const TASK_CC_NAV_ITEM: ShellNavItem = {
  id: 'task-cc',
  label: 'Task Control Center',
  icon: ListTodo,
  shortLabel: 'T',
}

/** Pinned Mission Control rail — high-frequency “now” surfaces only. */
export const MISSION_CONTROL_ITEMS: ShellNavItem[] = [
  { id: 'control-room', label: 'Control Room', icon: Gauge },
  { id: 'observability', label: 'Observability', icon: LineChart },
]

/**
 * Seat-adjacent retrospective records — collapsible under Mission Control.
 * Patrol Log / Insight Log stay on their desks (workflow adjacency).
 */
export const MISSION_CONTROL_RECORDS_ITEMS: ShellNavItem[] = [
  { id: 'defects', label: 'Defects', icon: Microscope },
  { id: 'audit', label: 'Audit', icon: History },
]

export const MISSION_CONTROL_RECORDS_LABEL = 'Defects & Audit'

/** Build Desk — field name `lifecycle` kept; display label is Build Desk.
 * Dev Sessions lives at framework chrome (header indicator / Operator Dock), not here. */
export const ENGINEER_LIFECYCLE_ITEMS: ShellNavItem[] = [
  { id: 'briefing', label: 'Briefing', icon: ClipboardList },
  { id: 'active-session', label: 'In Flight', icon: Orbit },
  { id: 'delivery-board', label: 'Delivery', icon: Archive },
]

/**
 * Launch Desk — always under Engineer Partner (below Build Desk).
 * Domain plane: Rocket / Satellite / Plugin / Engineer (Agent = L-1 host publish).
 */
export const ENGINEER_LAUNCH_ITEMS: ShellNavItem[] = [
  { id: 'platform-release', label: 'Rocket', icon: Container },
  { id: 'trade-release', label: 'Satellite', icon: Workflow },
  { id: 'plugin-release', label: 'Plugin', icon: Workflow },
  { id: 'agent-release', label: 'Agent', icon: Bot },
]

/**
 * Ops Desk subgroups — Operate (reactive inbox) vs Patrol (scheduled skills).
 * Empty label = no sub-header (governance / capability trail).
 * Field name `workspace` kept; display label is Ops Desk.
 */
export const ENGINEER_WORKSPACE_SUBGROUPS: ShellNavSubGroup[] = [
  {
    label: 'Operate',
    items: [{ id: 'queue', label: 'Queue', icon: Bot }],
  },
  {
    label: 'Patrol',
    items: [
      { id: 'autonomous-skills', label: 'Patrol', icon: CalendarClock },
      { id: 'execution-log', label: 'Patrol Log', icon: Activity },
    ],
  },
  {
    label: '',
    items: [
      { id: 'operator-plane', label: 'Operator Plane', icon: LifeBuoy },
      { id: 'agent-governance', label: 'Trust & Autonomy', icon: ShieldCheck },
      { id: 'agent-capability', label: 'Agent Capability', icon: Network },
    ],
  },
]

/** Flat Ops Desk items (order = subgroup order) — progress / allowed-tab filters. */
export const ENGINEER_WORKSPACE_ITEMS: ShellNavItem[] = ENGINEER_WORKSPACE_SUBGROUPS.flatMap(
  g => g.items,
)

/** Analysis Desk — field name `profile` kept; display label is Analysis Desk. */
export const ENGINEER_PROFILE_ITEMS: ShellNavItem[] = [
  { id: 'analysis-workspace', label: 'Analysis Workspace', icon: BrainCircuit },
  { id: 'insight-log', label: 'Insight Log', icon: Sparkles },
  { id: 'hermes-status', label: 'Hermes Status', icon: Cpu },
]

export function filterAllowedNavItems(
  items: readonly ShellNavItem[],
  allowedTabIds: Set<string> | null,
): ShellNavItem[] {
  if (allowedTabIds == null) return [...items]
  return items.filter(item => allowedTabIds.has(item.id))
}

export function buildSeatNavItems(
  allowedTabIds: Set<string> | null,
  showTaskControlCenter: boolean,
): ShellNavItem[] {
  const base = showTaskControlCenter
    ? [TASK_CC_NAV_ITEM, ...MISSION_CONTROL_ITEMS]
    : MISSION_CONTROL_ITEMS
  return filterAllowedNavItems(base, allowedTabIds)
}

/** Defects & Audit — filtered independently so the collapsible can hide when empty. */
export function buildSeatRecordsItems(allowedTabIds: Set<string> | null): ShellNavItem[] {
  return filterAllowedNavItems(MISSION_CONTROL_RECORDS_ITEMS, allowedTabIds)
}

export type PartnerNavSections = {
  lifecycle: ShellNavItem[]
  launch: ShellNavItem[]
  /** Flat Ops Desk items (active detection / secondary open). */
  workspace: ShellNavItem[]
  /** Ops Desk Operate / Patrol / trail subgroups (sidebar chrome). */
  workspaceGroups: ShellNavSubGroup[]
  profile: ShellNavItem[]
}

export function filterAllowedNavSubGroups(
  groups: readonly ShellNavSubGroup[],
  allowedTabIds: Set<string> | null,
): ShellNavSubGroup[] {
  return groups
    .map(g => ({
      label: g.label,
      items: filterAllowedNavItems(g.items, allowedTabIds),
    }))
    .filter(g => g.items.length > 0)
}

export function buildPartnerNavSections(
  allowedTabIds: Set<string> | null,
): PartnerNavSections | null {
  const lifecycle = filterAllowedNavItems(ENGINEER_LIFECYCLE_ITEMS, allowedTabIds)
  const launch = filterAllowedNavItems(ENGINEER_LAUNCH_ITEMS, allowedTabIds)
  const workspaceGroups = filterAllowedNavSubGroups(ENGINEER_WORKSPACE_SUBGROUPS, allowedTabIds)
  const workspace = workspaceGroups.flatMap(g => g.items)
  const profile = filterAllowedNavItems(ENGINEER_PROFILE_ITEMS, allowedTabIds)
  if (lifecycle.length + launch.length + workspace.length + profile.length === 0) return null
  return { lifecycle, launch, workspace, workspaceGroups, profile }
}

export const CONSOLE_NAV_GROUPS: ShellNavGroup[] = [
  {
    label: 'Satellite',
    icon: Satellite,
    defaultOpen: true,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'satellite-bus', label: 'Bus Status', icon: Activity },
          { id: 'satellite-health', label: 'Satellite Health', icon: Gauge },
        ],
      },
    ],
  },
  {
    label: 'Rocket',
    icon: Rocket,
    defaultOpen: true,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'cluster', label: 'Cluster', icon: Server },
          { id: 'rocket-health', label: 'Rocket Health', icon: HeartPulse },
        ],
      },
    ],
  },
  {
    label: 'Plugin',
    icon: Plug,
    defaultOpen: true,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'plugin-gallery', label: 'Plugin Gallery', icon: Boxes },
          { id: 'ib-gateway-manage', label: 'IB Gateway', icon: Plug },
          { id: 'market-data-manage', label: 'Market Data', icon: Database },
        ],
      },
      {
        label: 'Infra',
        items: [{ id: 'network', label: 'Network', icon: Network }],
      },
    ],
  },
]

/**
 * Governance / Guides reference library — browsed via Settings-style Guides shell
 * (User menu → Guides). Keep in sync with CONSOLE_NAV_PLANE_BY_TAB Governance tabs.
 */
export const GOVERNANCE_MENU_GROUPS: ReadonlyArray<{
  label: string
  items: ReadonlyArray<{ id: string; label: string; icon: typeof BookOpen }>
}> = [
  {
    label: 'Strategic',
    items: [
      { id: 'flywheel-vision', label: 'Vision', icon: Orbit },
      { id: 'blueprint', label: 'Blueprint', icon: Boxes },
      { id: 'roadmap', label: 'Roadmap', icon: MapPinned },
    ],
  },
  {
    label: 'Standards',
    items: [
      { id: 'platform-standards', label: 'Platform', icon: Shield },
      { id: 'agent-protocol', label: 'Agent Protocol', icon: FileCode2 },
      { id: 'agent-system', label: 'Agent System', icon: Boxes },
      { id: 'mcp-contract', label: 'MCP Contract', icon: Plug },
      { id: 'design-system', label: 'Design System', icon: Ruler },
      { id: 'briefing-reconciliation', label: 'Briefing Reconciliation', icon: Scale },
    ],
  },
  {
    label: 'AI Strategy',
    items: [
      { id: 'ai-compute', label: 'AI Compute Strategy', icon: Cpu },
    ],
  },
]

/** Default landing tab when opening Guides from the User menu. */
export const GUIDES_DEFAULT_TAB = 'flywheel-vision' as const

export type ConsoleNavPlane =
  | 'Mission Control'
  | 'Rocket'
  | 'Ground Systems'
  | 'Satellite'
  | 'Subcontractors'
  | 'Plugin'
  | 'Engineer'
  | 'Governance'

/** Map view tab id → sidebar plane (for headers, briefing packs, catalog cross-refs). */
export const CONSOLE_NAV_PLANE_BY_TAB: Record<string, ConsoleNavPlane> = {
  'control-room': 'Mission Control',
  observability: 'Mission Control',
  'task-cc': 'Mission Control',
  'runtime-map': 'Mission Control',
  defects: 'Mission Control',
  audit: 'Mission Control',
  cluster: 'Rocket',
  'rocket-health': 'Rocket',
  'platform-release': 'Rocket',
  /** Legacy `#console` redirects to Operator Dock — plane kept for hash/breadcrumb flash. */
  console: 'Plugin',
  network: 'Plugin',
  'satellite-bus': 'Satellite',
  'satellite-health': 'Satellite',
  // Legacy aliases (redirected to satellite-health)
  'satellite-telemetry': 'Satellite',
  'satellite-api': 'Satellite',
  'trade-release': 'Satellite',
  'plugin-gallery': 'Plugin',
  'ib-gateway-manage': 'Plugin',
  'market-data-manage': 'Plugin',
  'plugin-release': 'Plugin',
  'agent-release': 'Engineer',
  queue: 'Engineer',
  /** Legacy `#agent-desk` hash alias — plane kept for breadcrumb flash before redirect. */
  'agent-desk': 'Engineer',
  'agent-capability': 'Engineer',
  briefing: 'Engineer',
  'active-session': 'Engineer',
  'delivery-board': 'Engineer',
  'dev-sessions': 'Engineer',
  'autonomous-skills': 'Engineer',
  'execution-log': 'Engineer',
  'agent-governance': 'Engineer',
  'operator-plane': 'Engineer',
  'analysis-workspace': 'Engineer',
  'insight-log': 'Engineer',
  'hermes-status': 'Engineer',
  'flywheel-vision': 'Governance',
  blueprint: 'Governance',
  roadmap: 'Governance',
  'platform-standards': 'Governance',
  'agent-protocol': 'Governance',
  'agent-system': 'Governance',
  'mcp-contract': 'Governance',
  'design-system': 'Governance',
  'briefing-reconciliation': 'Governance',
  'ai-compute': 'Governance',
}

export function consoleNavPlane(tabId: string): ConsoleNavPlane | undefined {
  return CONSOLE_NAV_PLANE_BY_TAB[tabId]
}
