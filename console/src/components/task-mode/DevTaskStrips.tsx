import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { ClipboardList, Code2 } from 'lucide-react'
import type { DevAgentStatusResponse } from '@/api/devAgentTypes'
import type { ProgramDetailResponse } from '@/api/programsTypes'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import { TaskBriefingLauncher } from '@/components/task-mode/TaskBriefingLauncher'
import { TaskDevAgentStatus } from '@/components/task-mode/TaskDevAgentStatus'
import type { InlineBriefingPackResult } from '@/hooks/useInlineBriefingPack'
import type { TaskModeDef } from '@/lib/task-mode/types'
import { BUILD_DEV_LOOP_ELEMENT_ID } from '@/lib/task-mode/buildWorkbenchVerdict'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import { boardCloseTag, isProgramCatalogComplete } from '@/lib/briefing/programClose'

export type DevTaskStripsProps = {
  mode: TaskModeDef
  canOperate?: boolean
  programDetail?: ProgramDetailResponse
  programLoading?: boolean
  programError?: Error | null
  resolvedProgramId?: string
  createPending?: boolean
  hasActiveSession?: boolean
  activeLane?: string
  canCreateProgram?: boolean
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
}

function isTemplateMissing(err: Error | null | undefined): boolean {
  if (err == null) return false
  const msg = err.message.toLowerCase()
  return msg.includes('template not found') || msg.includes('template_id')
}

export function DevTaskStrips({
  mode,
  canOperate = false,
  programDetail,
  programLoading,
  programError,
  resolvedProgramId,
  createPending,
  hasActiveSession = false,
  activeLane,
  canCreateProgram = false,
  onCreateProgram,
  onCreateNewInstance,
  onNavigate,
  inlineBriefingPack,
  onOpenFullBriefing,
  onBriefingOpened,
  devAgentStatus,
  devAgentLoading,
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

  const signed = programDetail?.program.phases_signed ?? programDetail?.program.signed ?? 0
  const phaseCount = programDetail?.program.phase_count ?? 0
  const catalogComplete =
    programDetail != null ? isProgramCatalogComplete(programDetail.program) : false
  const closeTag = programDetail != null ? boardCloseTag(programDetail.program) : null
  const templateMissing = isTemplateMissing(programError)
  const needsProgram =
    hasActiveSession && !programLoading && programDetail == null && dev.templateId != null

  return (
    <div id={BUILD_DEV_LOOP_ELEMENT_ID} className="flex flex-col gap-3">
      <OpsSection title="Dev loop">
        <div className="flex flex-col gap-3 p-3">
          <TaskBriefingLauncher
            mode={briefingMode}
            programId={resolvedProgramId}
            inlinePack={inlineBriefingPack}
            hasActiveSession={hasActiveSession}
            programSigned={signed}
            programPhaseCount={phaseCount}
            onOpenFullBriefing={onOpenFullBriefing}
            onBriefingOpened={onBriefingOpened}
            onNavigate={onNavigate}
          />
          <TaskDevAgentStatus
            status={devAgentStatus}
            loading={devAgentLoading}
            onOpenDevAgent={() => onNavigate('dev-agent')}
          />

          {/* Single program-binding strip — follows Active Session */}
          {programDetail != null ? (
            <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
              <div className="flex flex-wrap items-center gap-2">
                <ClipboardList size={16} />
                <span className="text-[var(--text-dense-label)] font-semibold">Linked program</span>
                <DenseTag variant="neutral">{programDetail.program.id}</DenseTag>
                {programDetail.program.lane_id != null && (
                  <DenseTag variant="info">{programDetail.program.lane_id}</DenseTag>
                )}
                <DenseTag variant={catalogComplete ? 'success' : 'warning'}>
                  {catalogComplete
                    ? `${signed}/${phaseCount} signed`
                    : closeTag != null
                      ? 'Close pending'
                      : `${signed}/${phaseCount} signed`}
                </DenseTag>
                <StatusLamp
                  value={catalogComplete ? 'ok' : 'degraded'}
                  kind="reach"
                />
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
                {programDetail.program.description}
              </p>
              {programError != null && (
                <OpsFeedback variant="error" title="Program create failed">
                  {programError.message}
                </OpsFeedback>
              )}
              <div className="mt-2 flex flex-wrap gap-2">
                <Button variant="ghost" size="xs" onClick={() => onNavigate('delivery-board')}>
                  Delivery Board →
                </Button>
                {onCreateNewInstance != null && canCreateProgram && (
                  <Button
                    variant="secondary"
                    size="xs"
                    disabled={createPending || !canOperate}
                    onClick={onCreateNewInstance}
                    title={
                      !canOperate
                        ? 'Authenticate as operator'
                        : `Create a new instance for lane ${activeLane ?? 'session'}`
                    }
                  >
                    {createPending ? 'Creating…' : 'New instance for lane'}
                  </Button>
                )}
              </div>
            </div>
          ) : programLoading ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
              Loading program…
            </p>
          ) : !hasActiveSession ? (
            <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
              <div className="flex items-center gap-2">
                <Code2 size={16} />
                <span className="text-[var(--text-dense-label)] font-semibold">
                  No Active Session
                </span>
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
                Select a lane in Agent Briefing and Copy session (or Full Briefing) before linking a
                Delivery Board program — TCC follows the Active Session.
              </p>
              <Button
                variant="secondary"
                size="xs"
                className="mt-2"
                onClick={() => onNavigate('briefing')}
              >
                Open Briefing →
              </Button>
            </div>
          ) : needsProgram ? (
            <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
              <div className="flex flex-wrap items-center gap-2">
                <Code2 size={16} />
                <span className="text-[var(--text-dense-label)] font-semibold">
                  No program for this session
                </span>
                {activeLane != null && <DenseTag variant="info">{activeLane}</DenseTag>}
                {dev.templateId != null && (
                  <DenseTag variant="neutral">{dev.templateId}</DenseTag>
                )}
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
                Create a Delivery Board instance for the Active Session lane. This becomes the shared
                program for Briefing, phases, and sign-off.
              </p>
              {templateMissing && (
                <OpsFeedback variant="error" title="Program template unavailable">
                  {programError?.message ?? 'Template not found — check API programs/templates.'}
                </OpsFeedback>
              )}
              {!templateMissing && programError != null && (
                <OpsFeedback variant="error" title="Program create failed">
                  {programError.message}
                </OpsFeedback>
              )}
              {!canOperate && (
                <OpsFeedback variant="warning" title="Operator authentication required">
                  Authenticate as operator to create a program instance from template.
                </OpsFeedback>
              )}
              {onCreateProgram != null && (
                <Button
                  variant="secondary"
                  size="xs"
                  className="mt-2"
                  disabled={createPending || !canOperate || !canCreateProgram || templateMissing}
                  onClick={onCreateProgram}
                >
                  {createPending
                    ? 'Creating…'
                    : `Create program for ${activeLane ?? 'session'}`}
                </Button>
              )}
            </div>
          ) : null}
        </div>
      </OpsSection>
    </div>
  )
}
