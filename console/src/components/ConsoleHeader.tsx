import {
  DenseTag,
  SidebarTrigger,
  SHELL_TOP_BAR_HEIGHT_CLASS,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn,
} from '@bifrost/ui'
import { CircleHelp } from 'lucide-react'
import {
  viewerEnvBadgeLabel,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot'
import { viewerSeatTagVariant } from '@/lib/envVisual'
import { UserMenu } from '@/components/UserMenu'
import { ToolsMenu } from '@/components/ToolsMenu'
import { DevSessionsIndicator } from '@/components/DevSessionsIndicator'

function ViewerEnvChip({
  viewerEnv,
  isLoading,
}: {
  viewerEnv: FleetViewerEnv
  isLoading?: boolean
}) {
  if (isLoading) {
    return (
      <span title="Viewer seat: probing">
        <DenseTag variant="neutral">…</DenseTag>
      </span>
    )
  }
  const label = viewerEnvBadgeLabel(viewerEnv)
  return (
    <span title={`Viewer seat: ${viewerEnv}`}>
      <DenseTag variant={viewerSeatTagVariant(viewerEnv)}>{label}</DenseTag>
    </span>
  )
}

/**
 * Shell top bar. The only live reading is the Status sentence.
 * Dev Sessions is mounted by the parent only on the local console.
 */
export function ConsoleHeader({
  pageTitle,
  pageDescription,
  statusLine,
  healthy,
  onRefresh,
  viewerEnv,
  viewerEnvLoading,
  showDevSessions,
  onOpenDevSessions,
}: {
  pageTitle: string
  pageDescription?: string
  statusLine: string
  healthy: boolean | undefined
  onRefresh: () => void
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
  showDevSessions: boolean
  onOpenDevSessions: () => void
}) {
  return (
    <header
      className={cn(
        SHELL_TOP_BAR_HEIGHT_CLASS,
        'flex items-center gap-2 border-b border-[var(--table-rule)] bg-card px-3',
      )}
    >
      <SidebarTrigger />

      <nav aria-label="Breadcrumb" className="flex min-w-0 max-w-[min(16rem,28vw)] items-center gap-1">
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
      </nav>

      <p
        className="min-w-0 flex-1 truncate text-[var(--text-dense-caption)] text-muted-foreground"
        title={statusLine}
        aria-live="polite"
        data-shell-status-line
      >
        {statusLine}
      </p>

      <ViewerEnvChip viewerEnv={viewerEnv} isLoading={viewerEnvLoading} />

      {showDevSessions ? <DevSessionsIndicator onOpen={onOpenDevSessions} /> : null}

      <ToolsMenu />

      <UserMenu opsApiHealthy={healthy} onRefresh={onRefresh} />
    </header>
  )
}
