import { useQuery } from '@tanstack/react-query'
import { fetchMatrix } from '@/api/core'
import { ControlRoomPage } from '@/pages/ControlRoomPage'
import { ObservabilityPage } from '@/pages/ObservabilityPage'
import { RocketHealthPage } from '@/pages/RocketHealthPage'
import { ChecklistSignalsSummary } from '@/pages/shell/status/ChecklistSignalsSummary'
import { SystemVerdictLine } from '@/components/shell/SystemVerdictLine'

function openHash(hash: string) {
  if (window.location.hash !== hash) window.location.hash = hash
}

/** System status: global verdict, posture, observability and platform health. */
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
      <a href="#work?section=activity">Agent threads and work activity</a>
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
      <a href="#services?section=applications">Application health · Services</a>
    </div>
  )
}
