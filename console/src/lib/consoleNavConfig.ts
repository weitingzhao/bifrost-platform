import type { ShellNavGroup } from '@bifrost/ui'
import {
  Activity,
  BookOpen,
  Bot,
  Boxes,
  Building2,
  CalendarClock,
  ClipboardList,
  Code2,
  Container,
  Cpu,
  FileCode2,
  Gauge,
  Handshake,
  History,
  LifeBuoy,
  LineChart,
  Map,
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
  Terminal,
  Workflow,
} from 'lucide-react'

/**
 * Ops Console sidebar — seven system-domain groups (Apollo model).
 *
 * Instead of verb-based lenses (Observe / Operate / Architecture) the
 * navigation is organised by **system domain** so each domain self-contains
 * its observe, operate, and governance dimensions.
 *
 * | Group            | Domain                | Intent                                           |
 * |------------------|-----------------------|--------------------------------------------------|
 * | Mission Control  | Cross-domain ops hub  | Flight-director big board, topology, audit        |
 * | Rocket           | Ops Platform itself   | K8s cluster, releases, scheduling                 |
 * | Ground Systems   | Infrastructure        | Server console, network, compute                  |
 * | Satellite        | Trade stack payload   | Bus status, API health (L0 probes)                |
 * | Subcontractors   | External plugins      | Delivery Board, Plugin Gallery, future plugins    |
 * | Engineer         | AI Agent              | Workspace, autonomous, trust, L-1 plane           |
 * | Governance       | Cross-domain ref lib  | Strategic vision, standards, AI strategy           |
 *
 * Engineer is fate-isolated from the system it services (bootstrap paradox, D7).
 * Governance merges the former Architecture + Agent Doctrine pages into a
 * unified reference library.
 */
export const CONSOLE_NAV_GROUPS: ShellNavGroup[] = [
  {
    label: 'Mission Control',
    icon: Gauge,
    defaultOpen: true,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'control-room', label: 'Control Room', icon: Gauge },
          { id: 'runtime-map', label: 'Runtime Map', icon: Map },
          { id: 'defects', label: 'Defects', icon: Microscope },
          { id: 'audit', label: 'Audit', icon: History },
        ],
      },
    ],
  },
  {
    label: 'Rocket',
    icon: Rocket,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'cluster', label: 'Cluster', icon: Server },
          { id: 'observability', label: 'Observability', icon: Activity },
          { id: 'trade-release', label: 'Trade Release', icon: Workflow },
          { id: 'platform-release', label: 'Platform Release', icon: Container },
          { id: 'placement', label: 'Placement', icon: Network },
        ],
      },
    ],
  },
  {
    label: 'Ground Systems',
    icon: Building2,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'console', label: 'Server Console', icon: Terminal },
          { id: 'network', label: 'Network', icon: Network },
          { id: 'compute', label: 'Compute', icon: Cpu },
        ],
      },
    ],
  },
  {
    label: 'Satellite',
    icon: Satellite,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'satellite-bus', label: 'Bus Status', icon: Activity },
          { id: 'satellite-telemetry', label: 'Telemetry', icon: LineChart },
          { id: 'satellite-api', label: 'API Health', icon: Gauge },
        ],
      },
    ],
  },
  {
    label: 'Subcontractors',
    icon: Handshake,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'delivery-board', label: 'Delivery Board', icon: ClipboardList },
          { id: 'plugin-gallery', label: 'Plugin Gallery', icon: Plug },
        ],
      },
    ],
  },
  {
    label: 'Engineer',
    icon: Bot,
    subGroups: [
      {
        label: 'Workspace',
        items: [
          { id: 'agent-desk', label: 'Agent Desk', icon: Bot },
          { id: 'briefing', label: 'Agent Briefing', icon: ClipboardList },
          { id: 'dev-agent', label: 'Dev Agent', icon: Code2 },
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
        label: 'Trust',
        items: [
          { id: 'agent-governance', label: 'Trust & Autonomy', icon: ShieldCheck },
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
    label: 'Governance',
    icon: BookOpen,
    subGroups: [
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
    ],
  },
]

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
  'runtime-map': 'Mission Control',
  defects: 'Mission Control',
  audit: 'Mission Control',
  cluster: 'Rocket',
  observability: 'Rocket',
  'trade-release': 'Rocket',
  'platform-release': 'Rocket',
  placement: 'Rocket',
  console: 'Ground Systems',
  network: 'Ground Systems',
  compute: 'Ground Systems',
  'satellite-bus': 'Satellite',
  'satellite-telemetry': 'Satellite',
  'satellite-api': 'Satellite',
  'delivery-board': 'Subcontractors',
  'plugin-gallery': 'Subcontractors',
  'agent-desk': 'Engineer',
  briefing: 'Engineer',
  'dev-agent': 'Engineer',
  'autonomous-skills': 'Engineer',
  'execution-log': 'Engineer',
  'agent-governance': 'Engineer',
  'operator-plane': 'Engineer',
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
