import { WorldJourney } from '@/components/shell/world-journey/WorldJourney'
import { PageShell, SidebarInset, SidebarProvider, TooltipProvider, cn } from '@bifrost/ui'
import { Suspense, lazy, useCallback, useEffect, useState, type ComponentType } from 'react'
import { ConsoleHeader, type HeaderBack, type HeaderLooks } from '@/components/ConsoleHeader'
import { cityHref, ledgerHref, readCityQuery } from '@/domains/owner/city/gen2/content/looks'
import { ConsoleSidebar } from '@/components/ConsoleSidebar'
import { EntryRail, EntryTabBar, type EntryNavProps } from '@/components/shell/EntryNav'
import { DomainNav } from '@/components/shell/DomainNav'
import { BusPage } from '@/domains/foundation/bus/BusPage'
import { MorePage } from '@/components/shell/MorePage'
import { useShellStatusLine } from '@/domains/foundation/shell/useShellStatusLine'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { QuestionPage } from '@/pages/QuestionPage'
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
import { RouteLoading } from '@/pwa/ConsoleCrashScreen'
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
import { useShellLayout } from '@/lib/shell/useShellLayout'

import { readCommunicationLook } from '@/domains/coordination/communication/looks'

type DetailRouteId = Exclude<ShellRouteId, EntryRouteId>

// Its own chunk, with Phaser behind it: nothing of the city loads until this page opens.
const CityScreen = lazy(() => import('@/pages/shell/city/CityScreen'))
// The Console's ledger of the same places: rows, no Phaser.
const CityLedger = lazy(() => import('@/pages/shell/city/CityLedger'))
const CityRoute = () => <Suspense fallback={<RouteLoading place="the ledger" />}><CityLedger /></Suspense>

const AgentScreen = lazy(() => import('@/domains/coordination/communication/AgentScreen'))
const Collaboration = lazy(() => import('@/domains/coordination/communication/CommunicationPage'))
const CollaborationRoute = () => <Suspense fallback={<RouteLoading place="Agents" />}><Collaboration /></Suspense>

const Observation = lazy(() => import('@/domains/foundation/runtime-observation/RuntimeObservation'))
const ObservationRoute = () => <Suspense fallback={<RouteLoading place="Signals" />}><Observation /></Suspense>

const DETAIL_PAGES: Record<DetailRouteId, ComponentType> = {
  observation: ObservationRoute,
  status: StatusPage,
  data: DataPage,
  ib: IbPage,
  releases: ReleasesPage,
  infrastructure: InfrastructurePage,
  bus: BusPage,
  city: CityRoute,
  collaboration: CollaborationRoute,
  records: RecordsPage,
}

