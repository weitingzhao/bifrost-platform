import { useEffect, useRef, useState, type MutableRefObject } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { fetchAgentBridge } from '@/api/agentOps'
import { startRemediation } from '@/api/remediation'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { ambientAgentBlockedReason } from '@/lib/agent/ambientAgent'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import { DAILY_OPS_CHECKLIST_RUN_PROMPT } from '@/lib/control-room/checklistProgress'
import {
  buildOperatorPlaneFixPrompt,
  OPERATOR_PLANE_FIX_SCOPE,
} from '@/lib/agent/operatorPlaneFixPrompt'
import {
  buildGitDirtyRemediatePrompt,
  GIT_DIRTY_FIX_SCOPE,
} from '@/lib/agent/gitDirtyRemediatePrompt'
import { recordChecklistRunTouch } from '@/lib/control-room/dailyOpsChecklistCoverage'
import type { FleetSnapshot } from '@/lib/control-room/fleetSnapshot'
import type { RemediationJob } from '@/api/remediationTypes'

export function useChecklistGitDirtyFix({
  isDailyOps,
  canOperate,
  ambientJobId,
  ambientJobStatus,
  onStartAgentJob,
  fleet,
  runnerHealthy,
  checklistCheckAmbient,
  activeChecklistRunJob,
  dailyOpsFixStartedRef,
  checklistCheckStartedRef,
  setAgentJustSucceeded,
}: Pick<AmbientAgentShellProps, 'ambientJobId' | 'ambientJobStatus' | 'onStartAgentJob'> & {
  isDailyOps: boolean
  canOperate: boolean
  fleet: FleetSnapshot
  runnerHealthy: boolean
  checklistCheckAmbient: boolean
  activeChecklistRunJob: RemediationJob | null | undefined
  dailyOpsFixStartedRef: MutableRefObject<boolean>
  checklistCheckStartedRef: MutableRefObject<boolean>
  setAgentJustSucceeded: (value: boolean) => void
}) {
  const qc = useQueryClient()

  const aiOperatorPlaneFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: OPERATOR_PLANE_FIX_SCOPE,
    label: scopeToLabel(OPERATOR_PLANE_FIX_SCOPE),
    buildRequest: async () => {
      const bridge = await fetchAgentBridge()
      return { prompt: buildOperatorPlaneFixPrompt(bridge) }
    },
  })

  const aiGitDirtyFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: GIT_DIRTY_FIX_SCOPE,
    label: scopeToLabel(GIT_DIRTY_FIX_SCOPE),
    buildRequest: async () => {
      const bridge = await fetchAgentBridge()
      const base = buildGitDirtyRemediatePrompt(bridge)
      const extra = [
        '',
        '## Operator intent: PROPOSE COMMIT',
        'Draft commit_message → request_operator_approval → git_commit. Never stash.',
      ].join('\n')
      return { prompt: `${base}${extra}` }
    },
  })

  const aiChecklistCheck = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: DAILY_OPS_CHECKLIST_RUN_SCOPE,
    label: scopeToLabel(DAILY_OPS_CHECKLIST_RUN_SCOPE),
    buildRequest: () => ({ prompt: DAILY_OPS_CHECKLIST_RUN_PROMPT }),
  })

  const handleOperatorPlanFix = () => {
    dailyOpsFixStartedRef.current = true
    setAgentJustSucceeded(false)
    const engineerCell = fleet.cells.find(c => c.role === 'engineer')
    if (engineerCell != null) recordChecklistRunTouch(engineerCell)
    aiOperatorPlaneFix.trigger()
  }

  const handleProposeCommit = () => {
    dailyOpsFixStartedRef.current = true
    setAgentJustSucceeded(false)
    const engineerCell = fleet.cells.find(c => c.role === 'engineer')
    if (engineerCell != null) recordChecklistRunTouch(engineerCell)
    aiGitDirtyFix.trigger()
  }

  const handleChecklistCheck = () => {
    checklistCheckStartedRef.current = true
    aiChecklistCheck.trigger()
  }

  const checklistItemFixRef = useRef<{
    itemId: string
    scope: string
    label: string
    prompt: string
  } | null>(null)
  const [checklistItemFixActiveId, setChecklistItemFixActiveId] = useState<string | null>(null)

  const aiChecklistItemFix = useMutation({
    mutationFn: async () => {
      const r = checklistItemFixRef.current
      if (r == null) throw new Error('No checklist item selected for Fix')
      return startRemediation({ scope: r.scope, prompt: r.prompt })
    },
    onSuccess: job => {
      const r = checklistItemFixRef.current
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      void qc.invalidateQueries({ queryKey: ['checklist', 'signals'] })
      onStartAgentJob?.({
        id: job.id,
        scope: r?.scope ?? job.scope ?? 'checklist-item-fix',
        label: r?.label ?? scopeToLabel(r?.scope ?? job.scope ?? 'checklist-item-fix'),
      })
    },
  })

  useEffect(() => {
    if (ambientJobId == null) setChecklistItemFixActiveId(null)
  }, [ambientJobId])

  const checklistItemFixBlocked = ambientAgentBlockedReason(
    canOperate,
    ambientJobId,
    onStartAgentJob,
    ambientJobStatus,
  )

  const handleChecklistItemFix = (args: {
    itemId: string
    fixScope: string
    label: string
    prompt: string
  }) => {
    checklistItemFixRef.current = {
      itemId: args.itemId,
      scope: args.fixScope,
      label: args.label,
      prompt: args.prompt,
    }
    setChecklistItemFixActiveId(args.itemId)
    setAgentJustSucceeded(false)
    aiChecklistItemFix.mutate()
  }

  const checklistCheckActive =
    isDailyOps &&
    (aiChecklistCheck.isPending ||
      checklistCheckAmbient ||
      activeChecklistRunJob != null)

  const checklistCheckDisabled = aiChecklistCheck.disabled || !runnerHealthy

  const checklistCheckTitle = !runnerHealthy
    ? 'Remediation runner not healthy — check Engineer · runners-ha'
    : (aiChecklistCheck.disabledReason ??
      'AI Check: daily-ops-checklist-run probe → report_checklist_signals (not Operator Plane Fix)')

  const otherAgentPending =
    aiOperatorPlaneFix.isPending || aiGitDirtyFix.isPending

  return {
    aiOperatorPlaneFix,
    aiGitDirtyFix,
    aiChecklistCheck,
    aiChecklistItemFix,
    handleOperatorPlanFix,
    handleProposeCommit,
    handleChecklistCheck,
    handleChecklistItemFix,
    checklistCheckActive,
    checklistCheckDisabled,
    checklistCheckTitle,
    checklistItemFixActiveId,
    checklistItemFixBlocked,
    otherAgentPending,
  }
}

export type ChecklistGitDirtyFix = ReturnType<typeof useChecklistGitDirtyFix>
