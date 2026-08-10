import type { ShellNavGroup, ShellNavItem } from '@bifrost/ui'
import {
  Activity,
  Archive,
  BookOpen,
  Bot,
  Boxes,
  BrainCircuit,
  Building2,
  CalendarClock,
  ClipboardList,
  Container,
  Cpu,
  Database,
  FileCode2,
  Gauge,
  Handshake,
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
  Terminal,
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
 * | Seat     | Mission Control items (fixed)   | Execute (TCC) → Posture → Health; defects, audit |
 * | Partner  | Engineer strip (persona)        | Build Desk always visible; Ops Desk / Analysis Desk |
 * | Mission  | Satellite → Rocket (pig)        | Payload + Ops Platform                           |
 * | Support  | Ground Systems → Subcontractors | Infra + plugins (quieter, default collapsed)     |
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

export const MISSION_CONTROL_ITEMS: ShellNavItem[] = [
  { id: 'control-room', label: 'Control Room', icon: Gauge },
  { id: 'observability', label: 'Observability', icon: LineChart },
  { id: 'defects', label: 'Defects', icon: Microscope },
  { id: 'audit', label: 'Audit', icon: History },
]

/** Build Desk — field name `lifecycle` kept; display label is Build Desk. */
export const ENGINEER_LIFECYCLE_ITEMS: ShellNavItem[] = [
  { id: 'briefing', label: 'Briefing', icon: ClipboardList },
  { id: 'active-session', label: 'In Flight', icon: Orbit },
  { id: 'delivery-board', label: 'Delivery', icon: Archive },
  { id: 'dev-sessions', label: 'Dev Sessions', icon: Terminal },
]

/** Ops Desk — field name `workspace` kept; display label is Ops Desk. */
export const ENGINEER_WORKSPACE_ITEMS: ShellNavItem[] = [
  { id: 'queue', label: 'Queue', icon: Bot },
  { id: 'autonomous-skills', label: 'Patrol', icon: CalendarClock },
  { id: 'execution-log', label: 'Execution Log', icon: Activity },
  { id: 'operator-plane', label: 'Operator Plane', icon: LifeBuoy },
  { id: 'agent-governance', label: 'Trust & Autonomy', icon: ShieldCheck },
  { id: 'agent-capability', label: 'Agent Capability', icon: Network },
]

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

export type PartnerNavSections = {
  lifecycle: ShellNavItem[]
  workspace: ShellNavItem[]
  profile: ShellNavItem[]
}

export function buildPartnerNavSections(
  allowedTabIds: Set<string> | null,
): PartnerNavSections | null {
  const lifecycle = filterAllowedNavItems(ENGINEER_LIFECYCLE_ITEMS, allowedTabIds)
  const workspace = filterAllowedNavItems(ENGINEER_WORKSPACE_ITEMS, allowedTabIds)
  const profile = filterAllowedNavItems(ENGINEER_PROFILE_ITEMS, allowedTabIds)
  if (lifecycle.length + workspace.length + profile.length === 0) return null
  return { lifecycle, workspace, profile }
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
          { id: 'satellite-telemetry', label: 'Satellite Runtime', icon: LineChart },
          { id: 'satellite-api', label: 'API & Auth Probes', icon: Gauge },
          { id: 'trade-release', label: 'Deploy Satellite', icon: Workflow },
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
          { id: 'platform-release', label: 'Launch Rocket', icon: Container },
          { id: 'placement', label: 'Placement', icon: Network },
        ],
      },
    ],
  },
  {
    label: 'Ground Systems',
    icon: Building2,
    defaultOpen: false,
    dividerBefore: true,
    emphasis: 'secondary',
    subGroups: [
      {
        label: '',
        items: [
          { id: 'network', label: 'Network', icon: Network },
          { id: 'compute', label: 'Compute', icon: Cpu },
        ],
      },
    ],
  },
  {
    label: 'Subcontractors',
    icon: Handshake,
    defaultOpen: false,
    emphasis: 'secondary',
    subGroups: [
      {
        label: '',
        items: [
          { id: 'plugin-gallery', label: 'Plugin Gallery', icon: Plug },
          { id: 'market-data-manage', label: 'Market Data', icon: Database },
          { id: 'plugin-release', label: 'Launch Plugin', icon: Workflow },
        ],
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
  'platform-release': 'Rocket',
  placement: 'Rocket',
  /** Legacy `#console` redirects to Operator Dock — plane kept for hash/breadcrumb flash. */
  console: 'Ground Systems',
  network: 'Ground Systems',
  compute: 'Ground Systems',
  'satellite-bus': 'Satellite',
  'satellite-telemetry': 'Satellite',
  'satellite-api': 'Satellite',
  'trade-release': 'Satellite',
  'plugin-gallery': 'Subcontractors',
  'market-data-manage': 'Subcontractors',
  'plugin-release': 'Subcontractors',
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
