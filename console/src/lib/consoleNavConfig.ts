import type { ShellNavGroup } from '@bifrost/ui'
import {
  Activity,
  BookOpen,
  Boxes,
  ClipboardList,
  FileCode2,
  Gauge,
  Map,
  Milestone,
  Rocket,
  Ruler,
  Server,
  Shield,
  Terminal,
  Workflow,
} from 'lucide-react'

export const CONSOLE_NAV_GROUPS: ShellNavGroup[] = [
  {
    label: 'Ops',
    icon: Activity,
    defaultOpen: true,
    items: [
      { id: 'briefing', label: 'Agent Briefing', icon: ClipboardList },
      { id: 'control-room', label: 'Control Room', icon: Gauge },
      { id: 'pulse', label: 'Pulse', icon: Activity },
    ],
  },
  {
    label: 'Runtime',
    icon: Server,
    items: [
      { id: 'runtime-map', label: 'Runtime Map', icon: Map },
      { id: 'cluster', label: 'Cluster', icon: Server },
    ],
  },
  {
    label: 'Program',
    icon: Milestone,
    items: [
      { id: 'delivery', label: 'Delivery', icon: Workflow },
      { id: 'program', label: 'Milestones', icon: Milestone },
      { id: 'promote', label: 'Promote', icon: Rocket },
    ],
  },
  {
    label: 'Architecture',
    icon: Boxes,
    items: [
      { id: 'blueprint', label: 'Blueprint', icon: Boxes },
      { id: 'environments', label: 'Environments', icon: BookOpen },
    ],
  },
  {
    label: 'Standards',
    icon: Ruler,
    items: [
      { id: 'platform-standards', label: 'Platform', icon: Shield },
      { id: 'agent-protocol', label: 'Agent Protocol', icon: FileCode2 },
      { id: 'design-system', label: 'Design System', icon: Ruler },
    ],
  },
  {
    label: 'Tools',
    icon: Terminal,
    dividerBefore: true,
    items: [{ id: 'console', label: 'Server Console', icon: Terminal }],
  },
]
