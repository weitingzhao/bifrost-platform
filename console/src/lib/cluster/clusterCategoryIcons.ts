import type { LucideIcon } from 'lucide-react'
import {
  Database,
  MemoryStick,
  Gpu,
  Warehouse,
  Cog,
  AppWindow,
  GitBranch,
  Server,
  Boxes,
  ShieldCheck,
  Activity,
} from 'lucide-react'
import type { ClusterCategory } from '@/lib/cluster/clusterCategories'
import { isInfrastructureCategory } from '@/lib/cluster/clusterCategories'

const APPLICATION_ICONS: Record<string, LucideIcon> = {
  database: Database,
  redis: MemoryStick,
  gpu: Gpu,
  warehouse: Warehouse,
  workers: Cog,
  applications: AppWindow,
  cicd: GitBranch,
}

const INFRASTRUCTURE_ICONS: Record<string, LucideIcon> = {
  nodes: Server,
  workloads: Boxes,
  governance: ShieldCheck,
  observability: Activity,
}

export function categoryIcon(category: ClusterCategory): LucideIcon | undefined {
  if (isInfrastructureCategory(category)) {
    return INFRASTRUCTURE_ICONS[category]
  }
  return APPLICATION_ICONS[category]
}
