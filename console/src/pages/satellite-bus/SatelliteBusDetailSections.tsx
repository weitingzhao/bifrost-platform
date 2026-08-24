import type { Ref } from 'react'
import type { Target } from '@/api/matrixTypes'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import { OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import type { PayloadReadinessRow } from '@/lib/control-room/payloadReadiness'
import type { SatelliteBusViewModel } from '@/lib/satellite-bus/satelliteBusViewModel'
import type { InspectTarget } from '@/pages/satellite-bus/inspectTypes'
import { SatelliteBusCrossEnvSection } from '@/pages/satellite-bus/SatelliteBusCrossEnvSection'
import { SatelliteBusEvidenceSection } from '@/pages/satellite-bus/SatelliteBusEvidenceSection'
import { SatelliteBusSelectedEnvSection } from '@/pages/satellite-bus/SatelliteBusSelectedEnvSection'
import { SatelliteBusSharedRocketSection } from '@/pages/satellite-bus/SatelliteBusSharedRocketSection'
import {
  AttentionIssueRow,
  BusPageBand,
  BusPathNodeCard,
} from '@/pages/satellite-bus/satelliteBusTableParts'
import type { CriticalProcessRow, MonitorKvRow, TradeEnv, useSatelliteBusQueries } from '@/pages/satellite-bus/useSatelliteBusQueries'

type BusQueries = ReturnType<typeof useSatelliteBusQueries>

/** Body mode — View Segment: Operate | Shared | Compare. */
export type BusBodyMode = 'operate' | 'shared' | 'compare'

export type SatelliteBusDetailSectionsProps = {
  tradeEnv: TradeEnv
  ns: string
  viewModel: SatelliteBusViewModel
  busLoading: boolean
  bodyMode: BusBodyMode
  highlightSection: string | null
  /** K8s workload name from Activity (account-sync / daemon). */
  highlightWorkload?: string | null
  /** Runtime Consumers row id to ring (account-sync / trading_engine). */
  highlightRuntimeRowId?: string | null
  operateSectionRef?: Ref<HTMLDivElement>
  issuesSectionRef: Ref<HTMLElement>
  selectedSectionRef: Ref<HTMLDivElement>
  sharedSectionRef: Ref<HTMLDetailsElement>
  otherEnvsSectionRef: Ref<HTMLDetailsElement>
  evidenceSectionRef: Ref<HTMLDetailsElement>
  sharedOpen: boolean
  setSharedOpen: (open: boolean) => void
  otherEnvsOpen: boolean
  setOtherEnvsOpen: (open: boolean) => void
  evidenceOpen: boolean
  setEvidenceOpen: (open: boolean) => void
  openInspect: (target: InspectTarget) => void
  payloadRows: PayloadReadinessRow[]
  socketHealthMatrix: BusQueries['socketHealthMatrix']
  serviceReadinessQuery: BusQueries['serviceReadinessQuery']
  metricsQuery: BusQueries['metricsQuery']
  observabilityQuery: BusQueries['observabilityQuery']
  matrixQuery: BusQueries['matrixQuery']
  workloadsQuery: BusQueries['workloadsQuery']
  pluginWorkloadsQuery: BusQueries['pluginWorkloadsQuery']
  canOperate: boolean
  busDeep: SatelliteBusDeepResponse | undefined
  tradeApiTargetRows: Target[]
  criticalProcesses: CriticalProcessRow[]
  daemonRows: MonitorKvRow[]
  accountSyncRows: MonitorKvRow[]
  opsRows: MonitorKvRow[]
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  onOpenObservability?: () => void
  onOpenPluginGallery?: () => void
}

/** Scrollable detail body — Operate (+ Evidence), Shared, or Compare. */
export function SatelliteBusDetailSections({
  tradeEnv,
  ns,
  viewModel,
  busLoading,
  bodyMode,
  highlightSection,
  highlightWorkload = null,
  highlightRuntimeRowId = null,
  operateSectionRef,
  issuesSectionRef,
  selectedSectionRef,
  sharedSectionRef,
  otherEnvsSectionRef,
  evidenceSectionRef,
  sharedOpen,
  setSharedOpen,
  otherEnvsOpen,
  setOtherEnvsOpen,
  evidenceOpen,
  setEvidenceOpen,
  openInspect,
  payloadRows,
  socketHealthMatrix,
  serviceReadinessQuery,
  metricsQuery,
  observabilityQuery,
  matrixQuery,
  workloadsQuery,
  pluginWorkloadsQuery,
  canOperate,
  busDeep,
  tradeApiTargetRows,
  criticalProcesses,
  daemonRows,
  accountSyncRows,
  opsRows,
  onOpenCluster,
  onOpenTelemetry,
  onOpenObservability,
  onOpenPluginGallery,
}: SatelliteBusDetailSectionsProps) {
  const selectedIssues = viewModel.attention
  const crossEnvIssues = viewModel.crossEnvIssues

  return (
    <div className="flex flex-col gap-3 pb-[max(1.5rem,var(--agent-dock-reserve,2.75rem))]">
      {bodyMode === 'operate' && (
        <BusPageBand
          step="Operate"
          title={`${ns} work surface`}
          description="Path, issues, consumers, daemon actuate — summarized in the BUS HEALTH verdict"
        >
          <section className="page-section panel-elevated px-2.5 py-1.5">
            <div className="mb-1 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
              <OpsSubsectionTitle className="m-0">Data path</OpsSubsectionTitle>
              <span className="text-[var(--text-dense-caption)] text-muted-foreground">
                Hops that feed this verdict: Platform IB Gateway → redis-ib → socket consumers → {ns}
              </span>
            </div>
            <div className="flex flex-wrap items-stretch gap-1.5">
              {viewModel.path.map((node, idx) => (
                <div key={node.id} className="flex min-w-0 flex-1 items-center gap-1.5">
                  <BusPathNodeCard node={node} onInspect={() => openInspect({ kind: 'node', node })} />
                  {idx < viewModel.path.length - 1 && (
                    <span aria-hidden className="shrink-0 text-muted-foreground">
                      →
                    </span>
                  )}
                </div>
              ))}
            </div>
          </section>

          <section
            id="satellite-bus-issues"
            ref={issuesSectionRef}
            className={
              highlightSection === 'issues'
                ? 'page-section panel-elevated overflow-hidden ring-1 ring-[var(--ring)] ring-offset-1 ring-offset-[var(--background)]'
                : 'page-section panel-elevated overflow-hidden'
            }
          >
            <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 px-2.5 pt-1.5">
              <OpsSubsectionTitle className="m-0">Issues requiring attention</OpsSubsectionTitle>
              <span className="text-[var(--text-dense-caption)] text-muted-foreground">
                Selected env + shared deps first · cross-env rows are informational only
              </span>
            </div>
            {busLoading ? (
              <p className="m-0 px-3 py-2 text-[var(--text-dense-caption)] text-muted-foreground">
                Probing…
              </p>
            ) : selectedIssues.length === 0 && crossEnvIssues.length === 0 ? (
              <p className="m-0 flex items-center gap-1.5 px-3 py-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
                <StatusLamp value="ok" kind="reach" />
                No issues requiring attention — {tradeEnv.toUpperCase()} bus and shared dependencies
                are clear.
              </p>
            ) : (
              <>
                {selectedIssues.length > 0 && (
                  <ul className="m-0 flex list-none flex-col divide-y divide-[var(--border)] p-0">
                    {selectedIssues.map(issue => (
                      <AttentionIssueRow
                        key={issue.id}
                        issue={issue}
                        onInspect={() => openInspect({ kind: 'issue', issue })}
                      />
                    ))}
                  </ul>
                )}
                {crossEnvIssues.length > 0 && (
                  <>
                    <p className="m-0 border-t border-[var(--border)] px-3 py-1 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                      Cross-env attention — does not affect the {tradeEnv.toUpperCase()} verdict · see
                      View · Compare
                    </p>
                    <ul className="m-0 flex list-none flex-col divide-y divide-[var(--border)] p-0">
                      {crossEnvIssues.map(issue => (
                        <AttentionIssueRow
                          key={issue.id}
                          issue={issue}
                          onInspect={() => openInspect({ kind: 'issue', issue })}
                        />
                      ))}
                    </ul>
                  </>
                )}
              </>
            )}
          </section>

          <SatelliteBusSelectedEnvSection
            tradeEnv={tradeEnv}
            ns={ns}
            viewModel={viewModel}
            busLoading={busLoading}
            highlightSection={highlightSection}
            highlightWorkload={highlightWorkload}
            highlightRuntimeRowId={highlightRuntimeRowId}
            operateSectionRef={operateSectionRef}
            selectedSectionRef={selectedSectionRef}
            openInspect={openInspect}
            canOperate={canOperate}
            workloads={workloadsQuery.data?.workloads ?? []}
            workloadsLoading={workloadsQuery.isLoading}
          />

          <SatelliteBusEvidenceSection
            tradeEnv={tradeEnv}
            ns={ns}
            busDeep={busDeep}
            busLoading={busLoading}
            evidenceOpen={evidenceOpen}
            setEvidenceOpen={setEvidenceOpen}
            evidenceSectionRef={evidenceSectionRef}
            highlightSection={highlightSection}
            daemonRows={daemonRows}
            accountSyncRows={accountSyncRows}
            opsRows={opsRows}
            tradeApiTargetRows={tradeApiTargetRows}
            matrixLoading={matrixQuery.isLoading}
            criticalProcesses={criticalProcesses}
            workloadsLoading={workloadsQuery.isLoading || pluginWorkloadsQuery.isLoading}
          />
        </BusPageBand>
      )}

      {bodyMode === 'shared' && (
        <BusPageBand
          step="Shared"
          title="Rocket + Ground"
          description="Shared IB socket bus and cluster readiness — same for every Trade NS (not driven by Trade NS selection)"
        >
          <SatelliteBusSharedRocketSection
            rocketRow={socketHealthMatrix.rocket}
            payloadRows={payloadRows}
            serviceReadinessQuery={serviceReadinessQuery}
            metricsQuery={metricsQuery}
            observabilityQuery={observabilityQuery}
            sharedOpen={sharedOpen}
            setSharedOpen={setSharedOpen}
            sharedSectionRef={sharedSectionRef}
            highlightSection={highlightSection}
            onOpenCluster={onOpenCluster}
            onOpenTelemetry={onOpenTelemetry}
            onOpenObservability={onOpenObservability}
          />
        </BusPageBand>
      )}

      {bodyMode === 'compare' && (
        <BusPageBand
          step="Compare"
          title="Socket matrix · all envs"
          description="Cross-env DRIFT does not change BUS HEALTH for the selected Trade NS"
        >
          <SatelliteBusCrossEnvSection
            tradeEnv={tradeEnv}
            tradeRows={socketHealthMatrix.tradeRows}
            otherEnvsOpen={otherEnvsOpen}
            setOtherEnvsOpen={setOtherEnvsOpen}
            otherEnvsSectionRef={otherEnvsSectionRef}
            highlightSection={highlightSection}
            onOpenPluginGallery={onOpenPluginGallery}
          />
        </BusPageBand>
      )}
    </div>
  )
}
