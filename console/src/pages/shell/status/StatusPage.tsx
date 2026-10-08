import { useQuery } from '@tanstack/react-query'
import { fetchContext, fetchMatrix, isAllMatrices } from '@/api/core'
import type { MatrixResponse } from '@/api/matrixTypes'
import { ControlRoomPage } from '@/pages/ControlRoomPage'
import { ObservabilityPage } from '@/pages/ObservabilityPage'
import { RocketHealthPage } from '@/pages/RocketHealthPage'
import { SatelliteHealthPage } from '@/pages/SatelliteHealthPage'

function openHash(hash: string) {
  if (window.location.hash !== hash) window.location.hash = hash
}

function asMatrices(data: Awaited<ReturnType<typeof fetchMatrix>> | undefined): MatrixResponse[] {
  if (data == null) return []
  return isAllMatrices(data) ? data.matrices : [data]
}

/** Status: control-room posture, observability, platform card, Trade card. */
export function StatusPage() {
  const contextQ = useQuery({
    queryKey: ['ops', 'context', 'status'],
    queryFn: fetchContext,
    refetchInterval: 30_000,
    retry: false,
  })
  const matrixQ = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    refetchInterval: 30_000,
    retry: false,
  })
  const matrixError = matrixQ.error instanceof Error ? matrixQ.error : null

  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <section aria-label="Control room" className="flex w-full min-w-0 flex-col">
        <ControlRoomPage
          surface="status"
          context={contextQ.data}
          contextLoading={contextQ.isLoading}
          matrices={asMatrices(matrixQ.data)}
          matrixLoading={matrixQ.isLoading}
          matrixError={matrixError}
          platformHealthy
          onOpenRuntimeMap={() => openHash('#infrastructure')}
          onOpenDelivery={() => openHash('#releases')}
          onOpenCluster={() => openHash('#infrastructure')}
          onOpenAudit={() => openHash('#maintenance')}
          onOpenNetwork={() => openHash('#infrastructure')}
          onOpenAgentProtocol={() => openHash('#progress')}
        />
      </section>
      <section aria-label="Observability" className="flex w-full min-w-0 flex-col">
        <ObservabilityPage
          lockToViewer
          hideAgentActions
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
