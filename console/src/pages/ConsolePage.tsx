import { PageShell, SidebarInset, SidebarProvider, TooltipProvider, cn } from '@bifrost/ui'
import { useCallback, useEffect, useState, type ComponentType } from 'react'
import { ConsoleHeader, type HeaderBack } from '@/components/ConsoleHeader'
import { ConsoleSidebar } from '@/components/ConsoleSidebar'
import { EntryRail, EntryTabBar, type EntryNavProps } from '@/components/shell/EntryNav'
import { LocalDevSessionsDock } from '@/components/shell/LocalDevSessionsDock'
import { MorePage } from '@/components/shell/MorePage'
import { useShellStatusLine } from '@/hooks/useShellStatusLine'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { QuestionPage } from '@/pages/QuestionPage'
import { DevSessionsPage } from '@/pages/DevSessionsPage'
import {
  DataPage,
  IbPage,
  InfrastructurePage,
  OwnerConsolePage,
  RecordsPage,
  ReleasesPage,
  StatusPage,
} from '@/pages/shell/shellPages'
import { yoursNavBadge } from '@/pages/shell/owner/ownerModel'
import { useOwnerYours } from '@/pages/shell/owner/useOwnerYours'
import { syncAppBadge } from '@/pwa/appBadge'
import { ConsoleBuildBanner } from '@/pwa/ConsoleBuildBanner'
import { useConsoleBuildDrift } from '@/pwa/useConsoleBuildDrift'
import {
  HOME_ROUTE,
  MORE_ROUTE,
  NEEDS_YOU_HREF,
  NEEDS_YOU_ROUTE,
  entryOf,
  formatShellHash,
  isEntryRoute,
  isShellRouteId,
  locationId,
  resolveConsoleHash,
  resolveHashTab,
  shellNavEntry,
  type ConsoleLocation,
  type EntryRouteId,
  type ShellRouteId,
} from '@/lib/shell/consoleRoutes'
import { showDevSessions } from '@/lib/shell/localConsole'
import { useShellLayout } from '@/lib/shell/useShellLayout'

type DetailRouteId = Exclude<ShellRouteId, EntryRouteId>

const DETAIL_PAGES: Record<DetailRouteId, ComponentType> = {
  status: StatusPage,
  data: DataPage,
  ib: IbPage,
  releases: ReleasesPage,
  infrastructure: InfrastructurePage,
  records: RecordsPage,
}

