import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  type DenseTagVariant,
} from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  fetchDeliveryBoardPrograms,
  fetchProgramDetail,
  PROGRAMS_BOARD_QUERY_KEY,
  signoffProgramPhase,
} from '@/api/programs'
import type { ProgramPhaseDetail, ProgramSummary } from '@/api/programsTypes'
import { DeliveryBoardProgramPanels } from '@/components/delivery/DeliveryBoardProgramPanels'
import { PostCompletionPendingPanel } from '@/components/delivery/PostCompletionPendingPanel'
import { BriefingStatusBadge } from '@/components/briefing/BriefingStatusChrome'
import { TaskQueuePanel } from '@/components/briefing/TaskQueuePanel'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { queueItemToBriefingStatus } from '@/lib/briefing/briefingStatus'
import type { QueueItem, WorkLane } from '@/lib/briefing/workLanes'
import type { AuditRecord } from '@/api/auditTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'

/** Normalize `P0` / `md-expand-p0` / `foo-p0` → `p0` for Plan↔Delivery join. */
export function phaseJoinKey(id: string): string {
  const lower = id.trim().toLowerCase()
  const m = lower.match(/(?:^|[-_])(p\d+)$/)
  return m?.[1] ?? lower
}

function matchQueueItem(phase: ProgramPhaseDetail, queue: QueueItem[]): QueueItem | undefined {
  const key = phaseJoinKey(phase.id)
  return (
    queue.find(q => phaseJoinKey(q.id) === key) ??
    queue.find(q => q.id === phase.id) ??
    queue.find(q => q.label.toLowerCase().startsWith(`${phase.id.toLowerCase()} `))
  )
}

function deliveryStatusVariant(phase: ProgramPhaseDetail): DenseTagVariant {
  if (phase.signed_off) return 'success'
  const st = phase.progress?.status
  if (st === 'done' || st === 'verify_passed') return 'info'
  if (st === 'verify_failed') return 'danger'
  if (st === 'verify_pending' || st === 'in_progress') return 'warning'
  return 'neutral'
}

function deliveryStatusLabel(phase: ProgramPhaseDetail): string {
  if (phase.signed_off) return 'Signed'
  const st = phase.progress?.status
  if (st === 'done' || st === 'verify_passed') return 'Ready for sign-off'
  if (st) return st.replace(/_/g, ' ')
  return phase.status
}

function planStatusLabel(item: QueueItem | undefined): string {
  if (item == null) return '—'
  if (item.status === 'ready_for_signoff') return 'delivered'
  return item.status.replace(/_/g, ' ')
}

interface JoinedPhaseRow {
  phase: ProgramPhaseDetail
  queueItem?: QueueItem
}

function joinPhases(phases: ProgramPhaseDetail[], queue: QueueItem[]): {
  rows: JoinedPhaseRow[]
  orphanQueue: QueueItem[]
} {
  const used = new Set<string>()
  const rows: JoinedPhaseRow[] = phases.map(phase => {
    const queueItem = matchQueueItem(phase, queue)
    if (queueItem != null) used.add(queueItem.id)
    return { phase, queueItem }
  })
  const orphanQueue = queue.filter(q => !used.has(q.id))
  return { rows, orphanQueue }
}

