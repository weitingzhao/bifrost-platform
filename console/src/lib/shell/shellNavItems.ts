import type { ShellNavItem } from '@bifrost/ui'
import {
  Activity,
  Archive,
  Database,
  Gauge,
  Inbox,
  ListChecks,
  MessageSquare,
  Plug,
  Rocket,
  Server,
} from 'lucide-react'
import { SHELL_NAV, type ShellRouteId } from '@/lib/shell/consoleRoutes'

const ICONS: Record<ShellRouteId, ShellNavItem['icon']> = {
  you: Inbox,
  chief: MessageSquare,
  work: ListChecks,
  system: Activity,
  status: Gauge,
  data: Database,
  ib: Plug,
  releases: Rocket,
  infrastructure: Server,
  records: Archive,
}

function navItem(item: (typeof SHELL_NAV)[number]): ShellNavItem {
  return { id: item.id, label: item.label, icon: ICONS[item.id] }
}

/** The four entries: For you, Chief of staff, Work, System. */
export function shellEntryItems(): ShellNavItem[] {
  return SHELL_NAV.filter(item => item.group === 'entry').map(navItem)
}

/** The detail pages, in the order "Detail pages" and More list them. */
export function shellDetailItems(): ShellNavItem[] {
  return SHELL_NAV.filter(item => item.group === 'detail').map(navItem)
}

/** Every row of the desktop side nav: the entries, then the detail pages. */
export function shellSidebarItems(): ShellNavItem[] {
  return [...shellEntryItems(), ...shellDetailItems()]
}
