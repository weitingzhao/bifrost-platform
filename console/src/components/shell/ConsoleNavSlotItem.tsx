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

/** Count pill on a nav row. `unknown` is drawn muted so it never reads as zero. */
export type ConsoleNavBadge = {
  text: string
  title: string
  tone: 'attention' | 'unknown'
}

function NavBadge({ badge, collapsed }: { badge: ConsoleNavBadge; collapsed?: boolean }) {
  return (
    <span
      data-nav-badge={badge.tone}
      title={badge.title}
      aria-label={badge.title}
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-full font-semibold tabular-nums leading-none',
        collapsed
          ? 'absolute -right-1 -top-1 h-3.5 min-w-3.5 px-0.5 text-[9px]'
          : 'h-4 min-w-4 px-1 text-[10px]',
        badge.tone === 'attention'
          ? 'bg-[var(--color-lamp-red)] text-white'
          : 'bg-muted text-muted-foreground',
      )}
    >
      {badge.text}
    </span>
  )
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
  badge,
}: {
  item: ShellNavItem
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  collapsed?: boolean
  leading?: ReactNode
  signals?: ConsoleNavSlotSignals
  flyout?: boolean
  badge?: ConsoleNavBadge
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

  const label = <span className="flex-1 truncate text-left">{item.label}</span>

  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="relative">
            <button
              type="button"
              className={cn(shellNavCollapsedIconButtonClass(isActive), signalClass)}
              title={signalTitle}
              onClick={() => onSelect(item.id)}
            >
              {icon}
            </button>
            {badge != null ? <NavBadge badge={badge} collapsed /> : null}
          </div>
        </TooltipTrigger>
        <TooltipContent side="right" className="text-xs font-medium">
          {item.label}
        </TooltipContent>
      </Tooltip>
    )
  }

  if (flyout) {
    return (
      <div className="flex min-w-0 items-center gap-0.5">
        <button
          type="button"
          onClick={() => onSelect(item.id)}
          className={cn(
            shellNavFlyoutItemBaseClass,
            'min-w-0 flex-1 px-2.5',
            shellNavFlyoutItemClass(isActive),
            signalClass,
          )}
          title={signalTitle}
        >
          {leading}
          {icon}
          {label}
          {badge != null ? <NavBadge badge={badge} /> : null}
        </button>
      </div>
    )
  }

  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton
        isActive={isActive}
        className={shellNavSubItemButtonClassName({ flex: true, className: signalClass })}
        title={signalTitle}
        onClick={() => onSelect(item.id)}
      >
        {leading}
        {icon}
        {label}
        {badge != null ? <NavBadge badge={badge} /> : null}
      </SidebarMenuSubButton>
    </SidebarMenuSubItem>
  )
}
