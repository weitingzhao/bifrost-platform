import type { ShellNavItem } from '@bifrost/ui'
import {
  Activity,
  Archive,
  Database,
  GitBranch,
  Inbox,
  Plug,
  Rocket,
  Server,
} from 'lucide-react'
import { SHELL_NAV, type ShellRouteId } from '@/lib/shell/consoleRoutes'

const ICONS: Record<ShellRouteId, ShellNavItem['icon']> = {
  'needs-you': Inbox,
  status: Activity,
  data: Database,
  ib: Plug,
  releases: Rocket,
  infrastructure: Server,
  progress: GitBranch,
  records: Archive,
}

/** One layer. No System / Ops / Analysis lens, and no per-lens hiding. */
export function shellSidebarItems(): ShellNavItem[] {
  return SHELL_NAV.map(item => ({
    id: item.id,
    label: item.label,
    icon: ICONS[item.id],
  }))
}
