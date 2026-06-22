import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  BarChart3,
  Boxes,
  Cpu,
  Database,
  GitBranch,
  Layers,
  LayoutPanelLeft,
  MemoryStick,
  Server,
  Warehouse,
  Workflow,
} from 'lucide-react'

/** Lucide icon per K8s namespace — chip / subtoolbar identity. */
const NS_ICONS: Record<string, LucideIcon> = {
  'bifrost-dev': BarChart3,
  'bifrost-stg': BarChart3,
  'bifrost-prod': BarChart3,
  'bifrost-platform-stg': LayoutPanelLeft,
  'cnpg-system': Database,
  data: MemoryStick,
  'data-warehouse': Warehouse,
  ai: Cpu,
  cicd: GitBranch,
  'tekton-pipelines': Workflow,
  'tekton-pipelines-resolvers': Layers,
  'kube-system': Server,
  monitoring: Activity,
}

export function namespaceIcon(k8sName: string): LucideIcon {
  return NS_ICONS[k8sName] ?? Boxes
}
