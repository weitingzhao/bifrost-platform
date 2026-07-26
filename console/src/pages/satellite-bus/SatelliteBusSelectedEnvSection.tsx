import type { Ref } from 'react'
import { DenseTag, cn } from '@bifrost/ui'
import type { ClusterWorkload } from '@/api/clusterTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  busScopeGroupClass,
  tradeSingleEnvProbeSource,
  tradeSingleEnvScope,
} from '@/lib/satellite/busStatusScope'
import type { SatelliteBusViewModel } from '@/lib/satellite-bus/satelliteBusViewModel'
import type { InspectTarget } from '@/pages/satellite-bus/inspectTypes'
import { ConsumerTable } from '@/pages/satellite-bus/satelliteBusTableParts'
import { TradeDaemonOperatePanel } from '@/pages/satellite-bus/TradeDaemonOperatePanel'
import type { TradeEnv } from '@/pages/satellite-bus/useSatelliteBusQueries'

export function SatelliteBusSelectedEnvSection({
  tradeEnv,
  ns,
  viewModel,
  busLoading,
  highlightSection,
  selectedSectionRef,
  openInspect,
  canOperate,
  workloads,
  workloadsLoading,
}: {
  tradeEnv: TradeEnv
  ns: string
  viewModel: SatelliteBusViewModel
  busLoading: boolean
  highlightSection: string | null
  selectedSectionRef: Ref<HTMLDivElement>
  openInspect: (target: InspectTarget) => void
  canOperate: boolean
  workloads: ClusterWorkload[]
  workloadsLoading: boolean
}) {
  const singleEnvScope = tradeSingleEnvScope(tradeEnv)

  return (
    <div
      ref={selectedSectionRef}
      data-scope="trade-single-env"
      className={cn(
        'satellite-bus-group panel-elevated flex flex-col overflow-hidden rounded-md transition-shadow',
        busScopeGroupClass('trade-single-env'),
        highlightSection === 'monitor' && 'ring-1 ring-[var(--ring)] ring-offset-1 ring-offset-[var(--background)]',
      )}
    >
      <header className="satellite-bus-group-header">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
          <DenseTag variant="neutral" className="shrink-0 text-[10px] uppercase tracking-wide">
            Selected
          </DenseTag>
          <h3 className="satellite-bus-group-title">Selected Environment · {tradeEnv.toUpperCase()}</h3>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {singleEnvScope} · {tradeSingleEnvProbeSource(tradeEnv)}
          </span>
        </div>
      </header>
      <div className="satellite-bus-group-body flex flex-col">
        <OpsSection
          variant="flat"
          title="Data path consumers"
          bodyPadding="none"
          overflow="hidden"
          description="Socket consumers reading the shared bus — REQUIRED must be up; EXPECTED OFF is intentional env policy (never a fault)"
        >
          <ConsumerTable
            rows={viewModel.dataPathConsumers}
            loading={busLoading}
            onInspect={row => openInspect({ kind: 'consumer', row })}
          />
        </OpsSection>
        <TradeDaemonOperatePanel
          tradeEnv={tradeEnv}
          namespace={ns}
          canOperate={canOperate}
          workloads={workloads}
          workloadsLoading={workloadsLoading}
        />
        <OpsSection
          variant="flat"
          title="Runtime consumers"
          bodyPadding="none"
          overflow="hidden"
          description="Monitor consumers — trading daemon / Trade APIs / Celery workers / account sync. Issues here degrade the bus verdict but never mark it unavailable. K8s workload readiness is Evidence and does not affect Bus Health"
        >
          <ConsumerTable
            rows={viewModel.runtimeConsumers}
            loading={busLoading}
            onInspect={row => openInspect({ kind: 'consumer', row })}
          />
        </OpsSection>
      </div>
    </div>
  )
}
