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
  shellNavSecondaryCollapseTriggerClass,
  shellNavSubGroupSectionLabelClass,
  type ShellNavItem,
  type ShellNavSubGroup,
} from '@bifrost/ui'
import { Bot, ChevronDown } from 'lucide-react'
import { buildPartnerNavSections } from '@/lib/consoleNavConfig'
import { ConsoleNavSlotItem } from '@/components/shell/ConsoleNavSlotItem'
import { useBuildDeskWorkloadCounts } from '@/hooks/useBuildDeskWorkloadCounts'
import type { BuildDeskWorkloadCounts } from '@/lib/briefing/buildDeskWorkload'

function workloadBadgeForItem(
  itemId: string,
  counts: BuildDeskWorkloadCounts,
): ReactNode {
  const n =
    itemId === 'briefing'
      ? counts.briefing
      : itemId === 'active-session'
        ? counts.activeSession
        : 0
  if (n <= 0) return null
  return (
    <span
      className="shrink-0 text-dense-micro tabular-nums text-sidebar-foreground/55"
      aria-label={`${n}`}
    >
      {n}
    </span>
  )
}

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
  workloadCounts,
}: {
  label: string
  items: ShellNavItem[]
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  signals: { isDimmed?: (id: string) => boolean; isPhaseFocus?: (id: string) => boolean }
  workloadCounts?: BuildDeskWorkloadCounts
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
              trailing={
                workloadCounts != null ? workloadBadgeForItem(item.id, workloadCounts) : null
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
  const workloadCounts = useBuildDeskWorkloadCounts()
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
        workloadCounts={workloadCounts}
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
          workloadCounts={workloadCounts}
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
          <CollapsibleTrigger className={shellNavSecondaryCollapseTriggerClass}>
            <span>Ops & Analysis</span>
            <ChevronDown className="h-3 w-3 transition-transform group-data-[state=open]/partner-more:rotate-180" />
          </CollapsibleTrigger>
          <CollapsibleContent>
            {sections.workspaceGroups.length > 0 && (
              <PartnerSection
                label="Ops Desk"
                groups={sections.workspaceGroups}
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
  groups,
  activeId,
  onSelect,
  renderItemIcon,
  signals,
}: {
  label: string
  items?: ShellNavItem[]
  groups?: ShellNavSubGroup[]
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  signals: { isDimmed?: (id: string) => boolean; isPhaseFocus?: (id: string) => boolean }
}) {
  const blocks: ShellNavSubGroup[] =
    groups != null && groups.length > 0
      ? groups
      : items != null && items.length > 0
        ? [{ label: '', items }]
        : []

  return (
    <div>
      <div className="mx-3 mb-0.5 mt-2 flex items-center gap-2">
        <span className={shellNavSubGroupSectionLabelClass}>{label}</span>
        <div className="flex-1 border-t border-sidebar-border/50" />
      </div>
      <SidebarMenu>
        {blocks.map((block, index) => (
          <div key={block.label !== '' ? block.label : `ops-trail-${index}`}>
            {index > 0 && <div className="mx-3 my-1 border-t border-sidebar-border/40" />}
            {block.label !== '' ? (
              <p className={cn(shellNavSubGroupSectionLabelClass, 'mx-3 mt-1 mb-0.5 px-0')}>
                {block.label}
              </p>
            ) : null}
            <SidebarMenuSub>
              {block.items.map(item => (
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
          </div>
        ))}
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
  workloadCounts,
}: {
  sections: NonNullable<ReturnType<typeof buildPartnerNavSections>>
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  signals: { isDimmed?: (id: string) => boolean; isPhaseFocus?: (id: string) => boolean }
  isActive: boolean
  showStatusDot: boolean
  workloadCounts: BuildDeskWorkloadCounts
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
            workloadCounts={workloadCounts}
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
        {sections.workspaceGroups.length > 0 && (
          <FlyoutSection
            label="Ops Desk"
            groups={sections.workspaceGroups}
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
  groups,
  activeId,
  onSelect,
  renderItemIcon,
  signals,
  numbered,
  workloadCounts,
}: {
  label: string
  items?: ShellNavItem[]
  groups?: ShellNavSubGroup[]
  activeId: string
  onSelect: (id: string) => void
  renderItemIcon?: (item: ShellNavItem) => ReactNode
  signals: { isDimmed?: (id: string) => boolean; isPhaseFocus?: (id: string) => boolean }
  numbered?: boolean
  workloadCounts?: BuildDeskWorkloadCounts
}) {
  const blocks: ShellNavSubGroup[] =
    groups != null && groups.length > 0
      ? groups
      : items != null && items.length > 0
        ? [{ label: '', items }]
        : []

  return (
    <div>
      <p className={cn(shellNavSubGroupSectionLabelClass, 'px-1 pt-1.5 pb-0.5')}>{label}</p>
      {blocks.map((block, blockIndex) => (
        <div key={block.label !== '' ? block.label : `flyout-trail-${blockIndex}`}>
          {block.label !== '' ? (
            <p className={cn(shellNavSubGroupSectionLabelClass, 'px-1 pt-1 pb-0.5 opacity-80')}>
              {block.label}
            </p>
          ) : blockIndex > 0 ? (
            <div className="my-1 border-t border-sidebar-border/40" />
          ) : null}
          {block.items.map((item, index) => (
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
              trailing={
                workloadCounts != null ? workloadBadgeForItem(item.id, workloadCounts) : null
              }
            />
          ))}
        </div>
      ))}
    </div>
  )
}
