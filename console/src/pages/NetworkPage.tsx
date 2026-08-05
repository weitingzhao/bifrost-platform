import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import {
  fetchNetworkAnomalies,
  fetchNetworkClients,
  fetchNetworkDevices,
  fetchNetworkSla,
} from '@/api/network'
import { NetworkHealthPanel } from '@/components/control-room/NetworkHealthPanel'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import type { OpsVerdictLamp, OpsVerdictTagVariant } from '@/components/layout/OpsVerdictStrip'
import { NetworkClientsPanel } from '@/components/network/NetworkClientsPanel'
import { NetworkDevicesPanel } from '@/components/network/NetworkDevicesPanel'
import { NetworkFirewallPanel } from '@/components/network/NetworkFirewallPanel'
import { NetworkSlaPanel } from '@/components/network/NetworkSlaPanel'
import { useNetworkLiveProbe } from '@/hooks/useNetworkLiveProbe'
import { NETWORK_HEALTH_PROJECTION } from '@/lib/architecture/networkConsoleProjection'

function networkVerdict(
  liveProbe: ReturnType<typeof useNetworkLiveProbe>,
  devicesLoading: boolean,
  clientsLoading: boolean,
  deviceCount: number | undefined,
  clientCount: number | undefined,
  anomalyCount: number,
): {
  lamp: OpsVerdictLamp
  tagLabel: string
  tagVariant: OpsVerdictTagVariant
  summary: string
} {
  if (liveProbe.isLoading) {
    return {
      lamp: 'unknown',
      tagLabel: 'LOADING',
      tagVariant: 'neutral',
      summary: 'Probing UCG via platform-api… devices/clients wait on probe.',
    }
  }

  const classification = liveProbe.audit?.classification
  const inventoryCtx =
    liveProbe.probeReach === 'ok' || liveProbe.probeReach === 'degraded'
      ? devicesLoading || clientsLoading
        ? 'Loading devices/clients…'
        : `${deviceCount ?? 0} devices · ${clientCount ?? 0} clients`
      : 'Devices/clients paused until probe is reachable'

  const anomalyNote = anomalyCount > 0 ? ` · ${anomalyCount} anomaly alert(s)` : ''

  if (liveProbe.probeReach === 'fail') {
    return {
      lamp: 'fail',
      tagLabel: 'FAIL',
      tagVariant: 'danger',
      summary: `${liveProbe.summary} · ${inventoryCtx}${anomalyNote}`,
    }
  }

  if (liveProbe.probeReach === 'unknown') {
    return {
      lamp: 'unknown',
      tagLabel: 'UNKNOWN',
      tagVariant: 'neutral',
      summary: `${liveProbe.summary} · ${inventoryCtx}${anomalyNote}`,
    }
  }

  if (classification === 'POLICY_DRIFT' || liveProbe.probeReach === 'degraded' || anomalyCount > 0) {
    return {
      lamp: 'degraded',
      tagLabel:
        anomalyCount > 0
          ? 'ANOMALY'
          : classification === 'POLICY_DRIFT'
            ? 'POLICY DRIFT'
            : 'DEGRADED',
      tagVariant: 'warning',
      summary: `${liveProbe.summary} · ${inventoryCtx}${anomalyNote}`,
    }
  }

  return {
    lamp: 'ok',
    tagLabel: 'OK',
    tagVariant: 'success',
    summary: `${liveProbe.summary} · ${inventoryCtx}${anomalyNote}`,
  }
}

export function NetworkPage({
  context,
  onOpenAgentProtocol,
}: {
  context: OpsContextResponse | undefined
  onOpenAgentProtocol: () => void
}) {
  const liveProbe = useNetworkLiveProbe()
  const spineLoaded = context?.tracks?.infra != null
  const probeLive = liveProbe.probeReach === 'ok' || liveProbe.probeReach === 'degraded'

  const devicesQuery = useQuery({
    queryKey: ['network', 'devices'],
    queryFn: fetchNetworkDevices,
    refetchInterval: 60_000,
    enabled: probeLive,
  })

  const clientsQuery = useQuery({
    queryKey: ['network', 'clients'],
    queryFn: fetchNetworkClients,
    refetchInterval: 60_000,
    enabled: probeLive,
  })

  const anomaliesQuery = useQuery({
    queryKey: ['network', 'anomalies'],
    queryFn: fetchNetworkAnomalies,
    refetchInterval: 60_000,
  })

  const slaQuery = useQuery({
    queryKey: ['network', 'sla'],
    queryFn: fetchNetworkSla,
    refetchInterval: 60_000,
  })

  const anomalyCount = anomaliesQuery.data?.count ?? anomaliesQuery.data?.alerts?.length ?? 0

  const verdict = networkVerdict(
    liveProbe,
    devicesQuery.isLoading,
    clientsQuery.isLoading,
    devicesQuery.data?.count ?? devicesQuery.data?.devices?.length,
    clientsQuery.data?.count ?? clientsQuery.data?.clients?.length,
    anomalyCount,
  )

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Network ground verdict"
        title="NETWORK · GROUND"
        lamp={verdict.lamp}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
        summary={verdict.summary}
        actions={
          <Button variant="outline" size="sm" onClick={onOpenAgentProtocol}>
            Agent Protocol
          </Button>
        }
        meta={
          <>
            <span>
              Catalog {NETWORK_HEALTH_PROJECTION.catalogVersion} ·{' '}
              {spineLoaded ? 'spine + catalog' : 'catalog fallback'}
            </span>
            <span>Live probe every 30s · L0 health/bandwidth/anomalies/SLA</span>
            {anomalyCount > 0 && <DenseTag variant="warning">{anomalyCount} alerts</DenseTag>}
          </>
        }
      />

      <NetworkHealthPanel
        context={context}
        onOpenAgentProtocol={onOpenAgentProtocol}
        showPrimaryAgentAction={false}
        title="Probe & stream evidence"
        description="Catalog/spine stream projection and live UniFi probe detail (GET /api/v1/network/status + audit + health, Session v2 / D9)."
      />

      <NetworkSlaPanel
        sla={slaQuery.data}
        anomalies={anomaliesQuery.data}
        isLoading={slaQuery.isLoading || anomaliesQuery.isLoading}
      />

      <NetworkFirewallPanel />

      <NetworkDevicesPanel data={devicesQuery.data} isLoading={devicesQuery.isLoading} />

      <NetworkClientsPanel data={clientsQuery.data} isLoading={clientsQuery.isLoading} />
    </div>
  )
}
