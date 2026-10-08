import type { ReactNode } from 'react'
import {
  SidebarMenu,
  SidebarMenuSub,
  ShellNavSidebar,
  shellNavSubItemIconClass,
} from '@bifrost/ui'
import { ConsoleNavSlotItem } from '@/components/shell/ConsoleNavSlotItem'
import { TradeMonitoringPeerLinks } from '@/components/TradeMonitoringPeerLinks'
import type { FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'
import { shellSidebarItems } from '@/lib/shell/shellNavItems'

export function ConsoleSidebar({
  activeTab,
  onSelect,
  viewerEnv,
  viewerEnvLoading,
}: {
  activeTab: string
  onSelect: (id: string) => void
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
}) {
  const items = shellSidebarItems()
  const seat = (collapsed: boolean): ReactNode => (
    <nav aria-label="Ops">
      {collapsed ? (
        <div className="flex flex-col gap-1 px-1 py-1.5">
          {items.map(item => (
            <ConsoleNavSlotItem
              key={item.id}
              item={item}
              activeId={activeTab}
              onSelect={onSelect}
              collapsed
            />
          ))}
        </div>
      ) : (
        <SidebarMenu>
          <SidebarMenuSub>
            {items.map(item => (
              <ConsoleNavSlotItem
                key={item.id}
                item={item}
                activeId={activeTab}
                onSelect={onSelect}
                renderItemIcon={entry => {
                  const Icon = entry.icon
                  if (Icon == null) return null
                  return <Icon className={shellNavSubItemIconClass} aria-hidden />
                }}
              />
            ))}
          </SidebarMenuSub>
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
