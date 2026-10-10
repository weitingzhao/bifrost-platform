import { PageShell, SidebarInset, SidebarProvider, TooltipProvider } from '@bifrost/ui'
import { useCallback, useEffect, useState, type ComponentType } from 'react'
import { ConsoleHeader } from '@/components/ConsoleHeader'
import { ConsoleSidebar } from '@/components/ConsoleSidebar'
import { LocalDevSessionsDock } from '@/components/shell/LocalDevSessionsDock'
import { useShellStatusLine } from '@/hooks/useShellStatusLine'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { DevSessionsPage } from '@/pages/DevSessionsPage'
import {
  DataPage,
  IbPage,
  InfrastructurePage,
  NeedsYouPage,
  ProgressPage,
  RecordsPage,
  ReleasesPage,
  StatusPage,
} from '@/pages/shell/shellPages'
import { needsYouBadge } from '@/pages/shell/needs-you/needsYouModel'
import { useNeedsYou } from '@/pages/shell/needs-you/useNeedsYou'
import { syncAppBadge } from '@/pwa/appBadge'
import {
  HOME_ROUTE,
  formatShellHash,
  isShellRouteId,
  locationId,
  resolveConsoleHash,
  resolveHashTab,
  shellNavEntry,
  type ConsoleLocation,
  type ShellRouteId,
} from '@/lib/shell/consoleRoutes'
import { showDevSessions } from '@/lib/shell/localConsole'

const SHELL_PAGES: Record<ShellRouteId, ComponentType> = {
  'needs-you': NeedsYouPage,
  status: StatusPage,
  data: DataPage,
  ib: IbPage,
  releases: ReleasesPage,
  infrastructure: InfrastructurePage,
  progress: ProgressPage,
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
  const entry = shellNavEntry(location.id)
  return { title: entry.label, description: entry.question }
}

function ConsolePageInner() {
  const status = useShellStatusLine()
  const needsYou = useNeedsYou()
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

  const count = needsYou.count
  const knownCount = count.state === 'known' ? count.count : null
  useEffect(() => {
    syncAppBadge(knownCount)
  }, [knownCount])

  const select = useCallback((id: string) => {
    if (id === 'dev-sessions') {
      window.location.hash = '#dev-sessions'
      return
    }
    const next = locationId(resolveHashTab(id).location)
    window.location.hash = formatShellHash(isShellRouteId(next) ? next : HOME_ROUTE)
  }, [])

  const Page = location.kind === 'shell' ? SHELL_PAGES[location.id] : null
  const { title, description } = locationTitle(location)
  const activeTab =
    location.kind === 'dev-sessions'
      ? 'dev-sessions'
      : location.kind === 'approval'
        ? HOME_ROUTE
        : location.id
  const needsYouNavBadge = needsYouBadge(count)

  return (
    <TooltipProvider>
      <SidebarProvider>
        <ConsoleSidebar
          activeTab={activeTab}
          onSelect={select}
          viewerEnv={status.viewerEnv}
          viewerEnvLoading={status.viewerEnvLoading}
          badges={needsYouNavBadge != null ? { [HOME_ROUTE]: needsYouNavBadge } : undefined}
        />
        <SidebarInset className={local ? 'min-w-0 overflow-x-hidden pb-14' : 'min-w-0 overflow-x-hidden'}>
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
            />
          </div>
          <PageShell padding="compact" className="flex w-full min-w-0 flex-col gap-4">
            {location.kind === 'dev-sessions' && local ? <DevSessionsPage /> : null}
            {location.kind === 'approval' ? <ApprovalsPage /> : null}
            {Page != null ? <Page /> : null}
          </PageShell>
        </SidebarInset>
        {local ? <LocalDevSessionsDock onOpenPage={() => select('dev-sessions')} /> : null}
      </SidebarProvider>
    </TooltipProvider>
  )
}

export function ConsolePage() {
  return <ConsolePageInner />
}
