import type { ShellNavGroup } from '@bifrost/ui'
import {
  BookOpen,
  Boxes,
  ClipboardList,
  Container,
  Database,
  Eye,
  FileCode2,
  Gauge,
  GitBranch,
  History,
  Map,
  MapPinned,
  Milestone,
  Network,
  Orbit,
  Plug,
  PlugZap,
  Rocket,
  Ruler,
  Server,
  Shield,
  Terminal,
  Workflow,
  Zap,
} from 'lucide-react'

/**
 * Ops Console sidebar — three top-level planes (mirrors Delivery page Operate / Observe / Blueprint).
 *
 * | Plane        | Intent                                      |
 * |--------------|---------------------------------------------|
 * | Operate      | L1/L2 actuation — release, cluster, tools   |
 * | Observe      | L0 probes — live status, audit, briefing    |
 * | Architecture | PLAN static — governance, K3s, standards    |
 */
export const CONSOLE_NAV_GROUPS: ShellNavGroup[] = [
  {
    label: 'Operate',
    icon: Zap,
    defaultOpen: true,
    subGroups: [
      {
        label: 'Release',
        items: [
          { id: 'delivery', label: 'Delivery', icon: Workflow },
          { id: 'promote', label: 'Promote', icon: Rocket },
          { id: 'deploy-mainline', label: 'Deploy Mainline', icon: GitBranch },
        ],
      },
      {
        label: 'Cluster',
        items: [
          { id: 'cluster', label: 'Cluster', icon: Server },
          { id: 'placement', label: 'Placement', icon: Network },
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
        label: 'Overview',
        items: [
          { id: 'control-room', label: 'Control Room', icon: Gauge },
          { id: 'briefing', label: 'Agent Briefing', icon: ClipboardList },
        ],
      },
      {
        label: 'Runtime',
        items: [
          { id: 'runtime-map', label: 'Runtime Map', icon: Map },
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
          { id: 'data-layer', label: 'Data Layer', icon: Database },
        ],
      },
      {
        label: 'Standards',
        items: [
          { id: 'platform-standards', label: 'Platform', icon: Shield },
          { id: 'agent-protocol', label: 'Agent Protocol', icon: FileCode2 },
          { id: 'mcp-contract', label: 'MCP Contract', icon: Plug },
          { id: 'design-system', label: 'Design System', icon: Ruler },
        ],
      },
    ],
  },
]

/** Map view tab id → sidebar plane (for headers, briefing packs, catalog cross-refs). */
export const CONSOLE_NAV_PLANE_BY_TAB: Record<string, 'Operate' | 'Observe' | 'Architecture'> = {
  delivery: 'Operate',
  promote: 'Operate',
  'deploy-mainline': 'Operate',
  cluster: 'Operate',
  placement: 'Operate',
  console: 'Operate',
  'control-room': 'Observe',
  briefing: 'Observe',
  'runtime-map': 'Observe',
  audit: 'Observe',
  blueprint: 'Architecture',
  'flywheel-vision': 'Architecture',
  program: 'Architecture',
  environments: 'Architecture',
  roadmap: 'Architecture',
  'k3s-architecture': 'Architecture',
  'k3s-bootstrap': 'Architecture',
  'data-layer': 'Architecture',
  'platform-standards': 'Architecture',
  'agent-protocol': 'Architecture',
  'mcp-contract': 'Architecture',
  'design-system': 'Architecture',
}

export function consoleNavPlane(tabId: string): 'Operate' | 'Observe' | 'Architecture' | undefined {
  return CONSOLE_NAV_PLANE_BY_TAB[tabId]
}
