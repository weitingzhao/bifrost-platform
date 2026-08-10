import { useMemo, type ReactNode } from 'react'
import {
  SidebarMenu,
  SidebarMenuSub,
  type ShellNavItem,
} from '@bifrost/ui'
import { buildSeatNavItems } from '@/lib/consoleNavConfig'
import { ConsoleNavSlotItem } from '@/components/shell/ConsoleNavSlotItem'

function resolveIdChecker(
  ids: Set<string> | string[] | undefined,
): ((id: string) => boolean) | undefined {
  if (ids == null) return undefined
  if (ids instanceof Set) return (id: string) => ids.has(id)
  const set = new Set(ids)
  return (id: string) => set.has(id)
}

export function SeatStrip({
  collapsed,
  activeId,
  onSelect,
  allowedTabIds,
  showTaskControlCenter = false,
  renderItemIcon,
  dimmedIds,
  phaseFocusIds,
}: {
  collapsed: boolean
  activeId: string
  onSelect: (id: string) => void
  allowedTabIds: Set<string> | null
  showTaskControlCenter?: boolean
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  dimmedIds?: Set<string> | string[]
  phaseFocusIds?: Set<string> | string[]
}) {
  const items = useMemo(
    () => buildSeatNavItems(allowedTabIds, showTaskControlCenter),
    [allowedTabIds, showTaskControlCenter],
  )
  const signals = useMemo(
    () => ({
      isDimmed: resolveIdChecker(dimmedIds),
      isPhaseFocus: resolveIdChecker(phaseFocusIds),
    }),
    [dimmedIds, phaseFocusIds],
  )

  if (items.length === 0) return null

  if (collapsed) {
    return (
      <div className="flex flex-col gap-1 px-1 py-1.5" aria-label="Mission Control">
        {items.map(item => (
          <ConsoleNavSlotItem
            key={item.id}
            item={item}
            activeId={activeId}
            onSelect={onSelect}
            renderItemIcon={renderItemIcon}
            collapsed
            signals={signals}
          />
        ))}
      </div>
    )
  }

  return (
    <div aria-label="Mission Control">
      <p className="px-3 pt-2 pb-0.5 text-dense-caption font-medium uppercase tracking-wide text-muted-foreground">
        Mission Control
      </p>
      <SidebarMenu>
        <SidebarMenuSub>
          {items.map(item => (
            <ConsoleNavSlotItem
              key={item.id}
              item={item}
              activeId={activeId}
              onSelect={onSelect}
              renderItemIcon={renderItemIcon}
              signals={signals}
            />
          ))}
        </SidebarMenuSub>
      </SidebarMenu>
    </div>
  )
}
