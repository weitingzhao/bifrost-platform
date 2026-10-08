import type { ShellNavItem } from '@bifrost/ui'
import {
  Activity,
  ClipboardCheck,
  Database,
  GitBranch,
  Plug,
  Rocket,
  Server,
} from 'lucide-react'
import { SHELL_NAV, type ShellRouteId } from '@/lib/shell/consoleRoutes'

const ICONS: Record<ShellRouteId, ShellNavItem['icon']> = {
  status: Activity,
  data: Database,
  ib: Plug,
  maintenance: ClipboardCheck,
  releases: Rocket,
  infrastructure: Server,
  progress: GitBranch,
}

/** One layer. No System / Ops / Analysis lens, and no per-lens hiding. */
export function shellSidebarItems(): ShellNavItem[] {
  return SHELL_NAV.map(item => ({
    id: item.id,
    label: item.label,
    icon: ICONS[item.id],
  }))
}
