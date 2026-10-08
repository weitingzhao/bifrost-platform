import { useQuery } from '@tanstack/react-query'
import { fetchContext } from '@/api/core'
import { ClusterPage } from '@/pages/ClusterPage'
import { NetworkPage } from '@/pages/NetworkPage'
import { MiniCards } from '@/pages/shell/infrastructure/MiniCards'
import { RuntimeMapSection } from '@/pages/shell/infrastructure/RuntimeMapSection'

export function InfrastructurePage() {
  const contextQ = useQuery({
    queryKey: ['ops', 'context', 'infrastructure'],
    queryFn: fetchContext,
    staleTime: 60_000,
  })

  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <section className="flex flex-col gap-2">
        <h2 className="m-0 text-sm font-semibold">Cluster</h2>
        <ClusterPage />
      </section>
      <section className="flex flex-col gap-2">
        <h2 className="m-0 text-sm font-semibold">Network</h2>
        <NetworkPage
          context={contextQ.data}
          onOpenAgentProtocol={() => {
            window.location.hash = '#agent-protocol'
          }}
        />
      </section>
      <RuntimeMapSection />
      <MiniCards />
    </div>
  )
}
