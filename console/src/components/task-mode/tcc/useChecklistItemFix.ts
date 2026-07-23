import { useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchCluster, fetchClusterServiceReadiness } from '@/api/cluster'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import {
  cellAllowsAgentFix,
  pickFleetFixCell,
} from '@/lib/control-room/fleetCellFix'
import { resolveDailyOpsWorkflow } from '@/lib/control-room/dailyOpsWorkflow'
import type { FleetCell, FleetSnapshot } from '@/lib/control-room/fleetSnapshot'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import type { RemediationJob } from '@/api/remediationTypes'
import { useChecklistGitDirtyFix } from '@/components/task-mode/tcc/useChecklistGitDirtyFix'
import { useFleetCellFixAgents } from '@/components/task-mode/tcc/useFleetCellFixAgents'

/**
 * Checklist item Fix mutation + Daily Ops remediation ambient agents
 * (fleet cell / operator plane / git-dirty / checklist AI Check) and their
 * handlers / workflow. Extracted from TaskControlCenter.
 */
export function useChecklistItemFix({
  isDailyOps,
  canOperate,
  ambientJobId,
  ambientJobScope,
  onStartAgentJob,
  onNavigate,
  onOpenAgentDesk,
  onExpandAgentDock,
  fleet,
  setFleetFixCell,
  fleetFixCellRef,
  dailyOpsTargetCell,
  dailyOpsFixScope,
  clusterForFixQ,
  serviceReadinessForFixQ,
  runnerHealthy,
  checklistCheckAmbient,
  activeChecklistRunJob,
  operateQueueOpenCount,
}: AmbientAgentShellProps & {
  isDailyOps: boolean
  canOperate: boolean
  onNavigate: (tabId: string) => void
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
  fleet: FleetSnapshot
  setFleetFixCell: (cell: FleetCell | null) => void
  fleetFixCellRef: MutableRefObject<FleetCell | null>
  dailyOpsTargetCell: FleetCell | null
  dailyOpsFixScope: string
  clusterForFixQ: ReturnType<typeof useQuery<Awaited<ReturnType<typeof fetchCluster>>>>
  serviceReadinessForFixQ: ReturnType<
    typeof useQuery<Awaited<ReturnType<typeof fetchClusterServiceReadiness>>>
  >
  runnerHealthy: boolean
  checklistCheckAmbient: boolean
  activeChecklistRunJob: RemediationJob | null | undefined
  operateQueueOpenCount: number
}) {
  const qc = useQueryClient()
  const dailyOpsFixStartedRef = useRef(false)
  const checklistCheckStartedRef = useRef(false)
  const prevAmbientJobIdRef = useRef<string | null | undefined>(undefined)
  const prevAmbientJobScopeRef = useRef<string | null | undefined>(undefined)
  const [agentJustSucceeded, setAgentJustSucceeded] = useState(false)

  const gitDirty = useChecklistGitDirtyFix({
    isDailyOps,
    canOperate,
    ambientJobId,
    onStartAgentJob,
    fleet,
    runnerHealthy,
    checklistCheckAmbient,
    activeChecklistRunJob,
    dailyOpsFixStartedRef,
    checklistCheckStartedRef,
    setAgentJustSucceeded,
  })

  const fleetFix = useFleetCellFixAgents({
    isDailyOps,
    canOperate,
    ambientJobId,
    onStartAgentJob,
    onNavigate,
    fleet,
    setFleetFixCell,
    fleetFixCellRef,
    dailyOpsTargetCell,
    dailyOpsFixScope,
    clusterForFixQ,
    serviceReadinessForFixQ,
    dailyOpsFixStartedRef,
    setAgentJustSucceeded,
    otherAgentPending: gitDirty.otherAgentPending,
  })

  const dailyOpsWorkflow = useMemo(() => {
    if (!isDailyOps) return null
    return resolveDailyOpsWorkflow({
      fleet,
      agentPending: fleetFix.dailyOpsAgentPending,
      agentJustSucceeded,
      queueOpen: operateQueueOpenCount,
    })
  }, [
    isDailyOps,
    fleet,
    fleetFix.dailyOpsAgentPending,
    agentJustSucceeded,
    operateQueueOpenCount,
  ])

  useEffect(() => {
    const prevId = prevAmbientJobIdRef.current
    const prevScope = prevAmbientJobScopeRef.current
    prevAmbientJobIdRef.current = ambientJobId
    prevAmbientJobScopeRef.current = ambientJobScope

    if (prevId != null && ambientJobId == null) {
      if (prevScope === DAILY_OPS_CHECKLIST_RUN_SCOPE || checklistCheckStartedRef.current) {
        checklistCheckStartedRef.current = false
        void qc.invalidateQueries({ queryKey: ['checklist', 'signals'] })
        void qc.invalidateQueries({ queryKey: ['checklist', 'kpis'] })
        void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
        void qc.invalidateQueries({ queryKey: ['cockpit'] })
      }
      if (dailyOpsFixStartedRef.current && prevScope !== DAILY_OPS_CHECKLIST_RUN_SCOPE) {
        const jobsCaches = [
          qc.getQueryData<{ jobs: { id: string; status: string }[] }>(['remediation', 'jobs']),
          qc.getQueryData<{ jobs: { id: string; status: string }[] }>([
            'remediation',
            'jobs',
            'checklist-dispatch',
          ]),
        ]
        const ended = jobsCaches
          .flatMap(c => c?.jobs ?? [])
          .find(j => j.id === prevId)
        if (ended?.status === 'done') {
          setAgentJustSucceeded(true)
        }
        void qc.invalidateQueries({ queryKey: ['cockpit'] })
        void qc.invalidateQueries({ queryKey: ['checklist', 'signals'] })
        void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      }
    }
  }, [ambientJobId, ambientJobScope, qc])

  const handleFleetWorkflowAction = () => {
    if (dailyOpsWorkflow == null) return
    const action = dailyOpsWorkflow.primaryAction
    if (action.kind === 'agent-fix') {
      const cell =
        (action.cellKey != null ? fleet.cells.find(c => c.key === action.cellKey) : null) ??
        pickFleetFixCell(fleet)
      if (cell != null && cellAllowsAgentFix(cell)) {
        fleetFix.handleFleetCellFix(cell)
      }
      return
    }
    if (action.kind === 'operator-plan') {
      gitDirty.handleOperatorPlanFix()
      return
    }
    if (action.kind === 'propose-commit') {
      gitDirty.handleProposeCommit()
      return
    }
    if (action.kind === 'manual-next') {
      return
    }
    if (action.kind === 'view-agent') {
      if (onExpandAgentDock != null) {
        onExpandAgentDock()
        return
      }
      onOpenAgentDesk?.(ambientJobId ?? undefined)
      return
    }
    if (action.kind === 'navigate' || action.kind === 'clear-queue') {
      if (action.tabId != null) onNavigate(action.tabId)
      return
    }
    if (action.kind === 'ai-check' || action.kind === 'run-check') {
      gitDirty.handleChecklistCheck()
      return
    }
    if (action.kind === 'verify') {
      void qc.invalidateQueries({ queryKey: ['cockpit'] })
    }
  }

  return {
    aiDailyOpsFix: fleetFix.aiDailyOpsFix,
    aiOperatorPlaneFix: gitDirty.aiOperatorPlaneFix,
    aiGitDirtyFix: gitDirty.aiGitDirtyFix,
    aiChecklistCheck: gitDirty.aiChecklistCheck,
    aiChecklistItemFix: gitDirty.aiChecklistItemFix,
    handleFleetCellFix: fleetFix.handleFleetCellFix,
    handleOperatorPlanFix: gitDirty.handleOperatorPlanFix,
    handleProposeCommit: gitDirty.handleProposeCommit,
    handleProposeStash: gitDirty.handleProposeStash,
    handleChecklistCheck: gitDirty.handleChecklistCheck,
    handleChecklistItemFix: gitDirty.handleChecklistItemFix,
    handleFleetPrimaryCta: fleetFix.handleFleetPrimaryCta,
    handleFleetWorkflowAction,
    checklistCheckActive: gitDirty.checklistCheckActive,
    checklistCheckDisabled: gitDirty.checklistCheckDisabled,
    checklistCheckTitle: gitDirty.checklistCheckTitle,
    checklistItemFixActiveId: gitDirty.checklistItemFixActiveId,
    checklistItemFixBlocked: gitDirty.checklistItemFixBlocked,
    dailyOpsAgentPending: fleetFix.dailyOpsAgentPending,
    dailyOpsWorkflow,
  }
}
