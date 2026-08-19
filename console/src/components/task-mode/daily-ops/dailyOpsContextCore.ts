import { createContext } from 'react'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import type { DailyOpsWorkflowResult } from '@/lib/control-room/dailyOpsWorkflow'
import type { FleetSnapshot } from '@/lib/control-room/fleetSnapshot'
import type { ChecklistItemFixHandler } from '@/components/task-mode/operator-plan/checklistResolve'

/**
 * Shared Daily Ops Fleet Desk state.
 *
 * Eliminates prop drilling from OpsTaskStrips (DailyOpsFleetDesk) into
 * DailyOpsProcessStrip / DailyOpsExecutionPanel / DailyOpsOperatorPlanPanel /
 * DailyOpsFleetBoard. Only the densest shared clusters live here — fleet +
 * workflow, ambient agent job, propose commit, checklist AI Check, checklist
 * item Fix, and verify/re-probe. One-off or per-call-site composed props
 * (onNavigate, onPrimaryAction, coverage, selection state, …) stay as direct
 * component props.
 */
export type DailyOpsContextValue = {
  // Fleet + workflow
  fleet: FleetSnapshot
  fleetWorkflow?: DailyOpsWorkflowResult
  isLoading: boolean
  canOperate?: boolean
  /** Fleet-cell / primary Agent Fix pending (not Operator Plan / checklist). */
  agentFixPending?: boolean

  // Ambient agent job
  ambientJobId?: string | null
  ambientJobScope?: string | null
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
  /** Expand shell Agent Execution Dock — stay on Daily Ops board. */
  onExpandAgentDock?: () => void
  /** Adopt an existing remediation job as ambient (Queue → Now). */
  onStartAgentJob?: (job: { id: string; scope: string; label: string }) => void

  // Propose commit (git-dirty-remediate). Stash path removed.
  onProposeCommit?: () => void
  proposeCommitPending?: boolean
  proposeCommitDisabled?: boolean
  proposeCommitTitle?: string
  proposeCommitError?: string | null

  // Checklist AI Check (daily-ops-checklist-run)
  onChecklistCheck?: () => void
  checklistCheckPending?: boolean
  checklistCheckDisabled?: boolean
  checklistCheckTitle?: string
  checklistCheckError?: string | null
  checklistCheckActive?: boolean
  checklistCheckStatusHint?: string | null

  // Checklist item Fix (row-level Ops Agent)
  onChecklistItemFix?: ChecklistItemFixHandler
  checklistItemFixPending?: boolean
  checklistItemFixDisabled?: boolean
  checklistItemFixTitle?: string
  checklistItemFixError?: string | null
  checklistItemFixActiveId?: string | null

  /** Verify / re-probe fleet — same as Ops loop Verify CTA (invalidate cockpit). */
  onVerifyReprobe: () => void
}

export const DailyOpsContext = createContext<DailyOpsContextValue | null>(null)
