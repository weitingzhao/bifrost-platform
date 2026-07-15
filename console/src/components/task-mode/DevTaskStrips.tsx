import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { ClipboardList, Code2 } from 'lucide-react'
import type { DevAgentStatusResponse } from '@/api/devAgentTypes'
import type { ProgramDetailResponse } from '@/api/programsTypes'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import { TaskBriefingLauncher } from '@/components/task-mode/TaskBriefingLauncher'
import { TaskDevAgentStatus } from '@/components/task-mode/TaskDevAgentStatus'
import type { InlineBriefingPackResult } from '@/hooks/useInlineBriefingPack'
import type { TaskModeDef, TaskPhaseDef, TaskPhaseStatus } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'

export type DevTaskStripsProps = {
  mode: TaskModeDef
  canOperate?: boolean
  programDetail?: ProgramDetailResponse
  programLoading?: boolean
  programError?: Error | null
  resolvedProgramId?: string
  createPending?: boolean
  onCreateProgram?: () => void
  onCreateNewInstance?: () => void
  onNavigate: (tabId: string) => void
  /** Inline scoped pack — primary CTA stays on Task CC. */
  inlineBriefingPack: InlineBriefingPackResult
  /** Optional deep-dive into the global aggregate Briefing page. */
  onOpenFullBriefing?: (opts?: BriefingUrlState) => void
  onBriefingOpened?: () => void
  /** Lifted Dev Agent status from TaskControlCenter (F2) — avoid duplicate query. */
  devAgentStatus?: DevAgentStatusResponse
  devAgentLoading?: boolean
  /** Current playbook step (F11). */
  phases?: TaskPhaseDef[]
  phaseStatuses?: Record<string, TaskPhaseStatus>
}

function firstIncompletePhase(
  phases: TaskPhaseDef[],
  statuses: Record<string, TaskPhaseStatus>,
): TaskPhaseDef | null {
  for (const p of phases) {
    if (statuses[p.id] !== 'done') return p
  }
  return null
}

export function DevTaskStrips({
  mode,
  canOperate = false,
  programDetail,
  programLoading,
  programError,
  resolvedProgramId,
  createPending,
  onCreateProgram,
  onCreateNewInstance,
  onNavigate,
  inlineBriefingPack,
  onOpenFullBriefing,
  onBriefingOpened,
  devAgentStatus,
  devAgentLoading,
  phases = [],
  phaseStatuses = {},
}: DevTaskStripsProps) {
  const dev = mode.dev
  if (dev == null) return null

  const briefingMode: TaskModeDef =
    resolvedProgramId != null && resolvedProgramId !== dev.programId
      ? {
          ...mode,
          dev: { ...dev, programId: resolvedProgramId },
        }
      : mode

  const currentPhase =
    phases.find(p => phaseStatuses[p.id] === 'active') ??
    firstIncompletePhase(phases, phaseStatuses)
  const signed = programDetail?.program.phases_signed ?? programDetail?.program.signed ?? 0
  const phaseCount = programDetail?.program.phase_count ?? 0

  return (
    <div className="flex flex-col gap-3">
      {currentPhase != null && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-secondary px-3 py-2">
          <span className="text-[var(--text-dense-meta)] text-muted-foreground">Current step:</span>
          <span className="text-[var(--text-dense-label)] font-semibold">{currentPhase.title}</span>
          {currentPhase.id !== 'briefing' && currentPhase.navigateTab != null && (
            <Button
              variant="secondary"
              size="xs"
              onClick={() => onNavigate(currentPhase.navigateTab!)}
            >
              Open →
            </Button>
          )}
        </div>
      )}

      <OpsSection title="Dev loop">
        <div className="flex flex-col gap-3 p-3">
          <TaskBriefingLauncher
            mode={briefingMode}
            programId={resolvedProgramId}
            inlinePack={inlineBriefingPack}
            onOpenFullBriefing={onOpenFullBriefing}
            onBriefingOpened={onBriefingOpened}
          />
          <TaskDevAgentStatus
            status={devAgentStatus}
            loading={devAgentLoading}
            onOpenDevAgent={() => onNavigate('dev-agent')}
          />
          {programError != null && (
            <OpsFeedback variant="error" title="Program instance unavailable">
              {programError.message}
            </OpsFeedback>
          )}
          {programDetail != null && (
            <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
              <div className="flex flex-wrap items-center gap-2">
                <ClipboardList size={16} />
                <span className="text-[var(--text-dense-label)] font-semibold">Linked program</span>
                <DenseTag variant="neutral">{programDetail.program.id}</DenseTag>
                <DenseTag variant={programDetail.program.complete ? 'success' : 'warning'}>
                  {signed}/{phaseCount} signed
                </DenseTag>
                <StatusLamp
                  value={programDetail.program.complete ? 'ok' : 'degraded'}
                  kind="reach"
                />
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
                {programDetail.program.description}
              </p>
              <div className="mt-2 flex flex-wrap gap-2">
                <Button variant="ghost" size="xs" onClick={() => onNavigate('delivery-board')}>
                  Delivery Board →
                </Button>
                {onCreateNewInstance != null && (
                  <Button
                    variant="secondary"
                    size="xs"
                    disabled={createPending}
                    onClick={onCreateNewInstance}
                  >
                    {createPending ? 'Creating…' : 'New instance'}
                  </Button>
                )}
              </div>
            </div>
          )}
          {programLoading && (
            <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">Loading program…</p>
          )}
          {!programLoading && programDetail == null && dev.templateId != null && (
            <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
              <div className="flex items-center gap-2">
                <Code2 size={16} />
                <span className="text-[var(--text-dense-label)] font-semibold">Program template</span>
                <DenseTag variant="info">{dev.templateId}</DenseTag>
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
                No Delivery Board instance linked yet — create one from the {dev.templateId} template.
              </p>
              {!canOperate && (
                <OpsFeedback variant="warning" title="Operator authentication required">
                  Authenticate as operator to create program instance from template.
                </OpsFeedback>
              )}
              {onCreateProgram != null && (
                <Button
                  variant="secondary"
                  size="xs"
                  className="mt-2"
                  disabled={createPending || !canOperate}
                  onClick={onCreateProgram}
                >
                  {createPending ? 'Creating…' : 'Create program instance'}
                </Button>
              )}
            </div>
          )}
        </div>
      </OpsSection>
    </div>
  )
}
