import { ExternalLink } from 'lucide-react'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarSeparator,
  useSidebar,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn,
} from '@bifrost/ui'
import type { IconComponent } from '@bifrost/ui'
import {
  Activity,
  BookOpen,
  Boxes,
  FileText,
  Layers,
  Map,
  Milestone,
  Ruler,
  Server,
  Terminal,
} from 'lucide-react'

// ── Types ───────────────────────────────────────────────────────────────

export type ConsoleViewTab =
  | 'briefing'
  | 'control-room'
  | 'pulse'
  | 'runtime-map'
  | 'cluster'
  | 'delivery'
  | 'program'
  | 'promote'
  | 'blueprint'
  | 'environments'
  | 'platform-standards'
  | 'agent-protocol'
  | 'design-system'
  | 'console'

type NavItem = {
  id: ConsoleViewTab | string
  label: string
  icon?: IconComponent
  external?: boolean
  href?: string
}

type NavGroupDef = {
  label: string
  icon: IconComponent
  items: NavItem[]
  dividerBefore?: boolean
}

// ── Nav config ──────────────────────────────────────────────────────────

const NAV_GROUPS: NavGroupDef[] = [
  {
    label: 'Ops',
    icon: Activity,
    items: [
      { id: 'briefing', label: 'Agent Briefing' },
      { id: 'control-room', label: 'Control Room' },
      { id: 'pulse', label: 'Pulse' },
    ],
  },
  {
    label: 'Runtime',
    icon: Server,
    items: [
      { id: 'runtime-map', label: 'Runtime Map' },
      { id: 'cluster', label: 'Cluster' },
    ],
  },
  {
    label: 'Program',
    icon: Milestone,
    items: [
      { id: 'delivery', label: 'Delivery' },
      { id: 'program', label: 'Milestones' },
      { id: 'promote', label: 'Promote' },
    ],
  },
  {
    label: 'Architecture',
    icon: Boxes,
    items: [
      { id: 'blueprint', label: 'Blueprint' },
      { id: 'environments', label: 'Environments' },
    ],
  },
  {
    label: 'Standards',
    icon: Ruler,
    items: [
      { id: 'platform-standards', label: 'Platform' },
      { id: 'agent-protocol', label: 'Agent Protocol' },
      { id: 'design-system', label: 'Design System' },
    ],
  },
  {
    label: 'Tools',
    icon: Terminal,
    dividerBefore: true,
    items: [{ id: 'console', label: 'Server Console' }],
  },
]

// ── Peer-app link (Bifrost Trade) ───────────────────────────────────────

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

function PeerAppLink({ collapsed }: { collapsed: boolean }) {
  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <a
            href={TRADE_APP_URL}
            target="_blank"
            rel="noopener noreferrer"
            className={cn(
              'flex h-8 w-8 items-center justify-center rounded-md transition-colors mx-auto',
              'text-sidebar-foreground/50 hover:bg-sidebar-accent hover:text-sidebar-foreground',
            )}
          >
            <Layers className="h-4 w-4 shrink-0" />
          </a>
        </TooltipTrigger>
        <TooltipContent side="right" className="text-xs font-medium">
          Open Bifrost Trade
        </TooltipContent>
      </Tooltip>
    )
  }

  return (
    <a
      href={TRADE_APP_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="mx-2 mb-1 block rounded-md border border-sidebar-border bg-sidebar-accent/40 px-2.5 py-2 text-xs transition-colors hover:border-sidebar-primary/40 hover:bg-sidebar-accent"
    >
      <span className="flex items-center gap-1.5 font-semibold text-sidebar-primary">
        Bifrost Trade Monitoring
        <ExternalLink className="h-3 w-3 shrink-0 opacity-70" />
      </span>
      <span className="mt-0.5 block text-[10px] leading-snug text-sidebar-foreground/55">
        Business console · positions, daemon, market
      </span>
    </a>
  )
}

// ── Docs external links ─────────────────────────────────────────────────

type DocLink = { id: string; label: string; href: string }

