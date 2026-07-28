import { useState, type ReactNode } from 'react'
import { cn, DenseTag, SegmentControl, StatusLamp } from '@bifrost/ui'
import { Database, Plug, Rocket, Satellite, type LucideIcon } from 'lucide-react'
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
import { evidenceSummaryLine, type PluginLaunchEvidence } from '@/lib/delivery/pluginLaunchEvidence'
import { writeCategoryToUrl } from '@/lib/cluster/clusterCategories'
import { DataFreshnessPanel } from '@/components/cluster/DataFreshnessPanel'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

export type MissionLaunchBoardProps = {
  onNavigate: (tabId: string) => void
  launchVerdict?: LaunchVerdict
  launchCheckpoints?: LaunchCheckpoint[]
  satelliteLaunchVerdict?: LaunchVerdict
  satelliteLaunchCheckpoints?: LaunchCheckpoint[]
  pluginLaunchVerdict?: LaunchVerdict
  pluginLaunchCheckpoints?: LaunchCheckpoint[]
  pluginEvidence?: PluginLaunchEvidence
  onDispatchRelease?: () => void
  onDispatchTradeDeploy?: () => void
  onDispatchPluginLaunch?: () => void
  releasePending?: boolean
  tradeDeployPending?: boolean
  pluginLaunchPending?: boolean
  canDispatchRelease?: boolean
  canDispatchTradeDeploy?: boolean
  canDispatchPluginLaunch?: boolean
  releaseDisabledReason?: string
  tradeDeployDisabledReason?: string
  pluginLaunchDisabledReason?: string
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
  /** Primary Launch/Deploy lives in the Command row — hide duplicate gate-bar CTA. */
  hidePrimaryLaunch?: boolean
}

type CommandLane = 'vehicle' | 'payload' | 'plugin' | 'data-maintenance'

