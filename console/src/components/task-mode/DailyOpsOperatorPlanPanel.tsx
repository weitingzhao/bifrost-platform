import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@bifrost/ui'
import { fetchChecklistKPIs, fetchChecklistSignals, type ChecklistDispatchActionDto } from '@/api/checklist'
import { fetchRemediationJobs } from '@/api/remediation'
import type { RemediationJob } from '@/api/remediationTypes'
import {
  deriveChecklistHeaderProgress,
  findActiveChecklistRunJob,
} from '@/lib/control-room/checklistProgress'
import {
  buildChecklistCursorFailoverPack,
  buildChecklistCursorFailoverPrompt,
  checklistItemNeedsAttention,
  type ChecklistFailoverItemInput,
} from '@/lib/control-room/checklistCursorFailoverPrompt'
import { checklistStepIdsForRemediation } from '@/lib/control-room/dailyOpsChecklistCatalog'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import {
  coverageKeysForChecklistStep,
  type ChecklistCoverageIndex,
} from '@/lib/control-room/dailyOpsChecklistCoverage'
import { useNowMs } from '@/hooks/useNowMs'
import type { FleetCell } from '@/lib/control-room/fleetSnapshot'
import type { DailyOpsWorkflowPhase } from '@/lib/control-room/dailyOpsWorkflow'
import { useDailyOpsContext } from '@/components/task-mode/daily-ops/useDailyOpsContext'
import { ChecklistKPIHeader } from './operator-plan/ChecklistKPIHeader'
import { ChecklistSection } from './operator-plan/ChecklistSection'
import { resolveChecklist, toFailoverInput } from './operator-plan/checklistResolve'

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export type DailyOpsOperatorPlanPanelProps = {
  engineerCell: FleetCell | null
  coverage?: ChecklistCoverageIndex | null
  activeFlashStepId?: string | null
  onOpenFullOperatorPlane: () => void
  onFlashStep?: (stepId: string, coverageKeys: string[]) => void
  /** Optional override — defaults to live remediation job list query. */
  activeDispatchJobs?: RemediationJob[]
  onOpenDispatchJob?: (jobId: string) => void
  /** Wave 4.1 — open Control Room Operate Queue (checklist_dispatch). */
  onOpenOperateQueue?: (queueId?: string) => void
  /** When true (split layout beside Fleet Board), use single-column step cards. */
  compactColumns?: boolean
  /** Process strip phase — Remediate collapses governance meta; all phases default failing-only. */
  workflowPhase?: DailyOpsWorkflowPhase
}

