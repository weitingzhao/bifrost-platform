import type { ReactNode } from 'react'
import {
  Button,
  DenseTag,
  SidebarTrigger,
  SHELL_TOP_BAR_HEIGHT_CLASS,
  StatusLamp,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn,
} from '@bifrost/ui'
import { Bot, ChevronRight, CircleHelp } from 'lucide-react'
import type { ConsoleNavPlane } from '@/lib/consoleNavConfig'
import {
  viewerEnvBadgeLabel,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot'
import { UserMenu } from '@/components/UserMenu'
import { ActivityIndicator } from '@/components/activity/ActivityIndicator'
import { TaskModeCapsule } from '@/components/task-mode/TaskModeCapsule'
import type { TaskModeId } from '@/lib/task-mode/types'

export type ConsoleHeaderAmbientAgent = {
  label: string
  onOpen: () => void
  expanded?: boolean
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

/**
 * Shell top bar — breadcrumb is the system-wide page identity (plane › page).
 * Do not render a second PageHeader / ConsolePageHeader title on pages.
 */
export function ConsoleHeader({
  plane,
  pageTitle,
  pageDescription,
  pageActions,
  healthy,
  onRefresh,
  ambientAgent,
  viewerEnv,
  viewerEnvLoading,
  onModeChange,
  onSelectTab,
  children,
}: {
  plane?: ConsoleNavPlane
  pageTitle?: string
  /** Shown via ? help on the page crumb (replaces in-page subtitle). */
  pageDescription?: string
  /** Page-level actions that used to live in PageHeader (Copy All, Back, …). */
  pageActions?: ReactNode
  healthy: boolean | undefined
  onRefresh: () => void
  ambientAgent?: ConsoleHeaderAmbientAgent | null
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
  /** Task-mode identity capsule (replaces full-width TaskModeActiveBanner). */
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
  onSelectTab: (tabId: string) => void
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
          className="flex min-w-0 max-w-[min(32rem,50vw)] items-center gap-1"
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
            <span className="inline-flex min-w-0 items-center gap-1">
              <h1 className="m-0 truncate text-[var(--text-dense-label)] font-semibold tracking-tight text-foreground">
                {pageTitle}
              </h1>
              {pageDescription != null && pageDescription !== '' && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      className="inline-flex size-5 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                      aria-label={`About ${pageTitle}`}
                    >
                      <CircleHelp className="size-3.5" aria-hidden />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom" className="max-w-sm text-left">
                    {pageDescription}
                  </TooltipContent>
                </Tooltip>
              )}
            </span>
          )}
        </nav>
      )}

      {pageActions != null ? (
        <div className="flex shrink-0 items-center gap-1.5">{pageActions}</div>
      ) : null}

      <div className="min-w-0 flex-1" />

      <ViewerEnvChip viewerEnv={viewerEnv} isLoading={viewerEnvLoading} />

      <TaskModeCapsule onModeChange={onModeChange} />

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

      {/* Activity — shell chrome between Agent Task and User; idle = invisible */}
      <ActivityIndicator
        onOpenAudit={() => onSelectTab('audit')}
        onNavigate={onSelectTab}
      />

      {children != null ? <div className="shrink-0">{children}</div> : null}

      <UserMenu
        onSelectTab={onSelectTab}
        opsApiHealthy={healthy}
        onRefresh={onRefresh}
      />
    </header>
  )
}