function readLocation(): ConsoleLocation {
  if (typeof window === 'undefined') return { kind: 'shell', id: HOME_ROUTE }
  return resolveConsoleHash(window.location.hash.replace(/^#/, '')).location
}

function locationTitle(location: ConsoleLocation): { title: string; description: string } {
  if (location.kind === 'dev-sessions') {
    return {
      title: 'Dev Sessions',
      description: 'Local dev service orchestration — tmux sessions, logs, and restart.',
    }
  }
  if (location.kind === 'approval') {
    return { title: 'Request', description: 'Approve or reject one request.' }
  }
  if (location.kind === 'question') {
    return { title: 'Question', description: 'Answer one question. Answering approves nothing.' }
  }
  if (location.kind === 'more') {
    return { title: 'More', description: 'Every detail page, with the question it answers.' }
  }
  const entry = shellNavEntry(location.id)
  return { title: entry.label, description: entry.question }
}

/**
 * Where the header's back link goes on a phone or a tablet: a request or a question back to For
 * you's waiting items, a detail page back to its entry. The entries and More have none (More takes
 * its place).
 */
function backFor(location: ConsoleLocation): HeaderBack | undefined {
  if (location.kind === 'approval' || location.kind === 'question') {
    return { label: shellNavEntry(NEEDS_YOU_ROUTE).label, href: NEEDS_YOU_HREF }
  }
  if (location.kind !== 'shell' || isEntryRoute(location.id)) return undefined
  const entry = entryOf(location)
  return entry == null ? undefined : { label: shellNavEntry(entry).label, href: formatShellHash(entry) }
}

function ShellBuildBanner() {
  const stale = useConsoleBuildDrift()
  return <ConsoleBuildBanner stale={stale} />
}

function ConsolePageInner() {
  const status = useShellStatusLine()
  const layout = useShellLayout()
  const { yours } = useOwnerYours(Date.now())
  const local = showDevSessions({
    viewerEnv: status.viewerEnv,
    viewerEnvLoading: status.viewerEnvLoading,
    devBuild: import.meta.env.DEV,
  })
  const viewerKnown = !status.viewerEnvLoading || import.meta.env.DEV === true
  const [location, setLocation] = useState<ConsoleLocation>(readLocation)

  const applyHash = useCallback(() => {
    const raw = window.location.hash.replace(/^#/, '')
    const resolved = resolveConsoleHash(raw)
    let next = resolved.location
    let canonical = resolved.canonical
    if (next.kind === 'dev-sessions' && viewerKnown && !local) {
      next = { kind: 'shell', id: HOME_ROUTE }
      canonical = formatShellHash(HOME_ROUTE)
    }
    setLocation(next)
    if (window.location.hash !== canonical) {
      window.history.replaceState(null, '', canonical)
    }
  }, [local, viewerKnown])

  useEffect(() => {
    applyHash()
    window.addEventListener('hashchange', applyHash)
    return () => window.removeEventListener('hashchange', applyHash)
  }, [applyHash])

  // Another place starts at its top: the entries share one scroll, so the old position would carry over.
  const here = locationId(location)
  useEffect(() => {
    document.scrollingElement?.scrollTo?.({ top: 0 })
  }, [here])

  const knownCount = yours.state === 'known' ? yours.count : null
  useEffect(() => {
    syncAppBadge(knownCount)
  }, [knownCount])

  const select = useCallback((id: string) => {
    if (id === 'dev-sessions') {
      window.location.hash = '#dev-sessions'
      return
    }
    const next = locationId(resolveHashTab(id).location)
    window.location.hash = formatShellHash(isShellRouteId(next) || next === MORE_ROUTE ? next : HOME_ROUTE)
  }, [])

  const entry = location.kind === 'shell' && isEntryRoute(location.id) ? location.id : null
  const Page = location.kind === 'shell' && entry == null ? DETAIL_PAGES[location.id as DetailRouteId] : null
  const { title, description } = locationTitle(location)
  const activeTab =
    location.kind === 'dev-sessions'
      ? 'dev-sessions'
      : location.kind === 'approval' || location.kind === 'question'
        ? NEEDS_YOU_ROUTE
        : locationId(location)
  const yoursBadge = yoursNavBadge(yours)
  const back = layout === 'side' ? undefined : backFor(location)
  const nav: EntryNavProps = {
    active: entryOf(location),
    onSelect: select,
    yoursBadge,
    systemTone: status.statusTone,
    systemLine: status.statusLine,
  }

  return (
    <TooltipProvider>
      <SidebarProvider>
        {layout === 'side' ? (
          <ConsoleSidebar
            activeTab={activeTab}
            onSelect={select}
            viewerEnv={status.viewerEnv}
            viewerEnvLoading={status.viewerEnvLoading}
            badges={yoursBadge != null ? { you: yoursBadge } : undefined}
          />
        ) : null}
        {layout === 'rail' ? <EntryRail {...nav} /> : null}
        <SidebarInset
          data-shell-layout={layout}
          className={cn(
            'ops-console-main min-w-0 overflow-x-clip',
            local && layout !== 'tabs' && 'pb-14',
            layout === 'tabs' && 'pb-[calc(4.5rem+env(safe-area-inset-bottom))]',
          )}
        >
          <div className="console-shell-chrome sticky top-0 z-20 shrink-0 bg-card">
            <ConsoleHeader
              pageTitle={title}
              pageDescription={description}
              statusLine={status.statusLine}
              statusTone={status.statusTone}
              healthy={status.healthy}
              onRefresh={() => {
                void status.refetch()
              }}
              viewerEnv={status.viewerEnv}
              viewerEnvLoading={status.viewerEnvLoading}
              showDevSessions={local}
              onOpenDevSessions={() => select('dev-sessions')}
              layout={layout}
              back={back}
              moreHref={back == null && location.kind !== 'more' ? formatShellHash(MORE_ROUTE) : undefined}
            />
          </div>
          <PageShell padding="compact" className="ops-page-content flex w-full min-w-0 flex-col gap-4">
            {location.kind !== 'approval' ? <ShellBuildBanner /> : null}
            {location.kind === 'dev-sessions' && local ? <DevSessionsPage /> : null}
            {location.kind === 'approval' ? <ApprovalsPage /> : null}
            {location.kind === 'question' ? <QuestionPage /> : null}
            {location.kind === 'more' ? <MorePage viewerEnv={status.viewerEnv} viewerEnvLoading={status.viewerEnvLoading} /> : null}
            {/* One slot for the four entries: moving between them keeps the Owner Console mounted. */}
            {entry != null ? <OwnerConsolePage entry={entry} /> : Page != null ? <Page /> : null}
          </PageShell>
        </SidebarInset>
        {layout === 'tabs' ? <EntryTabBar {...nav} /> : null}
        {/* With the tab bar the header's Dev Sessions indicator stands in for the dock (local console only). */}
        {local && layout !== 'tabs' ? <LocalDevSessionsDock onOpenPage={() => select('dev-sessions')} /> : null}
      </SidebarProvider>
    </TooltipProvider>
  )
}

export function ConsolePage() {
  return <ConsolePageInner />
}