export function DailyOpsOperatorPlanPanel({
  coverage,
  activeFlashStepId = null,
  onOpenFullOperatorPlane,
  onFlashStep,
  activeDispatchJobs,
  onOpenDispatchJob,
  onOpenOperateQueue,
  compactColumns = false,
  workflowPhase,
}: DailyOpsOperatorPlanPanelProps) {
  const {
    fleet,
    onChecklistCheck,
    checklistCheckPending = false,
    checklistCheckDisabled = false,
    checklistCheckTitle,
    checklistCheckError = null,
    checklistCheckActive = false,
    checklistCheckStatusHint = null,
    onChecklistItemFix,
    checklistItemFixPending = false,
    checklistItemFixDisabled = false,
    checklistItemFixTitle,
    checklistItemFixError = null,
    checklistItemFixActiveId = null,
    ambientJobId = null,
    ambientJobScope = null,
  } = useDailyOpsContext()
  const nowMs = useNowMs()
  const resolved = useMemo(() => resolveChecklist(fleet), [fleet])
  const checkBusy = checklistCheckPending || checklistCheckActive
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'error'>('idle')
  const [copiedItemId, setCopiedItemId] = useState<string | null>(null)
  const remediatingPhase = workflowPhase === 'remediate'
  const [showMeta, setShowMeta] = useState(!remediatingPhase)
  // All Ops loop phases default to Failing only (Show all is opt-in).
  const [failingOnly, setFailingOnly] = useState(true)

  useEffect(() => {
    if (workflowPhase === 'remediate') {
      setShowMeta(false)
    } else if (workflowPhase === 'discover') {
      setShowMeta(true)
    }
    setFailingOnly(true)
  }, [workflowPhase])

  const agentRemediating =
    (ambientJobId != null && ambientJobId !== '') ||
    checklistItemFixPending ||
    checklistItemFixActiveId != null

  const remediatingStepIds = useMemo(() => {
    if (!agentRemediating) return new Set<string>()
    const scope =
      ambientJobScope === DAILY_OPS_CHECKLIST_RUN_SCOPE ? null : ambientJobScope
    return new Set(
      checklistStepIdsForRemediation({
        itemId: checklistItemFixActiveId,
        fixScope: scope,
      }),
    )
  }, [agentRemediating, ambientJobScope, checklistItemFixActiveId])

  // Sync Fleet Board flash with remediating Checklist section(s).
  useEffect(() => {
    if (remediatingStepIds.size === 0 || onFlashStep == null) return
    const stepId = [...remediatingStepIds][0]
    if (stepId == null) return
    const keys = coverageKeysForChecklistStep(coverage, stepId)
    onFlashStep(stepId, keys)
  }, [remediatingStepIds, coverage, onFlashStep])
  const signalsQuery = useQuery({
    queryKey: ['checklist', 'signals'],
    queryFn: fetchChecklistSignals,
    refetchInterval: checkBusy ? 5_000 : 30_000,
  })
  const kpisQuery = useQuery({
    queryKey: ['checklist', 'kpis'],
    queryFn: fetchChecklistKPIs,
    refetchInterval: checkBusy ? 5_000 : 60_000,
  })
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs', 'checklist-panel'],
    queryFn: fetchRemediationJobs,
    refetchInterval: checkBusy ? 3_000 : 15_000,
    enabled: activeDispatchJobs == null,
  })
  const jobs = useMemo(
    () => activeDispatchJobs ?? jobsQuery.data?.jobs ?? [],
    [activeDispatchJobs, jobsQuery.data?.jobs],
  )
  const dispatchByItem = useMemo(() => {
    const map = new Map<string, ChecklistDispatchActionDto>()
    for (const a of signalsQuery.data?.last_dispatch ?? []) {
      map.set(a.item_id, a)
    }
    return map
  }, [signalsQuery.data?.last_dispatch])
  const agentSignalByItem = useMemo(() => {
    const map = new Map<string, string>()
    for (const s of signalsQuery.data?.signals ?? []) {
      map.set(s.item_id, s.signal)
    }
    return map
  }, [signalsQuery.data?.signals])

  const headerProgress = useMemo(
    () =>
      deriveChecklistHeaderProgress({
        jobs,
        lastDispatch: signalsQuery.data?.last_dispatch,
      }),
    [jobs, signalsQuery.data?.last_dispatch],
  )
  const activeProber = findActiveChecklistRunJob(jobs)
  const proberHint =
    checklistCheckStatusHint ??
    (activeProber != null ? activeProber.phase : null) ??
    (checklistCheckActive ? 'running' : null)

  const totalItems = resolved.reduce((n, s) => n + s.items.length, 0)
  const okItems = resolved.reduce(
    (n, s) => n + s.items.filter(i => i.overallSignal === 'ok').length,
    0,
  )
  const failItems = resolved.reduce(
    (n, s) =>
      n +
      s.items.filter(i => i.overallSignal === 'fail' || i.overallSignal === 'degraded').length,
    0,
  )
  const streak = kpisQuery.data?.quiet_success_streak ?? 0
  const newFailHint = kpisQuery.data?.new_fail_hint

  const failoverInputs = useMemo((): ChecklistFailoverItemInput[] => {
    const out: ChecklistFailoverItemInput[] = []
    for (const rs of resolved) {
      for (const ri of rs.items) {
        out.push(
          toFailoverInput(
            rs.step,
            ri,
            agentSignalByItem.get(ri.checklistItem.id),
            dispatchByItem.get(ri.checklistItem.id),
          ),
        )
      }
    }
    return out
  }, [resolved, agentSignalByItem, dispatchByItem])

  const attentionCount = useMemo(
    () => failoverInputs.filter(i => checklistItemNeedsAttention(i.overallSignal)).length,
    [failoverInputs],
  )

  const copyFailoverPack = useCallback(async (pack: string, itemId?: string) => {
    try {
      await navigator.clipboard.writeText(pack)
      setCopyState('copied')
      setCopiedItemId(itemId ?? null)
      window.setTimeout(() => {
        setCopyState('idle')
        setCopiedItemId(null)
      }, 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 2000)
    }
  }, [])

  const handleAskAiAll = useCallback(() => {
    void copyFailoverPack(buildChecklistCursorFailoverPack(failoverInputs))
  }, [copyFailoverPack, failoverInputs])

  const handleAskAiItem = useCallback(
    (input: ChecklistFailoverItemInput) => {
      void copyFailoverPack(buildChecklistCursorFailoverPrompt(input), input.item.id)
    },
    [copyFailoverPack],
  )

  const visibleSteps = useMemo(() => {
    if (!failingOnly) return resolved
    return resolved
      .map(rs => ({
        ...rs,
        items: rs.items.filter(
          i => i.overallSignal === 'fail' || i.overallSignal === 'degraded',
        ),
      }))
      .filter(rs => rs.items.length > 0 || rs.stepSignal === 'fail' || rs.stepSignal === 'degraded')
  }, [resolved, failingOnly])

  return (
    <div
      className={cn(
        compactColumns
          ? 'min-w-0'
          : 'mt-1.5 rounded-md border border-border/60 bg-background/60 px-2.5 py-2',
      )}
    >
      <ChecklistKPIHeader
        okItems={okItems}
        totalItems={totalItems}
        failItems={failItems}
        streak={streak}
        newFailHint={newFailHint}
        onChecklistCheck={onChecklistCheck}
        checkBusy={checkBusy}
        proberHint={proberHint}
        checklistCheckTitle={checklistCheckTitle}
        checklistCheckDisabled={checklistCheckDisabled}
        checklistCheckPending={checklistCheckPending}
        checklistCheckError={checklistCheckError}
        attentionCount={attentionCount}
        copyState={copyState}
        copiedItemId={copiedItemId}
        onAskAiAll={handleAskAiAll}
        checklistItemFixError={checklistItemFixError}
        failingOnly={failingOnly}
        onToggleFailingOnly={() => setFailingOnly(v => !v)}
        remediatingPhase={remediatingPhase}
        showMeta={showMeta}
        onToggleShowMeta={() => setShowMeta(v => !v)}
        compactColumns={compactColumns}
        coverage={coverage}
        nowMs={nowMs}
        headerProgress={headerProgress}
        onOpenFullOperatorPlane={onOpenFullOperatorPlane}
      />

      <ChecklistSection
        showMeta={showMeta}
        visibleSteps={visibleSteps}
        failingOnly={failingOnly}
        compactColumns={compactColumns}
        activeFlashStepId={activeFlashStepId}
        remediatingStepIds={remediatingStepIds}
        checklistItemFixActiveId={checklistItemFixActiveId}
        ambientJobScope={ambientJobScope}
        dispatchByItem={dispatchByItem}
        agentSignalByItem={agentSignalByItem}
        jobs={jobs}
        onOpenDispatchJob={onOpenDispatchJob}
        onOpenOperateQueue={onOpenOperateQueue}
        onChecklistItemFix={onChecklistItemFix}
        checklistItemFixPending={checklistItemFixPending}
        checklistItemFixDisabled={checklistItemFixDisabled}
        checklistItemFixTitle={checklistItemFixTitle}
        copyState={copyState}
        copiedItemId={copiedItemId}
        onAskAiItem={handleAskAiItem}
        onFlashStep={onFlashStep}
        coverage={coverage}
      />
    </div>
  )
}
