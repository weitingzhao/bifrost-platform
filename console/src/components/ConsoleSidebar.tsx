import type { ReactNode } from 'react'
import { DomainNav } from './shell/DomainNav'
import { entryOf, resolveConsoleHash } from '@/lib/shell/consoleRoutes'
import {
  SidebarMenu,
  SidebarMenuSub,
  ShellNavSidebar,
  shellNavSubItemIconClass,
  type ShellNavItem,
} from '@bifrost/ui'
import { ConsoleNavSlotItem, type ConsoleNavBadge } from '@/components/shell/ConsoleNavSlotItem'
import { TradeMonitoringPeerLinks } from '@/components/TradeMonitoringPeerLinks'
import type { FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'
import { shellEntryItems } from '@/lib/shell/shellNavItems'

/** Desktop navigation: five domains and the current domain’s secondary links. */
export function ConsoleSidebar({
  activeTab,
  onSelect,
  viewerEnv,
  viewerEnvLoading,
  badges,
  local = false,
}: {
  local?: boolean
  activeTab: string
  onSelect: (id: string) => void
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
  badges?: Partial<Record<string, ConsoleNavBadge>>
}) {
  const entries = shellEntryItems()
  const resolved = resolveConsoleHash(window.location.hash.slice(1))
  const domain = entryOf(resolved.location, resolved.canonical.split('?')[1])
  const row = (item: ShellNavItem, collapsed: boolean) => (
    <ConsoleNavSlotItem
      key={item.id}
      item={item}
      activeId={domain ?? activeTab}
      onSelect={onSelect}
      collapsed={collapsed || undefined}
      badge={badges?.[item.id]}
      renderItemIcon={
        collapsed
          ? undefined
          : entry => {
              const Icon = entry.icon
              if (Icon == null) return null
              return <Icon className={shellNavSubItemIconClass} aria-hidden />
            }
      }
    />
  )
  const seat = (collapsed: boolean): ReactNode => (
    <nav aria-label="Ops">
      {collapsed ? (
        <div className="flex flex-col gap-1 px-1 py-1.5">
          {entries.map(item => row(item, true))}
        </div>
      ) : (
        <SidebarMenu>
          <SidebarMenuSub>{entries.map(item => row(item, false))}</SidebarMenuSub>
          {domain && <li className="list-none"><DomainNav entry={domain} local={local} vertical /></li>}
        </SidebarMenu>
      )}
    </nav>
  )

  return (
    <ShellNavSidebar
      productName="Bifrost Ops"
      productBadge="Ops"
      navGroups={[]}
      activeId={domain ?? activeTab}
      onSelect={item => onSelect(item.id)}
      storageKey="bifrost-ops-shell"
      seatContent={seat}
      footer={
        <TradeMonitoringPeerLinks viewerEnv={viewerEnv} viewerEnvLoading={viewerEnvLoading} />
      }
    />
  )
}
