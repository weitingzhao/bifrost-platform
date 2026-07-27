import { useMemo, type Ref } from 'react'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { ClusterServiceReadinessPanel } from '@/components/cluster/ClusterServiceReadinessPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { SatelliteObservabilityStrip } from '@/components/satellite/SatelliteObservabilityStrip'
import { StatusLamp } from '@/components/StatusLamp'
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
  const qc = useQueryClient()
  const domainsFetching = useIsFetching({ queryKey: ['cluster', 'service-readiness'] }) > 0
  const readiness = serviceReadinessQuery.data

  const signal = useMemo(() => {
    const domainIds = new Set(SATELLITE_DOMAIN_IDS)
    const domains = (readiness?.domains ?? [])
      .filter(d => domainIds.has(d.id as (typeof SATELLITE_DOMAIN_IDS)[number]))
      .map(d => ({
        id: d.id,
        label: d.label,
        status: d.status,
        reachability: d.reachability,
        summary: d.summary,
      }))
    return sharedContextSignal(rocketRow, payloadRows, domains)
  }, [payloadRows, readiness?.domains, rocketRow])

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
      <div className="satellite-bus-shared-lanes flex flex-col">
        <OpsSection
          variant="flat"
          title="1 · Rocket"
          bodyPadding="compact"
          overflow="hidden"
          description="Platform IB Gateway — quote / account / operator path (data/ib-gateway @ redis-ib)"
        >
          <RocketSocketBusRow row={rocketRow} />
        </OpsSection>

        <OpsSection
          variant="flat"
          title="2 · Payload readiness"
          bodyPadding="none"
          overflow="hidden"
          description="Fleet projection across Dev / Stg / Prod — not Service Domains"
        >
          <PayloadReadinessTable rows={payloadRows} showActions={false} />
        </OpsSection>

        <OpsSection
          variant="flat"
          title="3 · Service domains"
          bodyPadding="compact"
          overflow="hidden"
          description="Cluster workload readiness — workers, applications, database, redis"
          actions={
            <div className="flex flex-wrap items-center gap-2">
              {readiness != null && (
                <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] inline-flex items-center gap-1">
                  <StatusLamp value={readiness.reachability} kind="reach" />
                  {readiness.detail}
                </span>
              )}
              <SectionRefreshButton
                isFetching={domainsFetching || serviceReadinessQuery.isLoading}
                onClick={() => {
                  void qc.invalidateQueries({ queryKey: ['cluster', 'service-readiness'] })
                }}
              />
            </div>
          }
        >
          <div className="satellite-bus-shared-domain-grid grid gap-2 sm:grid-cols-2">
            {SATELLITE_DOMAIN_IDS.map(domainId => (
              <ClusterServiceReadinessPanel
                key={domainId}
                data={readiness}
                isLoading={serviceReadinessQuery.isLoading}
                compact
                embedded
                domainFilter={domainId}
              />
            ))}
          </div>
        </OpsSection>

        <SatelliteObservabilityStrip
          variant="flat"
          title="4 · Monitoring coverage"
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
        <div className="flex flex-wrap items-center gap-2 border-t border-[var(--border)] px-3 py-1.5">
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Shared {signal.label}
            {signal.detail != null ? ` — ${signal.detail}` : ''}
          </span>
          <div className="ml-auto flex items-center gap-2">
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
