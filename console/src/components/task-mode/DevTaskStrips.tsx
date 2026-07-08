import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { ClipboardList, Code2 } from 'lucide-react'
import { fetchDevAgentStatus } from '@/api/devAgent'
import type { ProgramDetailResponse } from '@/api/programsTypes'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import { TaskBriefingLauncher } from '@/components/task-mode/TaskBriefingLauncher'
import { TaskDevAgentStatus } from '@/components/task-mode/TaskDevAgentStatus'
import type { TaskModeDef } from '@/lib/task-mode/types'
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
  onOpenBriefing?: (opts?: BriefingUrlState) => void
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
  onOpenBriefing,
}: DevTaskStripsProps) {
  const dev = mode.dev
  if (dev == null) return null

  const devAgentQ = useQuery({
    queryKey: ['dev-agent', 'status'],
    queryFn: fetchDevAgentStatus,
    refetchInterval: 5000,
  })

  const briefingMode: TaskModeDef =
    resolvedProgramId != null && resolvedProgramId !== dev.programId
      ? {
          ...mode,
          dev: { ...dev, programId: resolvedProgramId },
        }
      : mode

  return (
    <div className="flex flex-col gap-3">
      <OpsSection title="Briefing → Dev Agent → Delivery Board">
        <div className="flex flex-col gap-3 p-3">
          <TaskBriefingLauncher mode={briefingMode} onOpenBriefing={onOpenBriefing} />
          <TaskDevAgentStatus
            status={devAgentQ.data}
            loading={devAgentQ.isLoading}
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
