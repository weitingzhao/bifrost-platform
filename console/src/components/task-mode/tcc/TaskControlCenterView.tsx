import { useEffect, useState, type ReactNode } from 'react'
import { DenseTag } from '@bifrost/ui'
import { ConsolePageHeader } from '@/components/layout/ConsolePageHeader'
import type { ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/deliveryTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { OpsTaskStrips, OpsTaskSummaryRow } from '@/components/task-mode/OpsTaskStrips'
import { DevModeStrips } from '@/components/task-mode/DevModeController'
import { TaskPhaseProgress } from '@/components/task-mode/TaskPhaseProgress'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { ViewerEnvBadge } from '@/components/task-mode/ViewerEnvBadge'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import type { FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { pickFailingFixSignal } from '@/lib/agent/prodEnvironmentFixPrompt'
import { fixScopeAgentTitle } from '@/lib/agent/readinessFixDispatch'
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
  releaseDispatchAllowed: boolean
  tradeDeployDispatchAllowed: boolean
  releaseDisabledReason?: string
  tradeDeployDisabledReason?: string
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
    headerDescription,
    viewerEnv,
    viewerEnvLoading,
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

  const phaseProgressBlock: ReactNode = isDailyOps ? null : phases.length > 0 ? (
    <details
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
      <ConsolePageHeader
        title="Task Control Center"
        help={headerDescription}
        actions={
          mode.loopArchetype === 'system' ? undefined : (
            <div className="flex flex-wrap items-center gap-1.5">
              {(isDailyOps || mode.loopArchetype === 'ops') && (
                <ViewerEnvBadge viewerEnv={viewerEnv} isLoading={viewerEnvLoading} />
              )}
              <DenseTag variant={mode.loopArchetype === 'dev' ? 'info' : 'warning'}>{loopLabel}</DenseTag>
            </div>
          )
        }
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
          releasePending={agents.aiRelease.isPending}
          tradeDeployPending={agents.aiTradeDeploy.isPending}
          canDispatchRelease={props.releaseDispatchAllowed}
          canDispatchTradeDeploy={props.tradeDeployDispatchAllowed}
          releaseDisabledReason={props.releaseDisabledReason}
          tradeDeployDisabledReason={props.tradeDeployDisabledReason}
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
              releasePending={agents.aiRelease.isPending}
              tradeDeployPending={agents.aiTradeDeploy.isPending}
              canDispatchRelease={props.releaseDispatchAllowed}
              canDispatchTradeDeploy={props.tradeDeployDispatchAllowed}
              releaseDisabledReason={props.releaseDisabledReason}
              tradeDeployDisabledReason={props.tradeDeployDisabledReason}
              promoteOnly
            />
          )}
        </>
      )}
    </div>
  )
}
