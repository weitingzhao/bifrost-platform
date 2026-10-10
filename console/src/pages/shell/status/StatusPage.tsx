import { useQuery } from '@tanstack/react-query'
import { fetchMatrix } from '@/api/core'
import { ControlRoomPage } from '@/pages/ControlRoomPage'
import { ObservabilityPage } from '@/pages/ObservabilityPage'
import { RocketHealthPage } from '@/pages/RocketHealthPage'
import { SatelliteHealthPage } from '@/pages/SatelliteHealthPage'
import { AgentThreadsInProgress } from '@/pages/shell/status/AgentThreadsInProgress'
import { ChecklistSignalsSummary } from '@/pages/shell/status/ChecklistSignalsSummary'
import { SystemVerdictLine } from '@/components/shell/SystemVerdictLine'

function openHash(hash: string) {
  if (window.location.hash !== hash) window.location.hash = hash
}

/** Status: the header's verdict with its red and yellow items, agent threads in progress, then posture, observability, platform, Trade. */
export function StatusPage() {
  const matrixQ = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    refetchInterval: 30_000,
    retry: false,
  })
  const matrixError = matrixQ.error instanceof Error ? matrixQ.error : null

  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <SystemVerdictLine showCauses />
      <AgentThreadsInProgress />
      <section aria-label="Control room" className="flex w-full min-w-0 flex-col">
        <ChecklistSignalsSummary />
        <ControlRoomPage
          context={undefined}
          matrixLoading={matrixQ.isLoading}
          matrixError={matrixError}
          onOpenNetwork={() => openHash('#infrastructure')}
        />
      </section>
      <section aria-label="Observability" className="flex w-full min-w-0 flex-col">
        <ObservabilityPage
          lockToViewer
          onNavigate={tab => openHash(tab.startsWith('#') ? tab : `#${tab}`)}
        />
      </section>
      <section aria-label="Platform" className="flex w-full min-w-0 flex-col">
        <RocketHealthPage lockToViewer />
      </section>
      <section aria-label="Trade" className="flex w-full min-w-0 flex-col">
        <SatelliteHealthPage lockToViewer />
      </section>
    </div>
  )
}
