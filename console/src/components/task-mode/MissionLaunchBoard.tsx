import { useState, type ReactNode } from 'react'
import { cn, DenseTag, SegmentControl, StatusLamp } from '@bifrost/ui'
import { Rocket, Satellite, type LucideIcon } from 'lucide-react'
import { OpsSection } from '@/components/layout/OpsSection'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { LaunchGateBar } from '@/components/task-mode/LaunchGateBar'
import { PipelineRunHistoryStrip } from '@/components/task-mode/PipelineRunHistoryStrip'
import {
  isProdReleaseBlocked,
  MissionSharedBusPanel,
  RocketReadinessStrip,
  SatelliteReadinessStrip,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'
import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import type { LaunchCheckpoint, LaunchVerdict } from '@/lib/task-mode/satelliteLaunchVerdict'
import { launchVerdictToSignal, readinessAnchorDomId } from '@/lib/task-mode/satelliteLaunchVerdict'
import { missionStatus } from '@/lib/control-room/missionSignals'
import { setSatelliteBusFocus } from '@/lib/task-mode/readinessChipActions'

export type MissionLaunchBoardProps = {
  onNavigate: (tabId: string) => void
  launchVerdict?: LaunchVerdict
  launchCheckpoints?: LaunchCheckpoint[]
  satelliteLaunchVerdict?: LaunchVerdict
  satelliteLaunchCheckpoints?: LaunchCheckpoint[]
  onDispatchRelease?: () => void
  onDispatchTradeDeploy?: () => void
  releasePending?: boolean
  tradeDeployPending?: boolean
  canDispatchRelease?: boolean
  canDispatchTradeDeploy?: boolean
  releaseDisabledReason?: string
  tradeDeployDisabledReason?: string
  onLaunchAgentFix?: () => void
  onSatelliteLaunchAgentFix?: () => void
  launchAgentFixPending?: boolean
  launchAgentFixActive?: boolean
  launchAgentFixDisabled?: boolean
  launchAgentFixTitle?: string
  satelliteLaunchAgentFixPending?: boolean
  satelliteLaunchAgentFixActive?: boolean
  satelliteLaunchAgentFixDisabled?: boolean
  satelliteLaunchAgentFixTitle?: string
  onOpenAgentDesk?: () => void
  onExpandAgentDock?: () => void
  readinessCanOperate?: boolean
  onAgentFixStg?: () => void
  onAgentFixProd?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
  onAgentTriage?: () => void
  agentTriagePending?: boolean
  agentTriageDisabled?: boolean
  agentTriageTitle?: string
  recentRuns?: DeliveryPipelineRunView[]
  recentRunsLoading?: boolean
  tradeRecentRuns?: DeliveryPipelineRunView[]
  tradeRecentRunsLoading?: boolean
  onOpenPlatformRun: (run: DeliveryPipelineRunView) => void
  onOpenTradeRun: (run: DeliveryPipelineRunView) => void
}

type ReleaseLane = 'vehicle' | 'payload'

function ReleaseLaneOptionLabel({
  active,
  icon: Icon,
  iconClass,
  children,
}: {
  active: boolean
  icon: LucideIcon
  iconClass: string
  children: ReactNode
}) {
  return (
    <span className={cn('release-lane-opt', active && 'release-lane-opt--active')}>
      <Icon
        className={cn('release-lane-opt__icon', iconClass)}
        strokeWidth={active ? 2.35 : 1.85}
        aria-hidden
      />
      <span className="release-lane-opt__text">{children}</span>
    </span>
  )
}

function laneTag(verdict: LaunchVerdict | undefined): {
  variant: 'success' | 'warning' | 'danger' | 'info' | 'neutral'
  label: string
} {
  if (verdict == null) return { variant: 'neutral', label: '—' }
  if (verdict.kind === 'GO') return { variant: 'success', label: 'GO' }
  if (verdict.kind === 'IN_FLIGHT') return { variant: 'info', label: 'IN FLIGHT' }
  return { variant: 'danger', label: 'NO-GO' }
}

/**
 * Mission Launch — two independent release lanes via tab.
 * Vehicle (Rocket / platform) and Payload (Satellite / trade) are separate
 * operational flows; Shared IB bus lives only on the Payload lane (coupling
 * precondition for trade deploy), not as a third full-width chapter.
 */
export function MissionLaunchBoard(props: MissionLaunchBoardProps) {
  const {
    onNavigate,
    launchVerdict,
    launchCheckpoints,
    satelliteLaunchVerdict,
    satelliteLaunchCheckpoints,
    onDispatchRelease,
    onDispatchTradeDeploy,
    releasePending,
    tradeDeployPending,
    canDispatchRelease,
    canDispatchTradeDeploy,
    releaseDisabledReason,
    tradeDeployDisabledReason,
    onLaunchAgentFix,
    onSatelliteLaunchAgentFix,
    launchAgentFixPending,
    launchAgentFixActive,
    launchAgentFixDisabled,
    launchAgentFixTitle,
    satelliteLaunchAgentFixPending,
    satelliteLaunchAgentFixActive,
    satelliteLaunchAgentFixDisabled,
    satelliteLaunchAgentFixTitle,
    onOpenAgentDesk,
    onExpandAgentDock,
    readinessCanOperate,
    onAgentFixStg,
    onAgentFixProd,
    agentFixPending,
    agentFixDisabled,
    agentFixTitle,
    onAgentTriage,
    agentTriagePending,
    agentTriageDisabled,
    agentTriageTitle,
    recentRuns,
    recentRunsLoading,
    tradeRecentRuns,
    tradeRecentRunsLoading,
    onOpenPlatformRun,
    onOpenTradeRun,
  } = props

  const [lane, setLane] = useState<ReleaseLane>('vehicle')
  const { rocketSignal, rocketDetail } = useSatelliteProdReadiness()
  const sharedBlocked = isProdReleaseBlocked(rocketSignal)

  const vehicleTag = laneTag(launchVerdict)
  const payloadTag = laneTag(satelliteLaunchVerdict)

  const vehicleCtaDisabled = !canDispatchRelease || launchVerdict?.kind !== 'GO'
  const payloadCtaDisabled =
    !canDispatchTradeDeploy || satelliteLaunchVerdict?.kind !== 'GO' || sharedBlocked

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-secondary px-3 py-2">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-label)] font-semibold shrink-0">Release lane</span>
          <SegmentControl
            size="sm"
            ariaLabel="Release lane"
            value={lane}
            onChange={v => setLane(v as ReleaseLane)}
            options={[
              {
                value: 'vehicle',
                label: (
                  <ReleaseLaneOptionLabel
                    active={lane === 'vehicle'}
                    icon={Rocket}
                    iconClass="release-lane-opt__icon--vehicle"
                  >
                    Vehicle · Rocket
                  </ReleaseLaneOptionLabel>
                ),
              },
              {
                value: 'payload',
                label: (
                  <ReleaseLaneOptionLabel
                    active={lane === 'payload'}
                    icon={Satellite}
                    iconClass="release-lane-opt__icon--payload"
                  >
                    Payload · Satellite
                  </ReleaseLaneOptionLabel>
                ),
              },
            ]}
          />
        </div>
        <div className="flex flex-wrap items-center gap-2 text-[var(--text-dense-caption)]">
          <span className="inline-flex items-center gap-1">
            <Rocket size={12} className="text-muted-foreground" />
            <StatusLamp value={launchVerdictToSignal(launchVerdict?.kind ?? 'NO_GO')} kind="reach" />
            <DenseTag variant={vehicleTag.variant} className="text-[9px]">
              {vehicleTag.label}
            </DenseTag>
          </span>
          <span className="inline-flex items-center gap-1">
            <Satellite size={12} className="text-muted-foreground" />
            <StatusLamp
              value={launchVerdictToSignal(satelliteLaunchVerdict?.kind ?? 'NO_GO')}
              kind="reach"
            />
            <DenseTag variant={payloadTag.variant} className="text-[9px]">
              {payloadTag.label}
            </DenseTag>
          </span>
          <span className="inline-flex items-center gap-1">
            <StatusLamp value={rocketSignal} kind="reach" />
            <span className="text-muted-foreground">IB bus · {missionStatus(rocketSignal)}</span>
          </span>
        </div>
      </div>

      {lane === 'vehicle' ? (
        <OpsSection
          title="Vehicle · Rocket"
          bodyPadding="compact"
          description="Rocket deliver STG → PROD. Independent of satellite deploy."
          actions={
            <button
              type="button"
              className="text-[var(--text-dense-caption)] text-primary hover:underline"
              onClick={() => onNavigate('platform-release')}
            >
              Launch Rocket →
            </button>
          }
        >
          <div className="flex flex-col gap-1.5">
            {launchVerdict != null && (
              <LaunchGateBar
                verdict={launchVerdict}
                checkpoints={launchCheckpoints ?? []}
                onAgentFix={onLaunchAgentFix}
                agentFixPending={launchAgentFixPending}
                agentFixActive={launchAgentFixActive}
                agentFixDisabled={launchAgentFixDisabled}
                agentFixTitle={launchAgentFixTitle}
                onExpandAgentDock={onExpandAgentDock}
                onOpenAgentDesk={onOpenAgentDesk}
                onLaunch={onDispatchRelease}
                launchLabel="Agent Launch"
                blockedLabel="Vehicle launch blocked"
                launchPending={releasePending}
                launchDisabled={vehicleCtaDisabled}
                launchDisabledReason={releaseDisabledReason}
                onOpenDetail={() => onNavigate('platform-release')}
                detailLabel="Platform →"
                onOpenActiveRun={() => onNavigate('platform-release')}
                openActiveRunLabel="Launch Rocket →"
              />
            )}
            <div className="grid min-w-0 grid-cols-1 gap-2 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)] lg:items-start">
              <OpsSection
                title="Readiness"
                bodyPadding="compact"
                className="h-full min-w-0"
              >
                <RocketReadinessStrip
                  onNavigate={onNavigate}
                  compact
                  summaryColumn
                  suppressProdBlockedBanner
                />
              </OpsSection>
              <OpsSection
                id={readinessAnchorDomId('pipeline')}
                title="Recent platform launches"
                bodyPadding="compact"
                className="scroll-mt-2 h-full min-w-0"
              >
                <PipelineRunHistoryStrip
                  runs={recentRuns}
                  isLoading={recentRunsLoading}
                  compact
                  embedded
                  linkLabel="Launch Rocket →"
                  onOpenFullHistory={() => onNavigate('platform-release')}
                  onOpenRun={onOpenPlatformRun}
                />
              </OpsSection>
            </div>
          </div>
        </OpsSection>
      ) : (
        <OpsSection
          title="Payload · Satellite"
          bodyPadding="compact"
          description="Satellite deliver STG → PROD. Requires shared IB bus when deploying live sockets."
          actions={
            <button
              type="button"
              className="text-[var(--text-dense-caption)] text-primary hover:underline"
              onClick={() => onNavigate('trade-release')}
            >
              Deploy Satellite →
            </button>
          }
        >
          <div className="flex flex-col gap-1.5">
            {satelliteLaunchVerdict != null && (
              <LaunchGateBar
                verdict={satelliteLaunchVerdict}
                checkpoints={satelliteLaunchCheckpoints ?? []}
                onAgentFix={onSatelliteLaunchAgentFix}
                agentFixPending={satelliteLaunchAgentFixPending}
                agentFixActive={satelliteLaunchAgentFixActive}
                agentFixDisabled={satelliteLaunchAgentFixDisabled}
                agentFixTitle={satelliteLaunchAgentFixTitle}
                onExpandAgentDock={onExpandAgentDock}
                onOpenAgentDesk={onOpenAgentDesk}
                onLaunch={onDispatchTradeDeploy}
                launchLabel="Agent Deploy"
                blockedLabel="Payload deploy blocked"
                launchPending={tradeDeployPending}
                launchDisabled={payloadCtaDisabled}
                launchDisabledReason={
                  sharedBlocked && satelliteLaunchVerdict.kind === 'GO'
                    ? `IB bus blocked — ${rocketDetail}`
                    : tradeDeployDisabledReason
                }
                onOpenDetail={() => onNavigate('trade-release')}
                detailLabel="Satellite →"
                onOpenActiveRun={() => onNavigate('trade-release')}
                openActiveRunLabel="Deploy Satellite →"
              />
            )}
            <div className="grid min-w-0 grid-cols-1 gap-2 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)] lg:items-start">
              <div className="flex min-w-0 flex-col gap-1.5">
                <OpsSection title="Readiness" bodyPadding="compact" className="min-w-0">
                  <SatelliteReadinessStrip
                    onNavigate={onNavigate}
                    compact
                    summaryColumn
                    suppressProdBlockedBanner
                    omitSharedBus
                    canOperate={readinessCanOperate}
                    onAgentFixStg={onAgentFixStg}
                    onAgentFixProd={onAgentFixProd}
                    agentFixPending={agentFixPending}
                    agentFixDisabled={agentFixDisabled}
                    agentFixTitle={agentFixTitle}
                  />
                </OpsSection>
                <OpsSection
                  title="IB bus (coupling)"
                  bodyPadding="compact"
                  description="Precondition for trade sockets — not a separate launch product."
                  className="border-border/80"
                  actions={
                    <div className="flex flex-wrap items-center gap-2">
                      <button
                        type="button"
                        className="text-[var(--text-dense-caption)] text-primary hover:underline"
                        onClick={() => {
                          setSatelliteBusFocus('rocket')
                          onNavigate('satellite-bus')
                        }}
                      >
                        Bus Status →
                      </button>
                      {onAgentTriage != null && (
                        <AgentTriggerButton
                          label="Agent Triage"
                          size="xs"
                          pending={agentTriagePending}
                          disabled={agentTriageDisabled}
                          title={
                            agentTriageTitle ??
                            'Cross-check Socket matrix vs Rocket IB gateway (D10 safe)'
                          }
                          onClick={onAgentTriage}
                        />
                      )}
                    </div>
                  }
                >
                  <MissionSharedBusPanel
                    compact
                    onNavigate={onNavigate}
                    canOperate={readinessCanOperate}
                  />
                </OpsSection>
              </div>
              <OpsSection
                id={readinessAnchorDomId('pipeline')}
                title="Recent trade launches"
                bodyPadding="compact"
                className="scroll-mt-2 h-full min-w-0"
              >
                <PipelineRunHistoryStrip
                  runs={tradeRecentRuns}
                  isLoading={tradeRecentRunsLoading}
                  compact
                  embedded
                  linkLabel="Deploy Satellite →"
                  onOpenFullHistory={() => onNavigate('trade-release')}
                  onOpenRun={onOpenTradeRun}
                />
              </OpsSection>
            </div>
          </div>
        </OpsSection>
      )}
    </div>
  )
}
