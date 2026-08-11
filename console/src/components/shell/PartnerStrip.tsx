import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Popover,
  PopoverContent,
  PopoverTrigger,
  SidebarMenu,
  SidebarMenuSub,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn,
  shellNavCollapsedIconButtonClass,
  shellNavFlyoutSectionTitleClass,
  shellNavSubGroupSectionLabelClass,
  type ShellNavItem,
} from '@bifrost/ui'
import { Bot, ChevronDown } from 'lucide-react'
import { buildPartnerNavSections } from '@/lib/consoleNavConfig'
import { ConsoleNavSlotItem } from '@/components/shell/ConsoleNavSlotItem'

function resolveIdChecker(
  ids: Set<string> | string[] | undefined,
): ((id: string) => boolean) | undefined {
  if (ids == null) return undefined
  if (ids instanceof Set) return (id: string) => ids.has(id)
  const set = new Set(ids)
  return (id: string) => set.has(id)
}

function NumberedDeskSection({
  label,
  items,
  activeId,
  onSelect,
  renderItemIcon,
  signals,
}: {
  label: string
  items: ShellNavItem[]
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  signals: { isDimmed?: (id: string) => boolean; isPhaseFocus?: (id: string) => boolean }
}) {
  return (
    <div>
      <div className="mx-3 mb-0.5 mt-2 flex items-center gap-2">
        <span className={shellNavSubGroupSectionLabelClass}>{label}</span>
        <div className="flex-1 border-t border-sidebar-border/50" />
      </div>
      <SidebarMenu>
        <SidebarMenuSub>
          {items.map((item, index) => (
            <ConsoleNavSlotItem
              key={item.id}
              item={item}
              activeId={activeId}
              onSelect={onSelect}
              renderItemIcon={renderItemIcon}
              signals={signals}
              leading={
                <span className="w-3 shrink-0 text-center text-dense-micro tabular-nums text-sidebar-foreground/30">
                  {index + 1}
                </span>
              }
            />
          ))}
        </SidebarMenuSub>
      </SidebarMenu>
    </div>
  )
}

