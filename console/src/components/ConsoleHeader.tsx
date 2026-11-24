import { communicationHref } from '@/domains/coordination/communication/looks'
import { useFrameWaiting } from '@/lib/shell/frameWaiting'
import { useNewVersionNotice } from '@/lib/shell/newVersion'
import { SidebarTrigger, Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { WorldFrame } from '@bifrost/ui/city/WorldFrame'
import { SignalsPinNote } from '@/components/shell/world-journey/SignalsPinNote'
import { useJourneyBack } from '@/components/shell/world-journey/journeyTrail'
import type { WorldTime } from '@bifrost/ui/city/world'
import { CircleHelp, Ellipsis } from 'lucide-react'
import {
  viewerEnvBadgeLabel,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot'
import { UserMenu } from '@/components/UserMenu'
import { AppearanceMenu } from '@/components/shell/AppearanceMenu'
import { ToolsMenu } from '@/components/ToolsMenu'
import type { ShellLayout } from '@/lib/shell/useShellLayout'
import { VERDICT_DOT_COLOR, type VerdictTone } from '@/lib/shell/shellStatusLine'
import { consoleCrumbs, consoleFramePlaces, type ConsoleLooks, type FrameAlerts, type FrameCrumb } from '@/lib/shell/consoleFrame'

/**
 * The looks of the place on screen (the Owner, 2026-11-19): City | Console, peers, at the same spot as
 * the city's own switch; inside the Console, Page | Ledger. Each half is a link to the same place.
 */
export type HeaderLooks = ConsoleLooks

/** The entry a page sits under: the crumb before the page (on a phone, the way back). */
export type HeaderBack = { label: string; href: string }

/**
 * Shell top: the one world frame every view wears (箭10 2026-11-23, cockpit/ui-world/frame), Console being
 * here. Row 1 is the engine's: brand, Console › entry › page, the four places, the environment and the
 * alerts corner (counted with alertCount, like the City's). Row 2 is the Console's own: Page | Table, the
 * side nav's toggle on a desktop, the system verdict (the sentence Status opens with; left out on a phone),
 * the page's question, More, appearance, tools and the user menu.
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
  layout = 'side',
  back,
  moreHref,
  looks,
  alerts,
  time,
  crumbs,
}: {
  pageTitle: string
  pageDescription?: string
  statusLine: string
  statusTone: VerdictTone
  healthy: boolean | undefined
  onRefresh: () => void
  viewerEnv: FleetViewerEnv
  viewerEnvLoading?: boolean
  layout?: ShellLayout
  /** The entry this page sits under (crumb and phone back); none on an entry itself. */
  back?: HeaderBack
  moreHref?: string
  looks?: HeaderLooks
  alerts?: FrameAlerts
  /** The time chip: live and when the shown reading was made (frameTimeOf). */
  time?: WorldTime
  /** Crumbs a place gives itself (the City's table: City › street › … › Table); otherwise Console › entry › page. */
  crumbs?: FrameCrumb[]
}) {
  const side = layout === 'side'
  // The frame's Return: the journey's previous stop (gap table item 8), as the City's frame has it.
  const journeyBack = useJourneyBack()
  const places = consoleFramePlaces(
    looks,
    communicationHref('agents', new URLSearchParams(window.location.hash.split('?')[1] ?? '').get('exchange')),
    window.location.hash,
  )
  const notice = useNewVersionNotice()
  const waiting = useFrameWaiting()
  return (
    <div className="ops-console-header ops-console-frame">
      <WorldFrame
        here="console"
        notice={notice}
        waiting={waiting}
        crumbs={crumbs ?? consoleCrumbs(back ?? null, pageTitle)}
        hrefs={places.hrefs}
        env={viewerEnvLoading ? undefined : viewerEnvBadgeLabel(viewerEnv)}
        alerts={alerts}
        time={time}
        back={journeyBack}
        looks={places.looks}
        context={
          <>
            {side ? <SidebarTrigger /> : null}
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
            <p
              className="m-0 hidden min-w-0 flex-1 items-center gap-1.5 sm:flex"
              title={statusLine}
              aria-live="polite"
              data-shell-status-line
              data-tone={statusTone}
            >
              <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: VERDICT_DOT_COLOR[statusTone] }} aria-hidden />
              <a href="#status" className="min-w-0 truncate text-inherit no-underline hover:text-foreground">
                {statusLine}
              </a>
            </p>
            {/* One implementation for every frame (World's, 2026-11-23). */}
            <SignalsPinNote className="shrink-0 text-[11px] text-muted-foreground no-underline hover:text-foreground" />
            <span className="ml-auto inline-flex shrink-0 items-center gap-1" data-console-tools>
              {!side && moreHref != null ? (
                <a href={moreHref} aria-label="More pages" className="inline-flex size-9 items-center justify-center rounded-md text-foreground no-underline">
                  <Ellipsis className="size-4" aria-hidden />
                </a>
              ) : null}
              <AppearanceMenu />
              <ToolsMenu />
              <UserMenu opsApiHealthy={healthy} onRefresh={onRefresh} />
            </span>
          </>
        }
      />
    </div>
  )
}