function readLocation(): ConsoleLocation {
  if (typeof window === 'undefined') return { kind: 'shell', id: HOME_ROUTE }
  return resolveConsoleHash(window.location.hash.replace(/^#/, '')).location
}

function locationTitle(location: ConsoleLocation): { title: string; description: string } {
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
  const entry = entryOf(location, window.location.hash.split('?')[1])
  return entry == null ? undefined : { label: shellNavEntry(entry).label, href: formatShellHash(entry) }
}

/**
 * The looks of the place a Console page shows (the Owner, 2026-11-19: the city and the Console are peers).
 * Every Console page is a place in the city: an entry is a district, a detail page a building in its
 * entry's district. On the `#city` route the Console shows the ledger of the place in the address.
 */
function looksFor(location: ConsoleLocation): HeaderLooks | undefined {
  if (location.kind !== 'shell' || location.id === 'observation') return undefined
  if (location.id === 'collaboration') return { on: 'ledger', city: '#city', ledger: window.location.hash, page: null }
  const query = window.location.hash.split('?')[1] ?? ''
  if (location.id === 'city') {
    const { path, sel } = readCityQuery(query)
    const last = path.at(-1)
    return { on: 'ledger', city: cityHref(path, sel), ledger: ledgerHref(path, sel), page: last != null && isShellRouteId(last) && last !== 'city' ? formatShellHash(last) : null }
  }
  const path = isEntryRoute(location.id) ? [location.id] : [entryOf({ kind: 'shell', id: location.id }) ?? 'system', location.id]
  return { on: 'page', city: cityHref(path), ledger: ledgerHref(path), page: formatShellHash(location.id) }
}

function ShellBuildBanner() {
  const stale = useConsoleBuildDrift()
  return <ConsoleBuildBanner stale={stale} />
}

function ConsolePageInner() {
  const status = useShellStatusLine()
  const layout = useShellLayout()
  const { yours } = useOwnerYours(Date.now())
  const [location, setLocation] = useState<ConsoleLocation>(readLocation)

  const applyHash = useCallback(() => {
    const raw = window.location.hash.replace(/^#/, '')
    const resolved = resolveConsoleHash(raw)
    setLocation(resolved.location)
    if (window.location.hash !== resolved.canonical) {
      window.history.replaceState(null, '', resolved.canonical)
    }
  }, [])

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
    const next = locationId(resolveHashTab(id).location)
    window.location.hash = formatShellHash(isShellRouteId(next) || next === MORE_ROUTE ? next : HOME_ROUTE)
  }, [])

  const entry = location.kind === 'shell' && isEntryRoute(location.id) ? location.id : null
  const Page = location.kind === 'shell' && entry == null ? DETAIL_PAGES[location.id as DetailRouteId] : null
  const { title, description } = locationTitle(location)
  const activeTab =
    location.kind === 'approval' || location.kind === 'question'
      ? NEEDS_YOU_ROUTE
      : locationId(location)
  const yoursBadge = yoursNavBadge(yours)
  const back = layout === 'side' ? undefined : backFor(location)
  const domain = entryOf(location, window.location.hash.split('?')[1])
  // The city look takes the whole screen, outside this frame: the Console does not hold it.
  const cityLook = location.kind === 'shell' && location.id === 'city' && readCityQuery(window.location.hash.split('?')[1] ?? '').look === 'city'
  const nav: EntryNavProps = {
    active: domain,
    onSelect: select,
    yoursBadge,
    systemTone: status.statusTone,
    systemLine: status.statusLine,
  }

  const agentsLook = location.kind === 'shell' && location.id === 'collaboration' && readCommunicationLook(window.location.hash.split('?')[1] ?? '').look === 'agents'
  if (location.kind === 'shell' && location.id === 'observation') return <Suspense fallback={<RouteLoading full place="Signals" />}><Observation /></Suspense>
  if (agentsLook) return <><Suspense fallback={<RouteLoading full place="Agents" />}><AgentScreen /></Suspense><SignalModeLink /></>
  if (cityLook) return <><Suspense fallback={<RouteLoading full place="the City" />}><CityScreen /></Suspense><SignalModeLink /></>
  return (
    <TooltipProvider>
      <SidebarProvider>
        {layout === 'side' ? (
          <ConsoleSidebar
            activeTab={activeTab}
            onSelect={select}
            viewerEnv={status.viewerEnv}
            viewerEnvLoading={status.viewerEnvLoading}
            badges={yoursBadge != null ? { command: yoursBadge } : undefined}
          />
        ) : null}
        {layout === 'rail' ? <EntryRail {...nav} /> : null}
        <SidebarInset
          data-shell-layout={layout}
          className={cn(
            'ops-console-main min-w-0 overflow-x-clip',
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
              layout={layout}
              back={back}
              moreHref={undefined}
              looks={looksFor(location)}
            />
            {layout !== 'side' && domain && <DomainNav entry={domain} />}
          </div>
          <PageShell padding="compact" className={cn('ops-page-content flex w-full min-w-0 flex-col gap-4', entry !== 'command' && location.kind !== 'approval' && location.kind !== 'question' && 'ops-workspace')}>
            {location.kind !== 'approval' ? <ShellBuildBanner /> : null}
            {location.kind === 'approval' ? <ApprovalsPage /> : null}
            {location.kind === 'question' ? <QuestionPage /> : null}
            {location.kind === 'more' ? <MorePage viewerEnv={status.viewerEnv} viewerEnvLoading={status.viewerEnvLoading} /> : null}
            {/* Keep the Owner Console mounted across domains and details so Command drafts survive. */}
            <div hidden={entry == null}><OwnerConsolePage entry={entry} /></div>
            {Page != null ? <Page /> : null}
          </PageShell>
        </SidebarInset>
        {layout === 'tabs' ? <EntryTabBar {...nav} /> : null}
      </SidebarProvider>
    </TooltipProvider>
  )
}

export function ConsolePage() {
  return <><ConsolePageInner /><WorldJourney /></>
}

/** Shell-owned peer entry; leaves the City and communication renderers untouched. */
function SignalModeLink() {
  return <a href="#observation" aria-label="Open Signals view" style={{position:'fixed',right:28,top:78,zIndex:45,padding:'6px 14px',border:'1px solid #496575',borderRadius:16,background:'#102330',color:'#9ae6d1',font:'12px system-ui',textDecoration:'none'}}>Signals ↗</a>
}
