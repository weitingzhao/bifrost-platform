import { useMemo, type Ref } from 'react'
import { ClusterServiceReadinessPanel } from '@/components/cluster/ClusterServiceReadinessPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { SatelliteObservabilityStrip } from '@/components/satellite/SatelliteObservabilityStrip'
import { PayloadReadinessTable } from '@/components/control-room/PayloadDepthPanel'
import type { PayloadReadinessRow } from '@/lib/control-room/payloadReadiness'
import { sharedContextSignal } from '@/lib/satellite-bus/contextSectionSignal'
import type { SocketHealthRow } from '@/lib/satellite/socketHealthSemantics'
import {
  RocketSocketBusRow,
  SecondaryGroup,
} from '@/pages/satellite-bus/satelliteBusTableParts'
import {
  SATELLITE_DOMAIN_IDS,
  type useSatelliteBusQueries,
} from '@/pages/satellite-bus/useSatelliteBusQueries'

type BusQueries = ReturnType<typeof useSatelliteBusQueries>

export function SatelliteBusSharedRocketSection({
  rocketRow,
  payloadRows,
  serviceReadinessQuery,
  metricsQuery,
  observabilityQuery,
  sharedOpen,
  setSharedOpen,
  sharedSectionRef,
  highlightSection,
  onOpenCluster,
  onOpenTelemetry,
  onOpenObservability,
}: {
  rocketRow: SocketHealthRow
  payloadRows: PayloadReadinessRow[]
  serviceReadinessQuery: BusQueries['serviceReadinessQuery']
  metricsQuery: BusQueries['metricsQuery']
  observabilityQuery: BusQueries['observabilityQuery']
  sharedOpen: boolean
  setSharedOpen: (open: boolean) => void
  sharedSectionRef: Ref<HTMLDetailsElement>
  highlightSection: string | null
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  onOpenObservability?: () => void
}) {
  const signal = useMemo(
    () => sharedContextSignal(rocketRow, payloadRows),
    [payloadRows, rocketRow],
  )
  return (
    <SecondaryGroup
      title="Rocket + Ground"
      description="Shared IB socket bus and cluster readiness — same for every Trade NS"
      badgeLabel="Shared"
      scope="rocket"
      signal={signal}
      open={sharedOpen}
      onOpenChange={setSharedOpen}
      sectionRef={sharedSectionRef}
      highlight={highlightSection === 'rocket' || highlightSection === 'cluster'}
    >
      <OpsSection
        variant="flat"
        title="Rocket · Platform IB Gateway"
        bodyPadding="compact"
        overflow="hidden"
        description="Authoritative quote/account/operator path for all trade namespaces (data/ib-gateway @ redis-ib)"
      >
        <RocketSocketBusRow row={rocketRow} />
      </OpsSection>
      <OpsSection variant="flat" title="Payload readiness (Fleet projection)" bodyPadding="none" overflow="hidden">
        <PayloadReadinessTable rows={payloadRows} showActions={false} />
      </OpsSection>
      <div className="grid lg:grid-cols-2 lg:divide-x lg:divide-[var(--border)]">
        <OpsSection variant="flat" title="Service domains" bodyPadding="none" overflow="hidden">
          {SATELLITE_DOMAIN_IDS.map(domainId => (
            <ClusterServiceReadinessPanel
              key={domainId}
              data={serviceReadinessQuery.data}
              isLoading={serviceReadinessQuery.isLoading}
              compact
              variant="flat"
              domainFilter={domainId}
            />
          ))}
        </OpsSection>
        <SatelliteObservabilityStrip
          variant="flat"
          metrics={metricsQuery.data}
          observability={observabilityQuery.data}
          metricsLoading={metricsQuery.isLoading}
          observabilityLoading={observabilityQuery.isLoading}
          onOpenCluster={onOpenCluster}
          onOpenTelemetry={onOpenTelemetry}
          onOpenObservability={onOpenObservability}
        />
      </div>
      {signal.reach !== 'ok' && (
        <div className="flex flex-wrap items-center gap-2 px-3 py-1.5 border-t border-[var(--border)]">
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Shared {signal.label}{signal.detail != null ? ` — ${signal.detail}` : ''}
          </span>
          <div className="flex items-center gap-2 ml-auto">
            {onOpenCluster != null && (
              <button
                type="button"
                className="focus-strip-link text-[var(--text-dense-caption)]"
                onClick={onOpenCluster}
              >
                Open Cluster
              </button>
            )}
            {onOpenObservability != null && (
              <button
                type="button"
                className="focus-strip-link text-[var(--text-dense-caption)]"
                onClick={onOpenObservability}
              >
                Observability
              </button>
            )}
          </div>
        </div>
      )}
    </SecondaryGroup>
  )
}
