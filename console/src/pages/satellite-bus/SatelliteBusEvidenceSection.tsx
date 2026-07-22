import type { Ref } from 'react'
import type { Target } from '@/api/matrixTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  CriticalProcessesTable,
  MonitorKvTable,
  SecondaryGroup,
  TradeApiReachTable,
} from '@/pages/satellite-bus/satelliteBusTableParts'
import type { CriticalProcessRow, MonitorKvRow, TradeEnv } from '@/pages/satellite-bus/useSatelliteBusQueries'

export function SatelliteBusEvidenceSection({
  tradeEnv,
  ns,
  busLoading,
  evidenceOpen,
  setEvidenceOpen,
  evidenceSectionRef,
  highlightSection,
  daemonRows,
  celeryRows,
  accountSyncRows,
  opsRows,
  tradeApiTargetRows,
  matrixLoading,
  criticalProcesses,
  workloadsLoading,
}: {
  tradeEnv: TradeEnv
  ns: string
  busLoading: boolean
  evidenceOpen: boolean
  setEvidenceOpen: (open: boolean) => void
  evidenceSectionRef: Ref<HTMLDetailsElement>
  highlightSection: string | null
  daemonRows: MonitorKvRow[]
  celeryRows: MonitorKvRow[]
  accountSyncRows: MonitorKvRow[]
  opsRows: MonitorKvRow[]
  tradeApiTargetRows: Target[]
  matrixLoading: boolean
  criticalProcesses: CriticalProcessRow[]
  workloadsLoading: boolean
}) {
  return (
    <SecondaryGroup
      title="Evidence"
      description={`Raw monitor FSM · Trade API reachability · K8s workloads for ${ns}`}
      scope="trade-single-env"
      open={evidenceOpen}
      onOpenChange={setEvidenceOpen}
      sectionRef={evidenceSectionRef}
      highlight={highlightSection === 'trade-apis' || highlightSection === 'workers'}
    >
      <OpsSection
        variant="flat"
        title="Raw monitor FSM"
        description="Strict trading-arm semantics — may differ from Bus health when observe / pause / expected-off is healthy"
        bodyPadding="none"
        overflow="hidden"
      >
        <MonitorKvTable rows={daemonRows} loading={busLoading} />
      </OpsSection>
      <div className="grid divide-x divide-[var(--border)] border-t border-[var(--border)] sm:grid-cols-2 xl:grid-cols-3">
        <OpsSection variant="flat" title="Celery" bodyPadding="none" overflow="hidden">
          <MonitorKvTable rows={celeryRows} loading={busLoading} />
        </OpsSection>
        <OpsSection variant="flat" title="Account sync" bodyPadding="none" overflow="hidden">
          <MonitorKvTable rows={accountSyncRows} loading={busLoading} />
        </OpsSection>
        <OpsSection variant="flat" title="Ops executor" bodyPadding="none" overflow="hidden">
          <MonitorKvTable rows={opsRows} loading={busLoading} />
        </OpsSection>
      </div>
      <OpsSection
        variant="flat"
        title="Trade API reachability"
        bodyPadding="none"
        overflow="hidden"
        description={`Matrix L0 HTTP probes for ${tradeEnv.toUpperCase()} · full detail on API & Auth Probes`}
      >
        <TradeApiReachTable targets={tradeApiTargetRows} loading={matrixLoading} />
      </OpsSection>
      <OpsSection
        variant="flat"
        title="Critical processes"
        bodyPadding="none"
        overflow="hidden"
        description={`K8s workload readiness in ${ns} — evidence only, not part of the Bus Health verdict`}
      >
        <CriticalProcessesTable rows={criticalProcesses} loading={workloadsLoading} />
      </OpsSection>
    </SecondaryGroup>
  )
}
