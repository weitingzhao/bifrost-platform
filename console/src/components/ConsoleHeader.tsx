import {
  DenseTag,
  SidebarTrigger,
  SHELL_TOP_BAR_HEIGHT_CLASS,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  cn,
} from '@bifrost/ui'
import { ChevronLeft, CircleHelp, Ellipsis } from 'lucide-react'
import {
  viewerEnvBadgeLabel,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot'
import { UserMenu } from '@/components/UserMenu'
import { AppearanceMenu } from '@/components/shell/AppearanceMenu'
import { ToolsMenu } from '@/components/ToolsMenu'
import { DevSessionsIndicator } from '@/components/DevSessionsIndicator'
import type { ShellLayout } from '@/lib/shell/useShellLayout'
import { VERDICT_DOT_COLOR, type VerdictTone } from '@/lib/shell/shellStatusLine'

/** The environment is identity, not severity: drawn neutral, never red (design system v2 §10.6). */
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
    <span title={`Viewer seat: ${viewerEnv}`} data-viewer-env={viewerEnv}>
      <DenseTag variant="neutral">{label}</DenseTag>
    </span>
  )
}

/** Where a detail page's back link goes, on a phone or a tablet. */
export type HeaderBack = { label: string; href: string }

/**
 * Shell top bar. The only live reading is the system verdict, the same sentence Status opens with.
 * On a phone it is left out (it had about 60px and showed nothing whole); the page title gets the
 * room, and the entries and Status open with a verdict of their own.
 * The side nav's toggle shows only on a desktop; a phone or a tablet gets the way back from a
 * detail page and More (the list of detail pages) instead. With the tab bar the title needs the
 * room: the page's question and the external tools move to More.
 * Dev Sessions is mounted by the parent only on the local console.
 */
export function ConsoleHeader({
  pageTitle,
  pageDescription,
  statusLine,
  statusTone,
  healthy,
  onRefresh,
  viewerEnv,
  viewerEnvLoading,
  showDevSessions,
  onOpenDevSessions,
  layout = 'side',
  back,
  moreHref,
}: {
  pageTitle: string
  pageDescription?: string
  statusLine: string
  statusTone: VerdictTone
  healthy: boolean | undefined
  onRefresh: () => void
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
  showDevSessions: boolean
  onOpenDevSessions: () => void
  layout?: ShellLayout
  back?: HeaderBack
  moreHref?: string
}) {
  const side = layout === 'side'
  return (
    <header
      className={cn(
        SHELL_TOP_BAR_HEIGHT_CLASS,
        'ops-console-header flex items-center gap-2 border-b border-[var(--table-rule)] bg-card px-3',
      )}
    >
      {side ? <SidebarTrigger /> : null}

      {!side && back != null ? (
        <a
          href={back.href}
          aria-label={`Back to ${back.label}`}
          className="inline-flex min-h-11 shrink-0 items-center gap-0.5 pr-1 text-sm font-medium text-muted-foreground no-underline hover:text-foreground"
        >
          <ChevronLeft className="size-5" aria-hidden />
          {back.label}
        </a>
      ) : null}

      <nav aria-label="Breadcrumb" className="flex min-w-0 flex-1 items-center gap-1 sm:max-w-[min(16rem,28vw)] sm:flex-none">
        <span className="inline-flex min-w-0 items-center gap-1">
          <h1 className="m-0 truncate text-[var(--text-dense-label)] font-semibold tracking-tight text-foreground">
            {pageTitle}
          </h1>
          {layout !== 'tabs' && pageDescription != null && pageDescription !== '' && (
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
        className="hidden min-w-0 flex-1 items-center gap-1.5 text-[var(--text-dense-caption)] text-muted-foreground sm:flex"
        title={statusLine}
        aria-live="polite"
        data-shell-status-line
        data-tone={statusTone}
      >
        <span
          className="size-2 shrink-0 rounded-full"
          style={{ backgroundColor: VERDICT_DOT_COLOR[statusTone] }}
          aria-hidden
        />
        <a href="#status" className="min-w-0 truncate text-inherit no-underline hover:text-foreground">
          {statusLine}
        </a>
      </p>

      <ViewerEnvChip viewerEnv={viewerEnv} isLoading={viewerEnvLoading} />

      <AppearanceMenu />

      {showDevSessions ? <span className="ops-local-session"><DevSessionsIndicator onOpen={onOpenDevSessions} /></span> : null}

      {!side && moreHref != null ? (
        <a
          href={moreHref}
          aria-label="More pages"
          className="inline-flex size-11 shrink-0 items-center justify-center rounded-xl border border-[var(--table-rule)] text-foreground no-underline"
        >
          <Ellipsis className="size-5" aria-hidden />
        </a>
      ) : null}

      {layout !== 'tabs' ? <ToolsMenu /> : null}

      <UserMenu opsApiHealthy={healthy} onRefresh={onRefresh} />
    </header>
  )
}
