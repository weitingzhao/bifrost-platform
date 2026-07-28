import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Button, DenseTag } from '@bifrost/ui'
import type { ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/deliveryTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { OpsTaskStrips, OpsTaskSummaryRow } from '@/components/task-mode/OpsTaskStrips'
import { DevModeStrips } from '@/components/task-mode/DevModeController'
import { TaskPhaseProgress } from '@/components/task-mode/TaskPhaseProgress'
import type { CommandLane } from '@/components/task-mode/MissionLaunchBoard'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { TaskCCVerdict } from '@/components/task-mode/tcc/TaskCCVerdict'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import type { FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { pickFailingFixSignal } from '@/lib/agent/prodEnvironmentFixPrompt'
import { fixScopeAgentTitle } from '@/lib/agent/readinessFixDispatch'
import { launchVerdictToSignal } from '@/lib/task-mode/satelliteLaunchVerdict'
import type { LaunchCheckpoint, LaunchVerdict } from '@/lib/task-mode/satelliteLaunchVerdict'
import { missionStatus } from '@/lib/control-room/missionSignals'
import type { PluginLaunchEvidence } from '@/lib/delivery/pluginLaunchEvidence'
import type { TaskPhaseFixAction, TaskPhaseHint } from '@/lib/task-mode/taskPhaseDiagnostics'
import type { TaskModeDef, TaskPhaseDef, TaskPhaseStatus } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import type { UseDevProgramInstanceResult } from '@/hooks/useDevProgramInstance'
import type { InlineBriefingPackResult } from '@/hooks/useInlineBriefingPack'
import type { useQuery } from '@tanstack/react-query'
import type { fetchDevAgentStatus } from '@/api/devAgent'
import type { useTaskControlQueries } from '@/components/task-mode/tcc/useTaskControlQueries'
import type { useChecklistItemFix } from '@/components/task-mode/tcc/useChecklistItemFix'
import type { useMissionLaunchFixAgents } from '@/components/task-mode/useMissionLaunchFixAgents'
import type {
  useRocketProdReadiness,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'

type VerdictLamp = 'ok' | 'degraded' | 'fail' | 'unknown'
type SemanticTextClass = 'text-success' | 'text-warning' | 'text-danger' | 'text-muted-foreground'

const LAMP_RANK: Record<VerdictLamp, number> = {
  ok: 0,
  unknown: 1,
  degraded: 2,
  fail: 3,
}

function worseLamp(a: VerdictLamp, b: VerdictLamp): VerdictLamp {
  return LAMP_RANK[a] >= LAMP_RANK[b] ? a : b
}

function signalTextClass(signal: VerdictLamp): SemanticTextClass {
  if (signal === 'ok') return 'text-success'
  if (signal === 'degraded') return 'text-warning'
  if (signal === 'fail') return 'text-danger'
  return 'text-muted-foreground'
}

function checklistTextClass(
  phases: TaskPhaseDef[],
  statuses: Record<string, TaskPhaseStatus>,
  doneCount: number,
): SemanticTextClass {
  // `blocked` is the current task-playbook failure equivalent.
  if (phases.some(phase => statuses[phase.id] === 'blocked')) return 'text-danger'
  if (phases.length > 0 && doneCount === phases.length) return 'text-success'
  return 'text-warning'
}

function launchVerdictTextClass(kind: LaunchVerdict['kind']): SemanticTextClass {
  if (kind === 'GO') return 'text-success'
  if (kind === 'IN_FLIGHT') return 'text-warning'
  return 'text-danger'
}

export type TaskControlCenterViewProps = {
  mode: TaskModeDef
  isMissionLaunch: boolean
  isDailyOps: boolean
  isDevLoop: boolean
  canOperate: boolean
  loopLabel: string
  headerDescription: string
  viewerEnv: FleetViewerEnv
  viewerEnvLoading: boolean
  showLaunchPad: boolean
  phases: TaskPhaseDef[]
  statuses: Record<string, TaskPhaseStatus>
  phaseHints: Record<string, TaskPhaseHint>
  doneCount: number
  phaseDefaultOpen: boolean
  rocketProd: ReturnType<typeof useRocketProdReadiness>
  satelliteProd: ReturnType<typeof useSatelliteProdReadiness>
  onOpenPhasePage: (phase: TaskPhaseDef) => void
  onPhaseFixAction: (action: TaskPhaseFixAction, phase: TaskPhaseDef) => void
  devProgram: UseDevProgramInstanceResult
  resolvedProgramId?: string
  inlineBriefingPack: InlineBriefingPackResult
  onNavigate: (tabId: string) => void
  onOpenBriefing?: (opts?: BriefingUrlState) => void
  handleBriefingOpened: () => void
  devAgentQ: ReturnType<typeof useQuery<Awaited<ReturnType<typeof fetchDevAgentStatus>>>>
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  stgSmoke?: StgSmokeResponse
  stgGate?: ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
  onOpenPromote?: () => void
  onOpenDelivery?: () => void
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
  onExpandAgentDock?: () => void
  ambientJobId?: string | null
  ambientJobScope?: string | null
  onStartAgentJob?: (job: { id: string; scope: string; label: string }) => void
  q: ReturnType<typeof useTaskControlQueries>
  agents: ReturnType<typeof useMissionLaunchFixAgents>
  fix: ReturnType<typeof useChecklistItemFix>
  dispatchReleaseAgent: () => void
  dispatchTradeDeployAgent: () => void
  dispatchPluginLaunchAgent: () => void
  releaseDispatchAllowed: boolean
  tradeDeployDispatchAllowed: boolean
  pluginLaunchDispatchAllowed: boolean
  releaseDisabledReason?: string
  tradeDeployDisabledReason?: string
  pluginLaunchDisabledReason?: string
  pluginLaunchVerdict: LaunchVerdict
  pluginLaunchCheckpoints: LaunchCheckpoint[]
  pluginEvidence: PluginLaunchEvidence
  missionOverall: VerdictLamp
  /** Shown when phase Agent Fix is not supported in the current mode. */
  phaseFixUnavailableHint?: string | null
}

/**
 * Pure layout assembly for Task Control Center — props in, no data fetching.
 */
export function TaskControlCenterView(props: TaskControlCenterViewProps) {
  const {
    mode,
    isMissionLaunch,
    isDailyOps,
    isDevLoop,
    canOperate,
    loopLabel,
    headerDescription: _headerDescription,
    showLaunchPad,
    phases,
    statuses,
    phaseHints,
    doneCount,
    phaseDefaultOpen,
    rocketProd,
    satelliteProd,
    onOpenPhasePage,
    onPhaseFixAction,
    onNavigate,
    q,
    agents,
    fix,
  } = props

  const [phaseOpen, setPhaseOpen] = useState(phaseDefaultOpen)
  const [selectedPhaseId, setSelectedPhaseId] = useState<string | undefined>()
  const [selectedCommandLane, setSelectedCommandLane] = useState<CommandLane>('vehicle')
  useEffect(() => {
    setPhaseOpen(phaseDefaultOpen)
  }, [phaseDefaultOpen, mode.id])

  const phaseProgressHint = isDevLoop
    ? phaseOpen
      ? 'Open — Dev playbook checklist'
      : 'Collapsed — Dev playbook checklist'
    : phaseOpen
      ? 'Open — not live Go/No-Go'
      : 'Collapsed — not live Go/No-Go'

  const phaseProgressCaption = isDevLoop
    ? 'Playbook phase status — Briefing → implement → deliver → sign-off'
    : 'Historical phase checklist — not live environment health'

  const firstIncompletePhase = isMissionLaunch
    ? phases.find(phase => statuses[phase.id] !== 'done')
    : undefined
  const phaseProgressTarget = firstIncompletePhase ?? phases.at(-1)
  const openPhaseProgress = () => {
    setPhaseOpen(true)
    if (phaseProgressTarget != null) setSelectedPhaseId(phaseProgressTarget.id)
    requestAnimationFrame(() => {
      document
        .getElementById('task-cc-phase-progress')
        ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    })
  }
  const selectedCommand = {
    vehicle: {
      label: 'Rocket',
      verdict:
        q.rocketVerdict.kind === 'GO'
          ? 'GO'
          : q.rocketVerdict.kind === 'IN_FLIGHT'
            ? 'IN FLIGHT'
            : 'NO-GO',
      valueClass: launchVerdictTextClass(q.rocketVerdict.kind),
    },
    payload: {
      label: 'Satellite',
      verdict:
        q.satelliteVerdict.kind === 'GO'
          ? 'GO'
          : q.satelliteVerdict.kind === 'IN_FLIGHT'
            ? 'IN FLIGHT'
            : 'NO-GO',
      valueClass: launchVerdictTextClass(q.satelliteVerdict.kind),
    },
    plugin: {
      label: 'Plugin · IB Gateway',
      verdict:
        props.pluginLaunchVerdict.kind === 'GO'
          ? 'GO'
          : props.pluginLaunchVerdict.kind === 'IN_FLIGHT'
            ? 'IN FLIGHT'
            : 'NO-GO',
      valueClass: launchVerdictTextClass(props.pluginLaunchVerdict.kind),
    },
    'data-maintenance': {
      label: 'Data · Maintenance',
      verdict: 'Freshness unavailable',
      valueClass: 'text-muted-foreground',
    },
  }[selectedCommandLane]
  const globalHealthClass = signalTextClass(props.missionOverall)
  const checklistClass = checklistTextClass(phases, statuses, doneCount)

  const verdictLamp = useMemo((): VerdictLamp => {
    if (isDevLoop) {
      if (doneCount === phases.length && phases.length > 0) return 'ok'
      if (doneCount > 0) return 'degraded'
      return 'unknown'
    }
    if (isDailyOps) {
      if (!q.runnerHealthy) return 'fail'
      if (q.fleetClear) return 'ok'
      return 'degraded'
    }
    if (isMissionLaunch) {
      const rocket = launchVerdictToSignal(q.rocketVerdict.kind)
      const satellite = launchVerdictToSignal(q.satelliteVerdict.kind)
      const plugin = launchVerdictToSignal(props.pluginLaunchVerdict.kind)
      return worseLamp(worseLamp(rocket, satellite), plugin)
    }
    return 'unknown'
  }, [
    isDevLoop,
    isDailyOps,
    isMissionLaunch,
    doneCount,
    phases.length,
    q.runnerHealthy,
    q.fleetClear,
    q.rocketVerdict.kind,
    q.satelliteVerdict.kind,
    props.pluginLaunchVerdict.kind,
  ])

  const verdictActions = useMemo(() => {
    const nodes: ReactNode[] = []
    if (isDailyOps && !q.fleetClear) {
      nodes.push(
        <Button
          key="daily-ops-fix"
          type="button"
          size="sm"
          variant="outline"
          className="shrink-0"
          disabled={!canOperate || fix.dailyOpsAgentPending}
          title={
            !canOperate
              ? 'Authenticate as operator to run Fleet Fix'
              : (fix.dailyOpsWorkflow?.primaryAction.label ?? 'Fix top signal')
          }
          onClick={() => fix.handleFleetPrimaryCta()}
        >
          {fix.dailyOpsWorkflow?.primaryAction.label ?? 'Fix top signal'}
        </Button>,
      )
    }
    if (isMissionLaunch && showLaunchPad && verdictLamp !== 'ok') {
      nodes.push(
        <button
          key="open-launch-board"
          type="button"
          className="shrink-0 text-[var(--text-dense-caption)] text-primary hover:underline"
          title="Scroll to launch lanes"
          onClick={() =>
            document
              .getElementById('task-cc-launch-board')
              ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
          }
        >
          Open launch board →
        </button>,
      )
    }
    return nodes.length > 0 ? <>{nodes}</> : undefined
  }, [
    isDailyOps,
    isMissionLaunch,
    showLaunchPad,
    canOperate,
    q.fleetClear,
    fix,
    fix.dailyOpsWorkflow?.primaryAction.label,
    verdictLamp,
  ])

  const phaseProgressBlock: ReactNode = isDailyOps ? null : phases.length > 0 ? (
    <details
      id="task-cc-phase-progress"
      className="rounded-lg border border-border bg-card px-3 py-1.5"
      open={phaseOpen}
      onToggle={e => setPhaseOpen((e.currentTarget as HTMLDetailsElement).open)}
    >
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-meta)] font-semibold">Phase progress</span>
          <DenseTag variant="neutral" className="text-[9px]">
            Playbook
          </DenseTag>
          <DenseTag variant="neutral" className="text-[9px]">
            {doneCount}/{phases.length} complete
          </DenseTag>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {phaseProgressHint}
          </span>
        </div>
      </summary>
      <p className="m-0 mb-1.5 mt-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
        {phaseProgressCaption}
      </p>
      <TaskPhaseProgress
        phases={phases}
        statuses={statuses}
        selectedPhaseId={selectedPhaseId}
        hints={phaseHints}
        onOpenFullPage={onOpenPhasePage}
        onFixAction={onPhaseFixAction}
      />
      {isMissionLaunch && (satelliteProd.prodBlocked || rocketProd.prodBlocked) && (
        <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-warning">
          Live readiness blocked — playbook Done does not clear release
        </p>
      )}
    </details>
  ) : null

  const devStripsBlock = isDevLoop ? (
    <DevModeStrips
      mode={mode}
      canOperate={canOperate}
      devProgram={props.devProgram}
      resolvedProgramId={props.resolvedProgramId}
      onNavigate={onNavigate}
      inlineBriefingPack={props.inlineBriefingPack}
      onOpenFullBriefing={props.onOpenBriefing}
      onBriefingOpened={props.handleBriefingOpened}
      devAgentQ={props.devAgentQ}
      phases={phases}
      phaseStatuses={statuses}
    />
  ) : null

  return (
    <div className="flex flex-col gap-4">
      <TaskCCVerdict
        mode={mode}
        lamp={verdictLamp}
        tagLabel={loopLabel}
        tagVariant={mode.loopArchetype === 'dev' ? 'info' : 'neutral'}
        tagTitle={mode.loopArchetype === 'ops' ? 'Workflow identity — not an alert' : undefined}
        summary={props.headerDescription}
        meta={
          isMissionLaunch ? (
            <>
              <button
                type="button"
                className="text-left hover:text-foreground hover:underline"
                title="Open Control Room"
                onClick={() => onNavigate('control-room')}
              >
                <strong className="font-semibold text-muted-foreground">Global health</strong>
                {' — '}
                <span className={`font-semibold ${globalHealthClass}`}>
                  {missionStatus(props.missionOverall)}
                </span>
              </button>
              <button
                type="button"
                className="text-left hover:text-foreground hover:underline"
                title="Open Phase progress"
                onClick={openPhaseProgress}
              >
                <strong className="font-semibold text-muted-foreground">Release checklist</strong>
                {' — '}
                <span className={`font-semibold ${checklistClass}`}>
                  {doneCount}/{phases.length}
                  {firstIncompletePhase != null
                    ? ` · next: ${firstIncompletePhase.title}`
                    : ' · complete'}
                  {' · Platform + Trade (Plugin separate)'}
                </span>
              </button>
              <span>
                <strong className="font-semibold text-muted-foreground">Selected Command</strong>
                {' — '}
                <span className={`font-semibold ${selectedCommand.valueClass}`}>
                  {selectedCommand.label} · {selectedCommand.verdict}
                </span>
              </span>
            </>
          ) : !isDailyOps && phases.length > 0 ? (
            <span className="font-mono-tabular">{doneCount}/{phases.length} phases complete</span>
          ) : undefined
        }
        actions={verdictActions}
      />

      {isDailyOps && fix.aiDailyOpsFix.error != null && (
        <OpsFeedback variant="error" title="Failed to start Agent Fix">
          {fix.aiDailyOpsFix.error.message}
        </OpsFeedback>
      )}
      {isDailyOps && fix.aiGitDirtyFix.error != null && (
        <OpsFeedback variant="error" title="Failed to start Propose commit">
          {fix.aiGitDirtyFix.error.message}
        </OpsFeedback>
      )}
      {props.phaseFixUnavailableHint != null && (
        <OpsFeedback variant="warning" title="Agent Fix unavailable">
          {props.phaseFixUnavailableHint}
        </OpsFeedback>
      )}

      {mode.loopArchetype === 'ops' && showLaunchPad && (
        <>
          {isMissionLaunch && !canOperate && (
            <OpsFeedback variant="warning" title="Authenticate as operator to run Launch Pad agents">
              Use the header auth control before starting release or trade-deploy Agent tasks.
            </OpsFeedback>
          )}
          {agents.aiRelease.error != null && (
            <OpsFeedback variant="error" title="Failed to start AI Release">
              {agents.aiRelease.error.message}
            </OpsFeedback>
          )}
          {agents.aiTradeDeploy.error != null && (
            <OpsFeedback variant="error" title="Failed to start Deploy Satellite agent">
              {agents.aiTradeDeploy.error.message}
            </OpsFeedback>
          )}
          {agents.aiPluginLaunch.error != null && (
            <OpsFeedback variant="error" title="Failed to start Launch Plugin agent">
              {agents.aiPluginLaunch.error.message}
            </OpsFeedback>
          )}
          {agents.aiPlatformProdFix.error != null && (
            <OpsFeedback variant="error" title="Failed to start Agent Fix">
              {agents.aiPlatformProdFix.error.message}
            </OpsFeedback>
          )}
          {agents.aiTradeProdFix.error != null && (
            <OpsFeedback variant="error" title="Failed to start Agent Fix">
              {agents.aiTradeProdFix.error.message}
            </OpsFeedback>
          )}
          {agents.aiBusIngestTriage.error != null && (
            <OpsFeedback variant="error" title="Failed to start Bus Ingest Triage">
              {agents.aiBusIngestTriage.error.message}
            </OpsFeedback>
          )}
        </>
      )}

      {mode.loopArchetype === 'ops' && (isMissionLaunch || isDailyOps) && (
        <OpsTaskSummaryRow
          mode={mode}
          context={props.context}
          matrices={props.matrices}
          stgSmoke={props.stgSmoke}
          stgGate={props.stgGate}
          lastDeliverSucceeded={props.lastDeliverSucceeded}
          tierB={props.tierB}
          onNavigate={onNavigate}
          onOpenPromote={props.onOpenPromote}
          onOpenDelivery={props.onOpenDelivery}
          onDispatchRelease={showLaunchPad ? props.dispatchReleaseAgent : undefined}
          onDispatchTradeDeploy={showLaunchPad ? props.dispatchTradeDeployAgent : undefined}
          onDispatchPluginLaunch={showLaunchPad ? props.dispatchPluginLaunchAgent : undefined}
          releasePending={agents.aiRelease.isPending}
          tradeDeployPending={agents.aiTradeDeploy.isPending}
          pluginLaunchPending={agents.aiPluginLaunch.isPending}
          canDispatchRelease={props.releaseDispatchAllowed}
          canDispatchTradeDeploy={props.tradeDeployDispatchAllowed}
          canDispatchPluginLaunch={props.pluginLaunchDispatchAllowed}
          releaseDisabledReason={props.releaseDisabledReason}
          tradeDeployDisabledReason={props.tradeDeployDisabledReason}
          pluginLaunchDisabledReason={props.pluginLaunchDisabledReason}
          readinessCanOperate={canOperate}
          onAgentFixStg={() => agents.aiTradeStgEnvFix.trigger()}
          onAgentFixProd={() => agents.aiTradeProdEnvFix.trigger()}
          agentFixPending={
            agents.aiTradeStgEnvFix.isPending || agents.aiTradeProdEnvFix.isPending
          }
          agentFixDisabled={!canOperate}
          agentFixTitle={fixScopeAgentTitle(
            agents.tradeStgEnvFixScope,
            scopeToLabel(agents.tradeStgEnvFixScope),
            pickFailingFixSignal(q.stgReadinessSignals)?.label ??
              pickFailingFixSignal(q.prodReadinessSignals)?.label,
          )}
          onAgentTriage={() => agents.aiBusIngestTriage.trigger()}
          agentTriagePending={agents.aiBusIngestTriage.isPending}
          agentTriageDisabled={agents.aiBusIngestTriage.disabled}
          agentTriageTitle={
            agents.aiBusIngestTriage.disabledReason ??
            'Cross-check Socket matrix vs Rocket IB gateway (D10 safe)'
          }
          onFleetCellFix={isDailyOps ? fix.handleFleetCellFix : undefined}
          onFleetPrimaryCta={isDailyOps ? fix.handleFleetPrimaryCta : undefined}
          fleetAgentFixPending={isDailyOps ? fix.dailyOpsAgentPending : undefined}
          fleetWorkflow={fix.dailyOpsWorkflow ?? undefined}
          fleetAgentFixError={isDailyOps ? (fix.aiDailyOpsFix.error?.message ?? null) : undefined}
          onFleetWorkflowAction={isDailyOps ? fix.handleFleetWorkflowAction : undefined}
          onOperatorPlanFix={isDailyOps ? fix.handleOperatorPlanFix : undefined}
          operatorPlanFixPending={isDailyOps ? fix.aiOperatorPlaneFix.isPending : undefined}
          operatorPlanFixDisabled={isDailyOps ? fix.aiOperatorPlaneFix.disabled : undefined}
          operatorPlanFixTitle={
            isDailyOps
              ? (fix.aiOperatorPlaneFix.disabledReason ??
                'Start Operator · Remediate with current bridge probe')
              : undefined
          }
          operatorPlanFixError={
            isDailyOps ? (fix.aiOperatorPlaneFix.error?.message ?? null) : undefined
          }
          onProposeCommit={isDailyOps ? fix.handleProposeCommit : undefined}
          onProposeStash={isDailyOps ? fix.handleProposeStash : undefined}
          proposeCommitPending={isDailyOps ? fix.aiGitDirtyFix.isPending : undefined}
          proposeCommitDisabled={isDailyOps ? fix.aiGitDirtyFix.disabled : undefined}
          proposeCommitTitle={
            isDailyOps
              ? (fix.aiGitDirtyFix.disabledReason ??
                'Start git-dirty-remediate — approval required before commit/stash')
              : undefined
          }
          proposeCommitError={
            isDailyOps ? (fix.aiGitDirtyFix.error?.message ?? null) : undefined
          }
          onChecklistCheck={isDailyOps ? fix.handleChecklistCheck : undefined}
          checklistCheckPending={isDailyOps ? fix.aiChecklistCheck.isPending : undefined}
          checklistCheckDisabled={isDailyOps ? fix.checklistCheckDisabled : undefined}
          checklistCheckTitle={isDailyOps ? fix.checklistCheckTitle : undefined}
          checklistCheckError={
            isDailyOps ? (fix.aiChecklistCheck.error?.message ?? null) : undefined
          }
          checklistCheckActive={isDailyOps ? fix.checklistCheckActive : undefined}
          checklistCheckStatusHint={
            isDailyOps ? (q.activeChecklistRunJob?.phase ?? null) : undefined
          }
          onChecklistItemFix={isDailyOps ? fix.handleChecklistItemFix : undefined}
          checklistItemFixPending={isDailyOps ? fix.aiChecklistItemFix.isPending : undefined}
          checklistItemFixDisabled={
            isDailyOps
              ? fix.checklistItemFixBlocked != null || !q.runnerHealthy
              : undefined
          }
          checklistItemFixTitle={
            isDailyOps
              ? !q.runnerHealthy
                ? 'Remediation runner not healthy — check Engineer · runners-ha'
                : (fix.checklistItemFixBlocked ??
                  'Start Ops Agent Fix for this checklist item (not Cursor Ask for AI)')
              : undefined
          }
          checklistItemFixError={
            isDailyOps ? (fix.aiChecklistItemFix.error?.message ?? null) : undefined
          }
          checklistItemFixActiveId={isDailyOps ? fix.checklistItemFixActiveId : undefined}
          recentRuns={isMissionLaunch ? q.platformRunsQ.data?.runs : undefined}
          recentRunsLoading={isMissionLaunch ? q.platformRunsQ.isLoading : false}
          tradeRecentRuns={isMissionLaunch ? q.tradeRunsQ.data?.runs : undefined}
          tradeRecentRunsLoading={isMissionLaunch ? q.tradeRunsQ.isLoading : false}
          tradePipelineRunsNamespace={isMissionLaunch ? q.tradeRunsQ.data?.namespace : undefined}
          launchVerdict={isMissionLaunch ? q.rocketVerdict : undefined}
          launchCheckpoints={isMissionLaunch ? q.rocketCheckpoints : undefined}
          satelliteLaunchVerdict={isMissionLaunch ? q.satelliteVerdict : undefined}
          satelliteLaunchCheckpoints={isMissionLaunch ? q.satelliteCheckpoints : undefined}
          pluginLaunchVerdict={isMissionLaunch ? props.pluginLaunchVerdict : undefined}
          pluginLaunchCheckpoints={isMissionLaunch ? props.pluginLaunchCheckpoints : undefined}
          pluginEvidence={isMissionLaunch ? props.pluginEvidence : undefined}
          onLaunchAgentFix={isMissionLaunch ? () => agents.aiPlatformProdFix.trigger() : undefined}
          onSatelliteLaunchAgentFix={
            isMissionLaunch ? () => agents.aiTradeProdFix.trigger() : undefined
          }
          launchAgentFixPending={isMissionLaunch ? agents.aiPlatformProdFix.isPending : false}
          launchAgentFixActive={isMissionLaunch ? agents.aiPlatformProdFix.isActive : false}
          launchAgentFixDisabled={isMissionLaunch ? agents.aiPlatformProdFix.disabled : true}
          launchAgentFixTitle={
            isMissionLaunch
              ? (agents.aiPlatformProdFix.disabledReason ??
                fixScopeAgentTitle(
                  agents.platformProdFixScope,
                  scopeToLabel(agents.platformProdFixScope),
                  pickFailingFixSignal(rocketProd.fixSignals ?? [])?.label,
                ))
              : undefined
          }
          satelliteLaunchAgentFixPending={isMissionLaunch ? agents.aiTradeProdFix.isPending : false}
          satelliteLaunchAgentFixActive={isMissionLaunch ? agents.aiTradeProdFix.isActive : false}
          satelliteLaunchAgentFixDisabled={isMissionLaunch ? agents.aiTradeProdFix.disabled : true}
          satelliteLaunchAgentFixTitle={
            isMissionLaunch
              ? (agents.aiTradeProdFix.disabledReason ??
                fixScopeAgentTitle(
                  agents.tradeProdFixScope,
                  scopeToLabel(agents.tradeProdFixScope),
                  pickFailingFixSignal(agents.tradeProdFixSignals)?.label,
                ))
              : undefined
          }
          onOpenAgentDesk={arg =>
            props.onOpenAgentDesk?.(arg ?? props.ambientJobId ?? undefined)
          }
          onExpandAgentDock={props.onExpandAgentDock}
          ambientJobId={props.ambientJobId}
          ambientJobScope={props.ambientJobScope}
          onStartAgentJob={isDailyOps ? props.onStartAgentJob : undefined}
          activeDispatchJobs={isDailyOps ? q.activeDispatchJobs : undefined}
          pipelineRunsNamespace={isMissionLaunch ? q.platformRunsQ.data?.namespace : undefined}
          platformStgGate={q.platformStgGateQ.data}
          platformProdGate={q.platformProdGateQ.data}
          supplyCmsPresent={q.supplyQ.data?.dockerfile_configmaps?.filter(c => c.present).length}
          supplyCmsTotal={q.supplyQ.data?.dockerfile_configmaps?.length}
          selectedCommandLane={selectedCommandLane}
          onSelectedCommandLaneChange={setSelectedCommandLane}
        />
      )}

      {isDevLoop ? (
        <>
          {devStripsBlock}
          {phaseProgressBlock}
        </>
      ) : (
        <>
          {phaseProgressBlock}
          {isMissionLaunch && (
            <OpsTaskStrips
              mode={mode}
              context={props.context}
              matrices={props.matrices}
              stgSmoke={props.stgSmoke}
              stgGate={props.stgGate}
              lastDeliverSucceeded={props.lastDeliverSucceeded}
              tierB={props.tierB}
              onNavigate={onNavigate}
              onOpenPromote={props.onOpenPromote}
              onOpenDelivery={props.onOpenDelivery}
              onDispatchRelease={showLaunchPad ? props.dispatchReleaseAgent : undefined}
              onDispatchTradeDeploy={showLaunchPad ? props.dispatchTradeDeployAgent : undefined}
              onDispatchPluginLaunch={showLaunchPad ? props.dispatchPluginLaunchAgent : undefined}
              releasePending={agents.aiRelease.isPending}
              tradeDeployPending={agents.aiTradeDeploy.isPending}
              pluginLaunchPending={agents.aiPluginLaunch.isPending}
              canDispatchRelease={props.releaseDispatchAllowed}
              canDispatchTradeDeploy={props.tradeDeployDispatchAllowed}
              canDispatchPluginLaunch={props.pluginLaunchDispatchAllowed}
              releaseDisabledReason={props.releaseDisabledReason}
              tradeDeployDisabledReason={props.tradeDeployDisabledReason}
              pluginLaunchDisabledReason={props.pluginLaunchDisabledReason}
              promoteOnly
              selectedCommandLane={selectedCommandLane}
              onSelectedCommandLaneChange={setSelectedCommandLane}
            />
          )}
        </>
      )}
    </div>
  )
}
