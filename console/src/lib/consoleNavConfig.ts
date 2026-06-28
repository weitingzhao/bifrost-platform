import type { ShellNavGroup } from '@bifrost/ui'
import {
  Activity,
  BookOpen,
  Boxes,
  Bot,
  CalendarClock,
  ClipboardList,
  Container,
  Database,
  Eye,
  FileCode2,
  Gauge,
  GitBranch,
  History,
  LifeBuoy,
  Map,
  MapPinned,
  Microscope,
  Milestone,
  Network,
  Orbit,
  Plug,
  PlugZap,
  Rocket,
  Ruler,
  Server,
  Shield,
  ShieldCheck,
  Terminal,
  Wifi,
  Workflow,
  Zap,
} from 'lucide-react'

/**
 * Ops Console sidebar — four top-level groups.
 *
 * Three are LENSES on the managed system (rocket = Ops Platform, payload = Trade);
 * one is the ACTOR's own home (engineer = AI Agent). The lenses describe what the
 * Owner does to the system; the Agent plane is everything ABOUT the engineer itself.
 *
 * | Group        | Axis    | Intent                                              |
 * |--------------|---------|-----------------------------------------------------|
 * | Agent        | actor   | The engineer's home — workspace, doctrine, L-1 plane|
 * | Operate      | lens    | L1/L2 actuation — release, cluster, tools           |
 * | Observe      | lens    | L0 probes — live status, audit                      |
 * | Architecture | lens    | PLAN static — rocket governance, K3s, standards     |
 *
 * Agent is fate-isolated from the system it services (bootstrap paradox, decision D7):
 *   Agent → Workspace: Agent Desk (dispatch) · Agent Briefing (session entry)
 *   Agent → Autonomous: Skills (Hermes scheduled) · Execution Log (history)
 *   Agent → Governance: Performance (KPIs) · Trust & Autonomy (per-Skill L0/L1/L2)
 *   Agent → Doctrine: Agent Protocol (modes) · MCP Contract (tool contract)
 *   Agent → Operator Plane (L-1): runner + Hermes heartbeats, dual-Mini deploy, watchdog
 */
export const CONSOLE_NAV_GROUPS: ShellNavGroup[] = [
  {
    label: 'Agent',
    icon: Bot,
    defaultOpen: true,
    subGroups: [
      {
        label: 'Workspace',
        items: [
          { id: 'agent-desk', label: 'Agent Desk', icon: Bot },
          { id: 'briefing', label: 'Agent Briefing', icon: ClipboardList },
        ],
      },
      {
        label: 'Autonomous',
        items: [
          { id: 'autonomous-skills', label: 'Skills & Schedules', icon: CalendarClock },
          { id: 'execution-log', label: 'Execution Log', icon: Activity },
        ],
      },
      {
        label: 'Governance',
        items: [
          { id: 'agent-governance', label: 'Trust & Autonomy', icon: ShieldCheck },
        ],
      },
      {
        label: 'Doctrine',
        items: [
          { id: 'agent-protocol', label: 'Agent Protocol', icon: FileCode2 },
          { id: 'mcp-contract', label: 'MCP Contract', icon: Plug },
        ],
      },
      {
        label: 'Operator Plane (L-1)',
        items: [
          { id: 'operator-plane', label: 'Operator Plane', icon: LifeBuoy },
        ],
      },
    ],
  },
  {
    label: 'Operate',
    icon: Zap,
    subGroups: [
      {
        label: 'Trade release',
        items: [
          { id: 'delivery', label: 'Delivery', icon: Workflow },
          { id: 'promote', label: 'Promote', icon: Rocket },
          { id: 'deploy-mainline', label: 'Deploy Mainline', icon: GitBranch },
        ],
      },
      {
        label: 'Platform',
        items: [
          { id: 'platform-release', label: 'Platform Release', icon: Container },
        ],
      },
      {
        label: 'Cluster ops',
        items: [
          { id: 'cluster', label: 'Cluster', icon: Server },
          { id: 'console', label: 'Server Console', icon: Terminal },
        ],
      },
    ],
  },
  {
    label: 'Observe',
    icon: Eye,
    subGroups: [
      {
        label: 'Diagnosis',
        items: [
          { id: 'control-room', label: 'Control Room', icon: Gauge },
          { id: 'runtime-map', label: 'Runtime Map', icon: Map },
          { id: 'defects', label: 'Defects', icon: Microscope },
        ],
      },
      {
        label: 'Scheduling',
        items: [{ id: 'placement', label: 'Placement', icon: Network }],
      },
      {
        label: 'Audit',
        items: [
          { id: 'audit', label: 'Audit', icon: History },
        ],
      },
    ],
  },
  {
    label: 'Architecture',
    icon: Boxes,
    subGroups: [
      {
        label: 'Governance',
        items: [
          { id: 'flywheel-vision', label: 'Vision', icon: Orbit },
          { id: 'blueprint', label: 'Blueprint', icon: Boxes },
          { id: 'roadmap', label: 'Roadmap', icon: MapPinned },
          { id: 'program', label: 'Milestones', icon: Milestone },
          { id: 'environments', label: 'Environments', icon: BookOpen },
        ],
      },
      {
        label: 'K3s',
        items: [
          { id: 'k3s-architecture', label: 'K3s Architecture', icon: Container },
          { id: 'k3s-bootstrap', label: 'K3s Bootstrap', icon: PlugZap },
          { id: 'cicd-bootstrap', label: 'CI/CD Bootstrap', icon: GitBranch },
          { id: 'data-layer', label: 'Data Layer', icon: Database },
          { id: 'network-upgrade', label: 'Network Upgrade', icon: Wifi },
        ],
      },
      {
        label: 'Standards',
        items: [
          { id: 'platform-standards', label: 'Platform', icon: Shield },
          { id: 'design-system', label: 'Design System', icon: Ruler },
        ],
      },
    ],
  },
]

export type ConsoleNavPlane = 'Agent' | 'Operate' | 'Observe' | 'Architecture'

/** Map view tab id → sidebar plane (for headers, briefing packs, catalog cross-refs). */
export const CONSOLE_NAV_PLANE_BY_TAB: Record<string, ConsoleNavPlane> = {
  'agent-desk': 'Agent',
  briefing: 'Agent',
  'autonomous-skills': 'Agent',
  'execution-log': 'Agent',
  'agent-governance': 'Agent',
  'agent-protocol': 'Agent',
  'mcp-contract': 'Agent',
  'operator-plane': 'Agent',
  delivery: 'Operate',
  promote: 'Operate',
  'deploy-mainline': 'Operate',
  'platform-release': 'Operate',
  cluster: 'Operate',
  placement: 'Observe',
  console: 'Operate',
  'control-room': 'Observe',
  'runtime-map': 'Observe',
  audit: 'Observe',
  defects: 'Observe',
  blueprint: 'Architecture',
  'flywheel-vision': 'Architecture',
  program: 'Architecture',
  environments: 'Architecture',
  roadmap: 'Architecture',
  'k3s-architecture': 'Architecture',
  'k3s-bootstrap': 'Architecture',
  'cicd-bootstrap': 'Architecture',
  'data-layer': 'Architecture',
  'network-upgrade': 'Architecture',
  'platform-standards': 'Architecture',
  'design-system': 'Architecture',
}

export function consoleNavPlane(tabId: string): ConsoleNavPlane | undefined {
  return CONSOLE_NAV_PLANE_BY_TAB[tabId]
}
