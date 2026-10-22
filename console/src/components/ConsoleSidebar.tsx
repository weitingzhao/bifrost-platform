import type { ReactNode } from 'react'
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
import { shellDetailItems, shellEntryItems } from '@/lib/shell/shellNavItems'

/**
 * The desktop side nav (1440 px and wider): the four entries, then "Detail pages" — the only place
 * every detail page is listed by name. A detail page that is open lights its own row.
 */
export function ConsoleSidebar({
  activeTab,
  onSelect,
  viewerEnv,
  viewerEnvLoading,
  badges,
}: {
  activeTab: string
  onSelect: (id: string) => void
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
  badges?: Partial<Record<string, ConsoleNavBadge>>
}) {
  const entries = shellEntryItems()
  const details = shellDetailItems()
  const row = (item: ShellNavItem, collapsed: boolean) => (
    <ConsoleNavSlotItem
      key={item.id}
      item={item}
      activeId={activeTab}
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
          <hr aria-hidden className="mx-1 my-1.5 border-[var(--table-rule)]" />
          {details.map(item => row(item, true))}
        </div>
      ) : (
        <SidebarMenu>
          <SidebarMenuSub>{entries.map(item => row(item, false))}</SidebarMenuSub>
          <li className="list-none mt-4">
            <details className="ops-detail-pages" open={details.some(item => item.id === activeTab) || undefined}>
              <summary id="console-detail-pages">Detail pages</summary>
              <SidebarMenuSub aria-labelledby="console-detail-pages">{details.map(item => row(item, false))}</SidebarMenuSub>
            </details>
          </li>
        </SidebarMenu>
      )}
    </nav>
  )

  return (
    <ShellNavSidebar
      productName="Bifrost Ops"
      productBadge="Ops"
      navGroups={[]}
      activeId={activeTab}
      onSelect={item => onSelect(item.id)}
      storageKey="bifrost-ops-shell"
      seatContent={seat}
      footer={
        <TradeMonitoringPeerLinks viewerEnv={viewerEnv} viewerEnvLoading={viewerEnvLoading} />
      }
    />
  )
}
