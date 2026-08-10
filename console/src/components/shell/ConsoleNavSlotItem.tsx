import type { ReactNode } from 'react'
import {
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn,
  shellNavCollapsedIconButtonClass,
  shellNavFlyoutItemBaseClass,
  shellNavFlyoutItemClass,
  shellNavItemSignalClass,
  shellNavItemSignalTitle,
  shellNavSubItemButtonClassName,
  shellNavSubItemIconClass,
  type ShellNavItem,
} from '@bifrost/ui'

export type ConsoleNavSlotSignals = {
  isDimmed?: (id: string) => boolean
  isPhaseFocus?: (id: string) => boolean
}

export function ConsoleNavSlotItem({
  item,
  activeId,
  onSelect,
  renderItemIcon,
  collapsed,
  leading,
  signals,
  flyout,
}: {
  item: ShellNavItem
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  collapsed?: boolean
  leading?: ReactNode
  signals?: ConsoleNavSlotSignals
  flyout?: boolean
}) {
  const isActive = item.id === activeId
  const phaseFocus = signals?.isPhaseFocus?.(item.id) === true
  const offPhase = !isActive && signals?.isDimmed?.(item.id) === true
  const signalClass = shellNavItemSignalClass({ phaseFocus, offPhase })
  const signalTitle = shellNavItemSignalTitle({ isActive, phaseFocus, offPhase })
  const ItemIcon = item.icon
  const icon =
    renderItemIcon != null
      ? renderItemIcon(item)
      : ItemIcon != null
        ? <ItemIcon className={shellNavSubItemIconClass} aria-hidden />
        : null

  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            className={cn(shellNavCollapsedIconButtonClass(isActive), signalClass)}
            title={signalTitle}
            onClick={() => onSelect(item.id)}
          >
            {icon}
          </button>
        </TooltipTrigger>
        <TooltipContent side="right" className="text-xs font-medium">
          {item.label}
        </TooltipContent>
      </Tooltip>
    )
  }

  if (flyout) {
    return (
      <button
        type="button"
        onClick={() => onSelect(item.id)}
        className={cn(
          shellNavFlyoutItemBaseClass,
          'w-full px-2.5',
          shellNavFlyoutItemClass(isActive),
          signalClass,
        )}
        title={signalTitle}
      >
        {leading}
        {icon}
        <span className="flex-1 text-left">{item.label}</span>
      </button>
    )
  }

  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton
        isActive={isActive}
        className={shellNavSubItemButtonClassName({ className: signalClass })}
        title={signalTitle}
        onClick={() => onSelect(item.id)}
      >
        {leading}
        {icon}
        <span className="flex-1">{item.label}</span>
      </SidebarMenuSubButton>
    </SidebarMenuSubItem>
  )
}