function UnifiedPhaseRow({
  phase,
  queueItem,
  canAdmin,
  allowSignOff,
  onSignOff,
}: {
  phase: ProgramPhaseDetail
  queueItem?: QueueItem
  canAdmin: boolean
  allowSignOff: boolean
  onSignOff: (phaseId: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const checklist = phase.sign_off?.checklist ?? phase.acceptance ?? []
  const hasDetail =
    checklist.length > 0 ||
    (phase.verify_cmd != null && phase.verify_cmd !== '') ||
    phase.progress?.summary != null ||
    (queueItem?.note != null && queueItem.note !== '') ||
    (phase.depends_on?.length ?? 0) > 0

  const planBriefing =
    queueItem != null ? queueItemToBriefingStatus(queueItem.status) : null

  return (
    <>
      <DenseTableRow>
        <DenseTableCell className="w-8">
          {hasDetail ? (
            <button
              type="button"
              className="inline-flex text-muted-foreground hover:text-foreground"
              onClick={() => setExpanded(v => !v)}
              aria-label={expanded ? 'Collapse phase detail' : 'Expand phase detail'}
            >
              {expanded ? (
                <ChevronDown className="h-3.5 w-3.5" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5" />
              )}
            </button>
          ) : null}
        </DenseTableCell>
        <DenseTableCell>
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="font-medium">{phase.id}</span>
            <span className="text-dense-meta text-muted-foreground">{phase.title}</span>
            {queueItem != null && (
              <code className="mt-0.5 max-w-full truncate font-mono text-dense-caption text-muted-foreground">
                {queueItem.id}
              </code>
            )}
          </div>
        </DenseTableCell>
        <DenseTableCell>
          <div className="flex flex-wrap items-center gap-1.5">
            {planBriefing != null ? (
              <BriefingStatusBadge status={planBriefing} />
            ) : (
              <span className="text-dense-caption text-muted-foreground">—</span>
            )}
            {queueItem?.status === 'ready_for_signoff' && (
              <DenseTag variant="warning">{planStatusLabel(queueItem)}</DenseTag>
            )}
          </div>
        </DenseTableCell>
        <DenseTableCell>
          <DenseTag variant={deliveryStatusVariant(phase)}>{deliveryStatusLabel(phase)}</DenseTag>
        </DenseTableCell>
        <DenseTableCell className="text-dense-meta text-muted-foreground whitespace-nowrap">
          {phase.signed_off_at ?? '—'}
        </DenseTableCell>
        <DenseTableCell>
          {!phase.signed_off && allowSignOff && phase.sign_off?.required !== false && (
            canAdmin ? (
              <Button type="button" size="sm" variant="outline" onClick={() => onSignOff(phase.id)}>
                Sign off
              </Button>
            ) : (
              <span className="text-dense-caption text-muted-foreground">Admin auth required</span>
            )
          )}
        </DenseTableCell>
      </DenseTableRow>
      {expanded && hasDetail && (
        <DenseTableRow className="bg-background/60">
          <DenseTableCell colSpan={6}>
            <div className="flex flex-col gap-3 px-1 py-2">
              {queueItem?.note != null && queueItem.note !== '' && (
                <div>
                  <p className="text-dense-label font-medium m-0 mb-1">Plan note</p>
                  <p className="text-dense-meta text-muted-foreground m-0">{queueItem.note}</p>
                </div>
              )}
              {phase.progress?.summary && (
                <div>
                  <p className="text-dense-label font-medium m-0 mb-1">Agent progress</p>
                  <p className="text-dense-meta text-muted-foreground m-0">{phase.progress.summary}</p>
                  <p className="text-dense-caption text-muted-foreground m-0 mt-0.5">
                    Updated {phase.progress.updated_at}
                    {phase.progress.verify_passed ? ' · verify passed' : ''}
                  </p>
                </div>
              )}
              {checklist.length > 0 && (
                <div>
                  <p className="text-dense-label font-medium m-0 mb-1">Acceptance checklist</p>
                  <ul className="m-0 list-disc pl-5 text-dense-meta text-muted-foreground">
                    {checklist.map(item => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                </div>
              )}
              {phase.verify_cmd != null && phase.verify_cmd !== '' && (
                <div>
                  <p className="text-dense-label font-medium m-0 mb-1">Verify command</p>
                  <code className="block rounded-md bg-secondary px-2 py-1 text-dense-meta font-mono">
                    {phase.verify_cmd}
                  </code>
                </div>
              )}
              {(phase.depends_on?.length ?? 0) > 0 && (
                <p className="text-dense-meta text-muted-foreground m-0">
                  Depends on: {phase.depends_on?.join(', ')}
                </p>
              )}
            </div>
          </DenseTableCell>
        </DenseTableRow>
      )}
    </>
  )
}

function ProgramUnifiedBoard({
  program,
  queue,
  allowSignOff,
}: {
  program: ProgramSummary
  queue: QueueItem[]
  allowSignOff: boolean
}) {
  const { canAdmin } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [confirmPhaseId, setConfirmPhaseId] = useState<string | null>(null)

  const detailQuery = useQuery({
    queryKey: ['programs', program.id],
    queryFn: () => fetchProgramDetail(program.id),
  })

  const signoffMutation = useMutation({
    mutationFn: (phaseId: string) => signoffProgramPhase(program.id, phaseId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['programs', program.id] })
      void queryClient.invalidateQueries({ queryKey: PROGRAMS_BOARD_QUERY_KEY })
      setConfirmPhaseId(null)
    },
  })

  const detail = detailQuery.data
  const joined = useMemo(
    () => (detail != null ? joinPhases(detail.phases, queue) : { rows: [], orphanQueue: queue }),
    [detail, queue],
  )

  const signedCount = detail?.phases.filter(p => p.signed_off).length ?? 0
  const gateCount =
    detail?.phases.filter(p => p.sign_off?.required !== false).length ??
    program.sign_off_required_count ??
    program.phase_count
  const panelSignOffOnly = program.id === 'vision' || program.id === 'mission-signal'
  const tableAllowSignOff = allowSignOff && !panelSignOffOnly

  if (panelSignOffOnly) {
    return (
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="m-0 text-sm font-semibold">{program.label ?? program.title}</h3>
          <DenseTag variant={signedCount === gateCount && gateCount > 0 ? 'success' : 'warning'}>
            {signedCount}/{gateCount} gates
          </DenseTag>
        </div>
        <DeliveryBoardProgramPanels programId={program.id} allowSignOff={allowSignOff} />
      </div>
    )
  }

  if (detailQuery.isLoading) {
    return <p className="m-0 text-dense-meta text-muted-foreground">Loading phases…</p>
  }
  if (detailQuery.isError || detail == null) {
    return <p className="m-0 text-dense-meta text-destructive">Failed to load program phases.</p>
  }

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="briefing-section-kicker m-0">Phases</p>
          <h3 className="m-0 mt-0.5 text-sm font-semibold">{program.label ?? program.title}</h3>
          <p className="m-0 mt-1 max-w-3xl text-dense-caption text-muted-foreground">
            Plan status and Owner sign-off on one row per phase.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <DenseTag variant={signedCount === gateCount && gateCount > 0 ? 'success' : 'warning'}>
            {signedCount}/{gateCount} gates signed
          </DenseTag>
          <DenseTag
            variant={
              detail.program.phases_done === detail.phases.length && detail.phases.length > 0
                ? 'success'
                : 'neutral'
            }
          >
            {detail.program.phases_done}/{detail.phases.length} phases done
          </DenseTag>
        </div>
      </div>

      <div className="min-w-0 overflow-x-auto rounded-lg border border-[var(--border)]">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-8" />
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Plan</DenseTableHead>
              <DenseTableHead>Delivery</DenseTableHead>
              <DenseTableHead>Signed at</DenseTableHead>
              <DenseTableHead />
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {joined.rows.map(({ phase, queueItem }) => (
              <UnifiedPhaseRow
                key={phase.id}
                phase={phase}
                queueItem={queueItem}
                canAdmin={canAdmin}
                allowSignOff={tableAllowSignOff}
                onSignOff={setConfirmPhaseId}
              />
            ))}
            {joined.orphanQueue.map(item => (
              <DenseTableRow key={`orphan-${item.id}`}>
                <DenseTableCell className="w-8" />
                <DenseTableCell>
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <span className="font-medium">{item.label}</span>
                    <code className="font-mono text-dense-caption text-muted-foreground">
                      {item.id}
                    </code>
                  </div>
                </DenseTableCell>
                <DenseTableCell>
                  <BriefingStatusBadge status={queueItemToBriefingStatus(item.status)} />
                </DenseTableCell>
                <DenseTableCell>
                  <span className="text-dense-caption text-muted-foreground">No program phase</span>
                </DenseTableCell>
                <DenseTableCell>—</DenseTableCell>
                <DenseTableCell />
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </div>

      {allowSignOff && <PostCompletionPendingPanel programId={program.id} />}

      <ConfirmDialog
        open={confirmPhaseId != null}
        title="Confirm phase sign-off"
        message={`Record Owner sign-off for phase ${confirmPhaseId}? This is persisted via platform-api.`}
        confirmLabel="Confirm sign-off"
        confirming={signoffMutation.isPending}
        onConfirm={() => {
          if (confirmPhaseId) signoffMutation.mutate(confirmPhaseId)
        }}
        onCancel={() => setConfirmPhaseId(null)}
      />
    </div>
  )
}

export interface ActiveSessionPhaseBoardProps {
  lane: WorkLane
  queue: QueueItem[]
  focusedProgramId?: string
  allowSignOff?: boolean
  /** Fallback when lane has no linked program (wave-only lanes). */
  context?: OpsContextResponse
  canAdmin?: boolean
  migrateTrackNext?: string | null
  auditRecords?: AuditRecord[]
  auditLoading?: boolean
  onOpenAudit?: () => void
}

/**
 * Unified Plan + Delivery phase table for Active Session.
 * Joins queue items to program phases by P0/P1/… key so one row shows both statuses.
 */
export function ActiveSessionPhaseBoard({
  lane,
  queue,
  focusedProgramId,
  allowSignOff = true,
  context,
  canAdmin = false,
  migrateTrackNext,
  auditRecords = [],
  auditLoading,
  onOpenAudit,
}: ActiveSessionPhaseBoardProps) {
  const programsQuery = useQuery({
    queryKey: PROGRAMS_BOARD_QUERY_KEY,
    queryFn: fetchDeliveryBoardPrograms,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })

  const lanePrograms = useMemo(() => {
    const all = programsQuery.data?.programs ?? []
    const linked = all.filter(p => p.lane_id === lane.id)
    if (focusedProgramId == null) return linked
    return [...linked].sort((a, b) => {
      if (a.id === focusedProgramId) return -1
      if (b.id === focusedProgramId) return 1
      return 0
    })
  }, [programsQuery.data, lane.id, focusedProgramId])

  if (programsQuery.isLoading) {
    return (
      <section className="page-section panel-elevated px-3 py-2.5">
        <p className="m-0 text-dense-caption text-muted-foreground">Loading phase board…</p>
      </section>
    )
  }

  if (lanePrograms.length === 0) {
    return (
      <TaskQueuePanel
        items={queue}
        lane={lane}
        context={context}
        canAdmin={canAdmin}
        migrateTrackNext={migrateTrackNext}
        auditRecords={auditRecords}
        auditLoading={auditLoading}
        onOpenAudit={onOpenAudit}
      />
    )
  }

  return (
    <section className="page-section panel-elevated flex min-w-0 max-w-full flex-col gap-4 overflow-x-hidden px-3 py-2.5">
      {lane.id === 'governance' && (
        <PostCompletionPendingPanel
          programId="dap-smoke-test"
          allowApprove={allowSignOff}
          emphasize
        />
      )}
      {lanePrograms.map(program => (
        <ProgramUnifiedBoard
          key={program.id}
          program={program}
          queue={queue}
          allowSignOff={allowSignOff}
        />
      ))}
    </section>
  )
}
