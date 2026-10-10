import { useQuery } from '@tanstack/react-query'
import { fetchMatrix, isAllMatrices } from '@/api/core'
import type { MatrixResponse } from '@/api/matrixTypes'
import { ControlRoomPage } from '@/pages/ControlRoomPage'
import { ObservabilityPage } from '@/pages/ObservabilityPage'
import { RocketHealthPage } from '@/pages/RocketHealthPage'
import { SatelliteHealthPage } from '@/pages/SatelliteHealthPage'
import { ChecklistSignalsSummary } from '@/pages/shell/status/ChecklistSignalsSummary'
import { SystemVerdictLine } from '@/components/shell/SystemVerdictLine'

function openHash(hash: string) {
  if (window.location.hash !== hash) window.location.hash = hash
}

function asMatrices(data: Awaited<ReturnType<typeof fetchMatrix>> | undefined): MatrixResponse[] {
  if (data == null) return []
  return isAllMatrices(data) ? data.matrices : [data]
}

/** Status: the header's verdict with its red and yellow items, then posture, observability, platform, Trade. */
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
      <section aria-label="Control room" className="flex w-full min-w-0 flex-col">
        <ChecklistSignalsSummary />
        <ControlRoomPage
          surface="status"
          context={undefined}
          contextLoading={false}
          matrices={asMatrices(matrixQ.data)}
          matrixLoading={matrixQ.isLoading}
          matrixError={matrixError}
          platformHealthy
          onOpenRuntimeMap={() => openHash('#infrastructure')}
          onOpenDelivery={() => openHash('#releases')}
          onOpenCluster={() => openHash('#infrastructure')}
          onOpenAudit={() => openHash('#records?tab=audit')}
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
