import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
import { fetchNetworkClients, fetchNetworkDevices } from '@/api/platform'
import { NetworkHealthPanel } from '@/components/control-room/NetworkHealthPanel'
import { NetworkClientsPanel } from '@/components/network/NetworkClientsPanel'
import { NetworkDevicesPanel } from '@/components/network/NetworkDevicesPanel'
import { useNetworkLiveProbe } from '@/hooks/useNetworkLiveProbe'

export function NetworkPage({
  context,
  onOpenAgentProtocol,
}: {
  context: OpsContextResponse | undefined
  onOpenAgentProtocol: () => void
}) {
  const liveProbe = useNetworkLiveProbe()

  const devicesQuery = useQuery({
    queryKey: ['network', 'devices'],
    queryFn: fetchNetworkDevices,
    refetchInterval: 60_000,
    enabled: liveProbe.probeReach === 'ok' || liveProbe.probeReach === 'degraded',
  })

  const clientsQuery = useQuery({
    queryKey: ['network', 'clients'],
    queryFn: fetchNetworkClients,
    refetchInterval: 60_000,
    enabled: liveProbe.probeReach === 'ok' || liveProbe.probeReach === 'degraded',
  })

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="Network"
        description="Ground floor LAN / UniFi — live UCG probe, firewall drift audit, devices, and clients."
      />

      <NetworkHealthPanel context={context} onOpenAgentProtocol={onOpenAgentProtocol} />

      <NetworkDevicesPanel data={devicesQuery.data} isLoading={devicesQuery.isLoading} />

      <NetworkClientsPanel data={clientsQuery.data} isLoading={clientsQuery.isLoading} />
    </div>
  )
}