export function PartnerStrip({
  collapsed,
  activeId,
  onSelect,
  allowedTabIds,
  renderItemIcon,
  dimmedIds,
  phaseFocusIds,
}: {
  collapsed: boolean
  activeId: string
  onSelect: (id: string) => void
  allowedTabIds: Set<string> | null
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  dimmedIds?: Set<string> | string[]
  phaseFocusIds?: Set<string> | string[]
}) {
  const sections = useMemo(() => buildPartnerNavSections(allowedTabIds), [allowedTabIds])
  const signals = useMemo(
    () => ({
      isDimmed: resolveIdChecker(dimmedIds),
      isPhaseFocus: resolveIdChecker(phaseFocusIds),
    }),
    [dimmedIds, phaseFocusIds],
  )

  const secondaryItems = useMemo(
    () => (sections == null ? [] : [...sections.workspace, ...sections.profile]),
    [sections],
  )
  const secondaryActive = secondaryItems.some(item => item.id === activeId)
  const [secondaryOpen, setSecondaryOpen] = useState(secondaryActive)

  useEffect(() => {
    if (secondaryActive) setSecondaryOpen(true)
  }, [secondaryActive])

  if (sections == null) return null

  const allItems = [
    ...sections.lifecycle,
    ...sections.launch,
    ...sections.workspace,
    ...sections.profile,
  ]
  const partnerActive = allItems.some(item => item.id === activeId)
  const partnerPhaseFocus = allItems.some(item => signals.isPhaseFocus?.(item.id) === true)
  const showStatusDot = partnerActive || partnerPhaseFocus

  if (collapsed) {
    return (
      <CollapsedPartnerButton
        sections={sections}
        activeId={activeId}
        onSelect={onSelect}
        renderItemIcon={renderItemIcon}
        signals={signals}
        isActive={partnerActive}
        showStatusDot={showStatusDot}
      />
    )
  }

  const hasSecondary = secondaryItems.length > 0

  return (
    <div aria-label="Engineer">
      <div className="flex items-center gap-1.5 px-3 pt-2 pb-0.5">
        <Bot className="h-3.5 w-3.5 shrink-0 text-sidebar-primary/80" aria-hidden />
        <span className="text-dense-label font-semibold text-sidebar-foreground">Engineer</span>
      </div>

      {sections.lifecycle.length > 0 && (
        <NumberedDeskSection
          label="Build Desk"
          items={sections.lifecycle}
          activeId={activeId}
          onSelect={onSelect}
          renderItemIcon={renderItemIcon}
          signals={signals}
        />
      )}

      {sections.launch.length > 0 && (
        <NumberedDeskSection
          label="Launch Desk"
          items={sections.launch}
          activeId={activeId}
          onSelect={onSelect}
          renderItemIcon={renderItemIcon}
          signals={signals}
        />
      )}

      {hasSecondary && (
        <Collapsible open={secondaryOpen} onOpenChange={setSecondaryOpen} className="group/partner-more">
          <CollapsibleTrigger className="flex w-full cursor-pointer items-center justify-between px-3 py-1 text-dense-caption text-muted-foreground select-none hover:text-sidebar-foreground">
            <span>Ops & Analysis</span>
            <ChevronDown className="h-3 w-3 transition-transform group-data-[state=open]/partner-more:rotate-180" />
          </CollapsibleTrigger>
          <CollapsibleContent>
            {sections.workspace.length > 0 && (
              <PartnerSection
                label="Ops Desk"
                items={sections.workspace}
                activeId={activeId}
                onSelect={onSelect}
                renderItemIcon={renderItemIcon}
                signals={signals}
              />
            )}
            {sections.profile.length > 0 && (
              <PartnerSection
                label="Analysis Desk"
                items={sections.profile}
                activeId={activeId}
                onSelect={onSelect}
                renderItemIcon={renderItemIcon}
                signals={signals}
              />
            )}
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  )
}

function PartnerSection({
  label,
  items,
  activeId,
  onSelect,
  renderItemIcon,
  signals,
}: {
  label: string
  items: ShellNavItem[]
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  signals: { isDimmed?: (id: string) => boolean; isPhaseFocus?: (id: string) => boolean }
}) {
  return (
    <div>
      <div className="mx-3 mb-0.5 mt-2 flex items-center gap-2">
        <span className={shellNavSubGroupSectionLabelClass}>{label}</span>
        <div className="flex-1 border-t border-sidebar-border/50" />
      </div>
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

function CollapsedPartnerButton({
  sections,
  activeId,
  onSelect,
  renderItemIcon,
  signals,
  isActive,
  showStatusDot,
}: {
  sections: NonNullable<ReturnType<typeof buildPartnerNavSections>>
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  signals: { isDimmed?: (id: string) => boolean; isPhaseFocus?: (id: string) => boolean }
  isActive: boolean
  showStatusDot: boolean
}) {
  const [open, setOpen] = useState(false)

  const handleSelect = (id: string) => {
    onSelect(id)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <button
              type="button"
              className={cn(shellNavCollapsedIconButtonClass(isActive), 'relative')}
              aria-label="Engineer"
            >
              <Bot className="h-4 w-4 shrink-0" aria-hidden />
              {showStatusDot && (
                <span
                  className="absolute top-1 right-1 h-1.5 w-1.5 rounded-full bg-sidebar-primary"
                  aria-hidden
                />
              )}
            </button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent side="right" className="text-xs font-medium">
          Engineer
        </TooltipContent>
      </Tooltip>

      <PopoverContent
        side="right"
        align="start"
        sideOffset={8}
        className="w-52 border-sidebar-border bg-sidebar p-2 shadow-xl"
      >
        <p className={shellNavFlyoutSectionTitleClass(isActive)}>Engineer</p>
        {sections.lifecycle.length > 0 && (
          <FlyoutSection
            label="Build Desk"
            items={sections.lifecycle}
            activeId={activeId}
            onSelect={handleSelect}
            renderItemIcon={renderItemIcon}
            signals={signals}
            numbered
          />
        )}
        {sections.launch.length > 0 && (
          <FlyoutSection
            label="Launch Desk"
            items={sections.launch}
            activeId={activeId}
            onSelect={handleSelect}
            renderItemIcon={renderItemIcon}
            signals={signals}
            numbered
          />
        )}
        {sections.workspace.length > 0 && (
          <FlyoutSection
            label="Ops Desk"
            items={sections.workspace}
            activeId={activeId}
            onSelect={handleSelect}
            renderItemIcon={renderItemIcon}
            signals={signals}
          />
        )}
        {sections.profile.length > 0 && (
          <FlyoutSection
            label="Analysis Desk"
            items={sections.profile}
            activeId={activeId}
            onSelect={handleSelect}
            renderItemIcon={renderItemIcon}
            signals={signals}
          />
        )}
      </PopoverContent>
    </Popover>
  )
}

function FlyoutSection({
  label,
  items,
  activeId,
  onSelect,
  renderItemIcon,
  signals,
  numbered,
}: {
  label: string
  items: ShellNavItem[]
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  signals: { isDimmed?: (id: string) => boolean; isPhaseFocus?: (id: string) => boolean }
  numbered?: boolean
}) {
  return (
    <div>
      <p className={cn(shellNavSubGroupSectionLabelClass, 'px-1 pt-1.5 pb-0.5')}>{label}</p>
      {items.map((item, index) => (
        <ConsoleNavSlotItem
          key={item.id}
          item={item}
          activeId={activeId}
          onSelect={onSelect}
          renderItemIcon={renderItemIcon}
          signals={signals}
          flyout
          leading={
            numbered === true ? (
              <span className="w-3 shrink-0 text-center text-dense-micro tabular-nums text-sidebar-foreground/30">
                {index + 1}
              </span>
            ) : undefined
          }
        />
      ))}
    </div>
  )
}