function DocsGroup({
  links,
  collapsed,
}: {
  links: DocLink[]
  collapsed: boolean
}) {
  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <a
            href={links[0]?.href}
            target="_blank"
            rel="noopener noreferrer"
            className={cn(
              'flex h-8 w-8 items-center justify-center rounded-md transition-colors mx-auto',
              'text-sidebar-foreground/50 hover:bg-sidebar-accent hover:text-sidebar-foreground',
            )}
          >
            <FileText className="h-4 w-4 shrink-0" />
          </a>
        </TooltipTrigger>
        <TooltipContent side="right" className="text-xs font-medium">
          Docs
        </TooltipContent>
      </Tooltip>
    )
  }

  return (
    <SidebarGroup>
      <SidebarGroupLabel className="text-xs font-semibold tracking-tight text-sidebar-foreground/70">
        <BookOpen className="mr-2 h-4 w-4 shrink-0 text-sidebar-foreground/50" />
        Docs
      </SidebarGroupLabel>
      <SidebarMenu>
        {links.map((link) => (
          <SidebarMenuItem key={link.id}>
            <SidebarMenuButton asChild>
              <a href={link.href} target="_blank" rel="noopener noreferrer">
                <ExternalLink className="h-3.5 w-3.5 opacity-50" />
                <span>{link.label}</span>
              </a>
            </SidebarMenuButton>
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    </SidebarGroup>
  )
}

// ── Collapsed icon ──────────────────────────────────────────────────────

function CollapsedGroupIcon({
  group,
  activeTab,
  onSelect,
}: {
  group: NavGroupDef
  activeTab: string
  onSelect: (id: string) => void
}) {
  const isActive = group.items.some((i) => i.id === activeTab)

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          onClick={() => onSelect(group.items[0].id)}
          className={cn(
            'flex h-8 w-8 items-center justify-center rounded-md transition-colors mx-auto',
            isActive
              ? 'bg-sidebar-accent text-sidebar-primary'
              : 'text-sidebar-foreground/50 hover:bg-sidebar-accent hover:text-sidebar-foreground',
          )}
        >
          <group.icon className="h-4 w-4 shrink-0" />
        </button>
      </TooltipTrigger>
      <TooltipContent side="right" className="text-xs font-medium">
        {group.label}
      </TooltipContent>
    </Tooltip>
  )
}

// ── Main sidebar component ──────────────────────────────────────────────

export function ConsoleSidebar({
  activeTab,
  onSelect,
  docLinks,
}: {
  activeTab: string
  onSelect: (id: string) => void
  docLinks: DocLink[]
}) {
  const { state } = useSidebar()
  const isCollapsed = state === 'collapsed'

  return (
    <Sidebar collapsible="icon">
      {/* Header */}
      <SidebarHeader className="flex h-12 flex-row items-center gap-0 border-b border-sidebar-border p-0 px-3">
        {isCollapsed ? (
          <div className="flex w-full items-center justify-center">
            <span className="text-lg font-bold text-sidebar-primary">B</span>
          </div>
        ) : (
          <div className="flex w-full items-center gap-2">
            <span className="text-sm font-bold text-sidebar-foreground">Bifrost Ops</span>
            <span className="rounded bg-sidebar-primary/15 px-1.5 py-0.5 text-[10px] font-semibold text-sidebar-primary">
              Ops
            </span>
          </div>
        )}
      </SidebarHeader>

      <SidebarContent>
        {isCollapsed ? (
          <div className="flex flex-col gap-1 py-2 px-1">
            {NAV_GROUPS.map((group) => (
              <div key={group.label}>
                {group.dividerBefore && (
                  <div className="my-1.5 border-t border-sidebar-border/60" />
                )}
                <CollapsedGroupIcon
                  group={group}
                  activeTab={activeTab}
                  onSelect={onSelect}
                />
              </div>
            ))}
          </div>
        ) : (
          NAV_GROUPS.map((group) => {
            const isGroupActive = group.items.some((i) => i.id === activeTab)

            return (
              <div key={group.label}>
                {group.dividerBefore && <SidebarSeparator />}
                <SidebarGroup>
                  <SidebarGroupLabel
                    className={cn(
                      'text-xs font-semibold tracking-tight',
                      isGroupActive
                        ? 'text-sidebar-foreground'
                        : 'text-sidebar-foreground/70',
                    )}
                  >
                    <group.icon
                      className={cn(
                        'mr-2 h-4 w-4 shrink-0',
                        isGroupActive
                          ? 'text-sidebar-primary'
                          : 'text-sidebar-foreground/50',
                      )}
                    />
                    {group.label}
                  </SidebarGroupLabel>
                  <SidebarMenu>
                    {group.items.map((item) => (
                      <SidebarMenuItem key={item.id}>
                        <SidebarMenuButton
                          isActive={activeTab === item.id}
                          onClick={() => onSelect(item.id)}
                          size="sm"
                        >
                          <span>{item.label}</span>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    ))}
                  </SidebarMenu>
                </SidebarGroup>
              </div>
            )
          })
        )}
        {/* Docs group */}
        <DocsGroup links={docLinks} collapsed={isCollapsed} />
      </SidebarContent>

      <SidebarFooter className="border-t border-sidebar-border">
        <PeerAppLink collapsed={isCollapsed} />
      </SidebarFooter>
    </Sidebar>
  )
}
