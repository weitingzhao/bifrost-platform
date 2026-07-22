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
 * System Domain SSOT (ids + purpose + scope→domain): systemDomainCatalog.ts
 *
 * Instead of verb-based lenses (Observe / Operate / Architecture) the
 * navigation is organised by **system domain** so each domain self-contains
 * its observe, operate, and governance dimensions.
 *
 * | Group            | Domain                | Intent                                           |
 * |------------------|-----------------------|--------------------------------------------------|
 * | Mission Control  | Cross-domain ops hub  | Control Room, Observability, defects, audit, delivery |
 * | Rocket           | Ops Platform itself   | K8s cluster, Launch Rocket, placement             |
 * | Ground Systems   | Infrastructure        | Server console, network, compute                  |
 * | Satellite        | Payload satellite(s)  | Bus, runtime, API & Auth Probes, Deploy Satellite |
 * | Subcontractors   | External plugins      | Plugin Gallery, future plugins                    |
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
          { id: 'observability', label: 'Observability', icon: LineChart },
          { id: 'defects', label: 'Defects', icon: Microscope },
          { id: 'audit', label: 'Audit', icon: History },
          { id: 'delivery-board', label: 'Delivery Board', icon: ClipboardList },
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
    label: 'Engineer',
    icon: Bot,
    subGroups: [
      {
        label: 'Workspace',
        items: [
          { id: 'agent-desk', label: 'Agent Desk', icon: Bot },
          { id: 'agent-capability', label: 'Agent Capability', icon: Network },
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
    label: 'Subcontractors',
    icon: Handshake,
    subGroups: [
      {
        label: '',
        items: [
          { id: 'plugin-gallery', label: 'Plugin Gallery', icon: Plug },
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
  observability: 'Mission Control',
  'task-cc': 'Mission Control',
  'runtime-map': 'Mission Control',
  defects: 'Mission Control',
  audit: 'Mission Control',
  'delivery-board': 'Mission Control',
  cluster: 'Rocket',
  'platform-release': 'Rocket',
  placement: 'Rocket',
  console: 'Ground Systems',
  network: 'Ground Systems',
  compute: 'Ground Systems',
  'satellite-bus': 'Satellite',
  'satellite-telemetry': 'Satellite',
  'satellite-api': 'Satellite',
  'trade-release': 'Satellite',
  'plugin-gallery': 'Subcontractors',
  'agent-desk': 'Engineer',
  'agent-capability': 'Engineer',
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
