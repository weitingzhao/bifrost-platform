import { PageShell, SidebarInset, SidebarProvider, TooltipProvider } from '@bifrost/ui'
import { useCallback, useEffect, useState, type ComponentType } from 'react'
import { ConsoleHeader } from '@/components/ConsoleHeader'
import { ConsoleSidebar } from '@/components/ConsoleSidebar'
import { LocalDevSessionsDock } from '@/components/shell/LocalDevSessionsDock'
import { useShellStatusLine } from '@/hooks/useShellStatusLine'
import { DevSessionsPage } from '@/pages/DevSessionsPage'
import {
  DataPage,
  IbPage,
  InfrastructurePage,
  MaintenancePage,
  ProgressPage,
  ReleasesPage,
  StatusPage,
} from '@/pages/shell/shellPages'
import {
  formatShellHash,
  hashQueryWithoutTaskMode,
  isShellRouteId,
  locationId,
  resolveHashTab,
  shellNavEntry,
  type ConsoleLocation,
  type ShellRouteId,
} from '@/lib/shell/consoleRoutes'
import { showDevSessions } from '@/lib/shell/localConsole'

const SHELL_PAGES: Record<ShellRouteId, ComponentType> = {
  status: StatusPage,
  data: DataPage,
  ib: IbPage,
  maintenance: MaintenancePage,
  releases: ReleasesPage,
  infrastructure: InfrastructurePage,
  progress: ProgressPage,
}

function readLocation(): ConsoleLocation {
  if (typeof window === 'undefined') return { kind: 'shell', id: 'status' }
  const tab = window.location.hash.replace(/^#/, '').split('?')[0] ?? ''
  return resolveHashTab(tab).location
}

function ConsolePageInner() {
  const status = useShellStatusLine()
  const local = showDevSessions({
    viewerEnv: status.viewerEnv,
    viewerEnvLoading: status.viewerEnvLoading,
    devBuild: import.meta.env.DEV,
  })
  const viewerKnown = !status.viewerEnvLoading || import.meta.env.DEV === true
  const [location, setLocation] = useState<ConsoleLocation>(readLocation)

  const applyHash = useCallback(() => {
    const raw = window.location.hash.replace(/^#/, '')
    const tab = raw.split('?')[0] ?? ''
    const query = hashQueryWithoutTaskMode(raw)
    const resolved = resolveHashTab(tab)
    let next = resolved.location
    if (next.kind === 'dev-sessions' && viewerKnown && !local) {
      next = { kind: 'shell', id: 'status' }
    }
    setLocation(next)
    const id = locationId(next)
    const dropQuery = tab === 'dev-sessions' && viewerKnown && !local
    const canonical = formatShellHash(id, dropQuery ? '' : query)
    const shouldRewrite =
      resolved.legacy ||
      tab === '' ||
      (tab === 'dev-sessions' && viewerKnown && !local) ||
      (tab !== '' && tab !== id && resolved.legacy)
    if ((shouldRewrite || tab === '') && window.location.hash !== canonical) {
      window.history.replaceState(null, '', canonical)
    }
  }, [local, viewerKnown])

  useEffect(() => {
    applyHash()
    window.addEventListener('hashchange', applyHash)
    return () => window.removeEventListener('hashchange', applyHash)
  }, [applyHash])

  const select = useCallback((id: string) => {
    if (id === 'dev-sessions') {
      window.location.hash = '#dev-sessions'
      return
    }
    const resolved = resolveHashTab(id)
    const next = locationId(resolved.location)
    window.location.hash = formatShellHash(isShellRouteId(next) ? next : 'status')
  }, [])

  const pageId = location.kind === 'dev-sessions' ? null : location.id
  const Page = pageId != null ? SHELL_PAGES[pageId] : null
  const title =
    location.kind === 'dev-sessions'
      ? 'Dev Sessions'
      : shellNavEntry(location.id).label
  const description =
    location.kind === 'dev-sessions'
      ? 'Local dev service orchestration — tmux sessions, logs, and restart.'
      : shellNavEntry(location.id).question

  return (
    <TooltipProvider>
      <SidebarProvider>
        <ConsoleSidebar
          activeTab={location.kind === 'dev-sessions' ? 'dev-sessions' : location.id}
          onSelect={select}
          viewerEnv={status.viewerEnv}
          viewerEnvLoading={status.viewerEnvLoading}
        />
        <SidebarInset className={local ? 'min-w-0 overflow-x-hidden pb-14' : 'min-w-0 overflow-x-hidden'}>
          <div className="console-shell-chrome sticky top-0 z-20 shrink-0 bg-card">
            <ConsoleHeader
              pageTitle={title}
              pageDescription={description}
              statusLine={status.statusLine}
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
