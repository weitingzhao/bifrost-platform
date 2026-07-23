import type { ReactNode } from 'react'
import { Button, DenseTag, SidebarTrigger, SHELL_TOP_BAR_HEIGHT_CLASS, StatusLamp, cn } from '@bifrost/ui'
import { Bot, ChevronRight } from 'lucide-react'
import type { ConsoleNavPlane } from '@/lib/consoleNavConfig'
import {
  viewerEnvBadgeLabel,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot'
import { UserMenu } from '@/components/UserMenu'

export type ConsoleHeaderAmbientAgent = {
  label: string
  onOpen: () => void
  /** Dock already expanded — button focuses / still expands. */
  expanded?: boolean
  /** True when an ambient Fix job is live. */
  running?: boolean
}

function ViewerEnvChip({
  viewerEnv,
  isLoading,
}: {
  viewerEnv: FleetViewerEnv
  isLoading?: boolean
}) {
  if (isLoading) {
    return (
      <span title="Viewer environment: probing">
        <DenseTag variant="neutral">…</DenseTag>
      </span>
    )
  }
  const label = viewerEnvBadgeLabel(viewerEnv)
  const variant =
    viewerEnv === 'prod' ? 'danger' : viewerEnv === 'stg' ? 'warning' : 'info'
  return (
    <span title={`Viewer environment: ${viewerEnv}`}>
      <DenseTag variant={variant}>{label}</DenseTag>
    </span>
  )
}

export function ConsoleHeader({
  plane,
  pageTitle,
  pageDescription,
  healthy,
  onRefresh,
  ambientAgent,
  viewerEnv,
  viewerEnvLoading,
  onSelectTab,
  children,
}: {
  /** Sidebar plane — system domain (breadcrumb parent). */
  plane?: ConsoleNavPlane
  /** Current page name (breadcrumb leaf; doubles as document h1 when PageHeader omitted). */
  pageTitle?: string
  /** Optional subtitle — shown as title tooltip on the page crumb. */
  pageDescription?: string
  healthy: boolean | undefined
  onRefresh: () => void
  /** Global Agent Task → Execution Dock (always available). */
  ambientAgent?: ConsoleHeaderAmbientAgent | null
  /** Fleet Viewer seat — chrome chip (slot 1). */
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
  /** User menu Governance / shell navigation. */
  onSelectTab: (tabId: string) => void
  /** Optional extra right-side slot (rarely used). */
  children?: ReactNode
}) {
  const showBreadcrumb = plane != null || (pageTitle != null && pageTitle !== '')

  return (
    <header
      className={cn(
        SHELL_TOP_BAR_HEIGHT_CLASS,
        'flex items-center gap-2 border-b border-border bg-card px-3',
      )}
    >
      <SidebarTrigger />

      {showBreadcrumb && (
        <nav
          aria-label="Breadcrumb"
          className="flex min-w-0 max-w-[min(28rem,45vw)] items-center gap-1"
        >
          {plane != null && (
            <span className="hidden shrink-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-muted-foreground sm:inline">
              {plane}
            </span>
          )}
          {plane != null && pageTitle != null && pageTitle !== '' && (
            <ChevronRight
              size={12}
              className="hidden shrink-0 text-muted-foreground/70 sm:inline"
              aria-hidden
            />
          )}
          {pageTitle != null && pageTitle !== '' && (
            <h1
              className="m-0 truncate text-[var(--text-dense-label)] font-semibold tracking-tight text-foreground"
              title={pageDescription}
            >
              {pageTitle}
            </h1>
          )}
        </nav>
      )}

      <div className="min-w-0 flex-1" />

      {/* Slot 1 — Viewer Env (header SSOT; sidebar logo no longer repeats env) */}
      <ViewerEnvChip viewerEnv={viewerEnv} isLoading={viewerEnvLoading} />

      {/* Slot 2 — Agent Task (compact on narrow widths so User stays visible) */}
      {ambientAgent != null && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className={cn(
            'h-7 shrink-0 gap-1.5 px-2 text-[var(--text-dense-caption)] shadow-sm',
            ambientAgent.running
              ? 'border-[color-mix(in_oklab,var(--task-mode-accent,#f59e0b)_55%,var(--border))] bg-[color-mix(in_oklab,var(--task-mode-accent,#f59e0b)_14%,var(--card))]'
              : 'border-border bg-secondary/50',
          )}
          onClick={ambientAgent.onOpen}
          title="Open Agent Execution Dock — live status (shell bottom)"
        >
          <StatusLamp value={ambientAgent.running ? 'degraded' : 'unknown'} kind="reach" />
          <Bot size={12} aria-hidden />
          <span className="font-semibold text-foreground">
            <span className="sm:hidden">Agent</span>
            <span className="hidden sm:inline">Agent Task</span>
          </span>
          {ambientAgent.running && (
            <span className="hidden max-w-[6rem] truncate text-muted-foreground xl:inline">
              {ambientAgent.label}
            </span>
          )}
          <span className="hidden font-medium text-primary md:inline">
            {ambientAgent.expanded ? 'Focus' : 'Dock'}
          </span>
        </Button>
      )}

      {children != null ? <div className="shrink-0">{children}</div> : null}

      {/* Slot 3 — User (Session · Guides · Shell) */}
      <UserMenu
        onSelectTab={onSelectTab}
        opsApiHealthy={healthy}
        onRefresh={onRefresh}
      />
    </header>
  )
}

/** Second chrome row — spine / matrix context (below title bar). Compact single-line by default. */
export function OpsContextBar({ children }: { children: ReactNode }) {
  return (
    <div className="border-b border-border bg-secondary/40 px-3 py-1.5">{children}</div>
  )
}