function CommandLaneOptionLabel({
  active,
  icon: Icon,
  iconClass,
  verdict,
  children,
}: {
  active: boolean
  icon: LucideIcon
  iconClass: string
  verdict?: LaunchVerdict
  children: ReactNode
}) {
  const tag = laneTag(verdict)

  return (
    <span className={cn('release-lane-opt', active && 'release-lane-opt--active')}>
      <Icon
        className={cn('release-lane-opt__icon', iconClass)}
        strokeWidth={active ? 2.35 : 1.85}
        aria-hidden
      />
      <span className="release-lane-opt__text">{children}</span>
      <span className="release-lane-opt__verdict">
        <StatusLamp value={launchVerdictToSignal(verdict?.kind ?? 'NO_GO')} kind="reach" />
        <DenseTag variant={tag.variant} className="text-[9px]">
          {tag.label}
        </DenseTag>
      </span>
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
 * Mission Launch — platform, trade, plugin, and data-maintenance command lanes.
 * Vehicle (Rocket / platform), Payload (Satellite / trade), Plugin (IB Gateway publish).
 * Shared IB bus lives only on the Payload lane (coupling precondition for trade deploy).
 */
export function MissionLaunchBoard(props: MissionLaunchBoardProps) {
  const {
    onNavigate,
    launchVerdict,
    launchCheckpoints,
    satelliteLaunchVerdict,
    satelliteLaunchCheckpoints,
    pluginLaunchVerdict,
    pluginLaunchCheckpoints,
    pluginEvidence,
    onDispatchRelease,
    onDispatchTradeDeploy,
    onDispatchPluginLaunch,
    releasePending,
    tradeDeployPending,
    pluginLaunchPending,
    canDispatchRelease,
    canDispatchTradeDeploy,
    canDispatchPluginLaunch,
    releaseDisabledReason,
    tradeDeployDisabledReason,
    pluginLaunchDisabledReason,
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
    hidePrimaryLaunch = false,
  } = props

  const [lane, setLane] = useState<CommandLane>('vehicle')
  const { canAdmin } = usePlatformAuth()
  const { rocketSignal, rocketDetail } = useSatelliteProdReadiness()
  const sharedBlocked = isProdReleaseBlocked(rocketSignal)
  const ibBusStatus = missionStatus(rocketSignal)
  const ibBusStatusClass =
    ibBusStatus === 'NOMINAL'
      ? 'text-muted-foreground hover:text-primary'
      : ibBusStatus === 'CRITICAL'
        ? 'font-semibold text-danger hover:text-danger/80'
        : 'font-semibold text-warning hover:text-warning/80'
  const openRocketBus = () => {
    setSatelliteBusFocus('rocket')
    onNavigate('satellite-bus')
  }

  const vehicleCtaDisabled = !canDispatchRelease || launchVerdict?.kind !== 'GO'
  const payloadCtaDisabled =
    !canDispatchTradeDeploy || satelliteLaunchVerdict?.kind !== 'GO' || sharedBlocked
  const pluginCtaDisabled = !canDispatchPluginLaunch || pluginLaunchVerdict?.kind !== 'GO'
  const vehicleCtaTitle =
    releaseDisabledReason ??
    (vehicleCtaDisabled
      ? (launchVerdict?.disabledReason ?? 'Vehicle launch unavailable')
      : 'Launch platform release agent')
  const payloadCtaTitle =
    sharedBlocked && satelliteLaunchVerdict?.kind === 'GO'
      ? `IB bus blocked — ${rocketDetail}`
      : (tradeDeployDisabledReason ??
        (payloadCtaDisabled
          ? (satelliteLaunchVerdict?.disabledReason ?? 'Payload deploy unavailable')
          : 'Deploy Trade satellite agent'))
  const pluginCtaTitle =
    pluginLaunchDisabledReason ??
    (pluginCtaDisabled
      ? (pluginLaunchVerdict?.disabledReason ?? 'Plugin launch unavailable')
      : 'Launch plugin publish agent')
  const commandAction =
    lane === 'vehicle' ? (
      <AgentTriggerButton
        label="Launch"
        size="xs"
        pending={releasePending}
        disabled={vehicleCtaDisabled}
        title={vehicleCtaTitle}
        onClick={() => onDispatchRelease?.()}
      />
    ) : lane === 'payload' ? (
      <AgentTriggerButton
        label="Deploy"
        size="xs"
        pending={tradeDeployPending}
        disabled={payloadCtaDisabled}
        title={payloadCtaTitle}
        onClick={() => onDispatchTradeDeploy?.()}
      />
    ) : lane === 'plugin' ? (
      <AgentTriggerButton
        label="Launch"
        size="xs"
        pending={pluginLaunchPending}
        disabled={pluginCtaDisabled}
        title={pluginCtaTitle}
        onClick={() => onDispatchPluginLaunch?.()}
      />
    ) : null

  return (
    <div id="task-cc-launch-board" className="flex scroll-mt-2 flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-secondary px-3 py-2">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-label)] font-semibold shrink-0">Command</span>
          <SegmentControl
            size="sm"
            ariaLabel="Release lanes"
            value={lane}
            onChange={v => setLane(v as CommandLane)}
            options={[
              {
                value: 'vehicle',
                label: (
                  <CommandLaneOptionLabel
                    active={lane === 'vehicle'}
                    icon={Rocket}
                    iconClass="release-lane-opt__icon--vehicle"
                    verdict={launchVerdict}
                  >
                    Vehicle · Rocket
                  </CommandLaneOptionLabel>
                ),
              },
              {
                value: 'payload',
                label: (
                  <CommandLaneOptionLabel
                    active={lane === 'payload'}
                    icon={Satellite}
                    iconClass="release-lane-opt__icon--payload"
                    verdict={satelliteLaunchVerdict}
                  >
                    Payload · Satellite
                  </CommandLaneOptionLabel>
                ),
              },
              {
                value: 'plugin',
                label: (
                  <CommandLaneOptionLabel
                    active={lane === 'plugin'}
                    icon={Plug}
                    iconClass="release-lane-opt__icon--plugin"
                    verdict={pluginLaunchVerdict}
                  >
                    Plugin
                  </CommandLaneOptionLabel>
                ),
              },
            ]}
          />
          <span className="h-5 border-l border-border" aria-hidden />
          <SegmentControl
            size="sm"
            ariaLabel="Data maintenance"
            value={lane}
            onChange={v => setLane(v as CommandLane)}
            options={[
              {
                value: 'data-maintenance',
                label: (
                  <CommandLaneOptionLabel
                    active={lane === 'data-maintenance'}
                    icon={Database}
                    iconClass="release-lane-opt__icon--data-maintenance"
                  >
                    Data · Maintenance
                  </CommandLaneOptionLabel>
                ),
              },
            ]}
          />
        </div>
        <div className="flex shrink-0 items-center gap-2 text-[var(--text-dense-caption)]">
          {commandAction}
          {commandAction != null && <span className="h-5 border-l border-border" aria-hidden />}
          <button
            type="button"
            className={cn(
              'inline-flex items-center gap-1 rounded px-1 py-0.5 transition-colors hover:bg-primary/10 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
              ibBusStatusClass,
            )}
            title={`${rocketDetail} — open Rocket IB bus diagnostics`}
            onClick={openRocketBus}
          >
            <StatusLamp value={rocketSignal} kind="reach" />
            <span>IB bus · {ibBusStatus}</span>
            {ibBusStatus !== 'NOMINAL' && <span aria-hidden>Fix →</span>}
          </button>
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
                hidePrimaryLaunch={hidePrimaryLaunch}
              />
            )}
            <div className="grid min-w-0 grid-cols-1 gap-2 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)] lg:items-start">
              <OpsSection title="Readiness" bodyPadding="compact" className="h-full min-w-0">
                <RocketReadinessStrip
                  onNavigate={onNavigate}
                  compact
                  summaryColumn
                  suppressProdBlockedBanner
                  canOperate={readinessCanOperate}
                  onAgentFix={onLaunchAgentFix}
                  agentFixPending={launchAgentFixPending}
                  agentFixDisabled={launchAgentFixDisabled}
                  agentFixTitle={launchAgentFixTitle}
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
      ) : lane === 'payload' ? (
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
                hidePrimaryLaunch={hidePrimaryLaunch}
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
      ) : lane === 'plugin' ? (
        <OpsSection
          title="Plugin · IB Gateway"
          bodyPadding="compact"
          description="Publish bifrost-platform-plugin via make install — not Tekton. Gallery observes; this lane publishes."
          actions={
            <button
              type="button"
              className="text-[var(--text-dense-caption)] text-primary hover:underline"
              onClick={() => onNavigate('plugin-release')}
            >
              Launch Plugin →
            </button>
          }
        >
          <div className="flex flex-col gap-1.5">
            {pluginLaunchVerdict != null && (
              <LaunchGateBar
                verdict={pluginLaunchVerdict}
                checkpoints={pluginLaunchCheckpoints ?? []}
                onExpandAgentDock={onExpandAgentDock}
                onOpenAgentDesk={onOpenAgentDesk}
                onLaunch={onDispatchPluginLaunch}
                launchLabel="AI Launch Plugin"
                blockedLabel="Plugin launch blocked"
                launchPending={pluginLaunchPending}
                launchDisabled={pluginCtaDisabled}
                launchDisabledReason={pluginLaunchDisabledReason}
                onOpenDetail={() => onNavigate('plugin-release')}
                detailLabel="Plugin →"
                onOpenActiveRun={() => onNavigate('plugin-release')}
                openActiveRunLabel="Launch Plugin →"
                hidePrimaryLaunch={hidePrimaryLaunch}
              />
            )}
            <OpsSection title="Last install / verify evidence" bodyPadding="compact">
              <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
                {evidenceSummaryLine(pluginEvidence ?? {})}
              </p>
              <div className="mt-1.5 flex flex-wrap gap-2">
                <button
                  type="button"
                  className="text-[var(--text-dense-caption)] text-primary hover:underline"
                  onClick={() => onNavigate('plugin-release')}
                >
                  Open full steps →
                </button>
                <button
                  type="button"
                  className="text-[var(--text-dense-caption)] text-muted-foreground hover:underline"
                  onClick={() => onNavigate('plugin-gallery')}
                >
                  Gallery (observe) →
                </button>
              </div>
            </OpsSection>
          </div>
        </OpsSection>
      ) : lane === 'data-maintenance' ? (
        <DataFreshnessPanel
          canAdmin={canAdmin}
          onOpenFullPostgres={() => {
            writeCategoryToUrl('database')
            onNavigate('cluster')
          }}
        />
      ) : null}
    </div>
  )
}
