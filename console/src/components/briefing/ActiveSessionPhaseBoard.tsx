import {
  Button,
  CollapsibleChevron,
  CollapsibleGroup,
  CollapsibleGroupBody,
  CollapsibleGroupHeader,
  CollapsibleGroupStats,
  CollapsibleGroupTitle,
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
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { CollapseExpandIcon } from '@/components/layout/CollapseExpandIcon'
import { collapseExpandAriaLabel } from '@/components/layout/collapseExpandAria'
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
import { isProgramSessionReleased, queueItemToBriefingStatus } from '@/lib/briefing/briefingStatus'
import type { QueueItem, WorkLane } from '@/lib/briefing/workLanes'
import type { AuditRecord } from '@/api/auditTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { phaseJoinKey } from '@/lib/briefing/phaseJoinKey'

const PHASE_TABLE_COL_COUNT = 6

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
  const noGate = phase.sign_off?.required === false
  const st = phase.progress?.status
  const progressDone = st === 'done' || st === 'verify_passed' || phase.status === 'done'
  if (noGate && progressDone) return 'Done (no gate)'
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
                Owner sign-off
              </Button>
            ) : (
              <span className="text-dense-caption text-muted-foreground">Admin auth required</span>
            )
          )}
        </DenseTableCell>
      </DenseTableRow>
      {expanded && hasDetail && (
        <DenseTableRow className="bg-background/60">
          <DenseTableCell colSpan={PHASE_TABLE_COL_COUNT}>
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

function PhaseGridFold({
  programId,
  rowCount,
  unsignedCount,
  children,
}: {
  programId: string
  rowCount: number
  unsignedCount: number
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    setOpen(false)
  }, [programId])

  return (
    <CollapsibleGroup variant="card" className="mb-0">
      <CollapsibleGroupHeader
        expanded={open}
        onToggle={() => setOpen(v => !v)}
        aria-label={collapseExpandAriaLabel(open, 'phase grid')}
      >
        <CollapsibleChevron expanded={open} />
        <CollapsibleGroupTitle>Phase grid</CollapsibleGroupTitle>
        <CollapsibleGroupStats>
          {rowCount} phases
          {unsignedCount > 0 ? ` · ${unsignedCount} unsigned` : ''}
        </CollapsibleGroupStats>
      </CollapsibleGroupHeader>
      {open ? (
        <CollapsibleGroupBody className="px-0 pb-0">
          <div className="min-w-0 overflow-x-auto border-t border-border">{children}</div>
        </CollapsibleGroupBody>
      ) : null}
    </CollapsibleGroup>
  )
}

function ProgramBoardShell({
  program,
  signedCount,
  gateCount,
  phasesDone,
  phaseTotal,
  defaultOpen,
  children,
}: {
  program: ProgramSummary
  signedCount: number
  gateCount: number
  phasesDone: number
  phaseTotal: number
  defaultOpen: boolean
  children: ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  useEffect(() => {
    setOpen(defaultOpen)
  }, [program.id, defaultOpen])

  const title = program.label ?? program.title
  const released = isProgramSessionReleased(program)
  const gatesOk = gateCount > 0 && signedCount === gateCount
  const closePending = !released && gatesOk

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <button
        type="button"
        className="flex w-full min-w-0 items-start gap-2 text-left"
        aria-expanded={open}
        aria-label={collapseExpandAriaLabel(open, title)}
        onClick={() => setOpen(v => !v)}
      >
        <div className="min-w-0 flex-1">
          <p className="briefing-section-kicker m-0">Phases</p>
          <h3 className="m-0 mt-0.5 text-sm font-semibold">{title}</h3>
          {!open && (
            <p className="m-0 mt-1 text-dense-caption text-muted-foreground">
              {signedCount}/{gateCount} gates signed
              {phaseTotal > 0 ? ` · ${phasesDone}/${phaseTotal} phases done` : ''}
              {closePending ? ' · close pending' : released ? ' · closed' : ''}
            </p>
          )}
        </div>
        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
          <DenseTag variant={gatesOk ? 'success' : 'warning'}>
            {signedCount}/{gateCount} gates signed
          </DenseTag>
          {phaseTotal > 0 && (
            <DenseTag variant={phasesDone === phaseTotal ? 'success' : 'neutral'}>
              {phasesDone}/{phaseTotal} phases done
            </DenseTag>
          )}
          {closePending && <DenseTag variant="warning">Close pending</DenseTag>}
          {released && <DenseTag variant="success">Closed</DenseTag>}
          <CollapseExpandIcon open={open} className="mt-0.5" />
        </div>
      </button>
      {open ? children : null}
    </div>
  )
}

function ProgramUnifiedBoard({
  program,
  queue,
  allowSignOff,
  defaultOpen,
}: {
  program: ProgramSummary
  queue: QueueItem[]
  allowSignOff: boolean
  defaultOpen: boolean
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

  const signedCount =
    detail?.phases.filter(p => p.signed_off).length ??
    program.signed ??
    program.phases_signed ??
    0
  const gateCount =
    detail?.phases.filter(p => p.sign_off?.required !== false).length ??
    program.sign_off_required_count ??
    program.phase_count
  const phasesDone = detail?.program.phases_done ?? program.phases_done
  const phaseTotal = detail?.phases.length ?? program.phase_count
  const panelSignOffOnly = program.id === 'vision' || program.id === 'mission-signal'
  const tableAllowSignOff = allowSignOff && !panelSignOffOnly

  const shell = {
    program,
    signedCount,
    gateCount,
    phasesDone,
    phaseTotal,
    defaultOpen,
  }

  if (panelSignOffOnly) {
    return (
      <ProgramBoardShell {...shell}>
        <DeliveryBoardProgramPanels programId={program.id} allowSignOff={allowSignOff} />
      </ProgramBoardShell>
    )
  }

  if (detailQuery.isLoading) {
    return (
      <ProgramBoardShell {...shell}>
        <p className="m-0 text-dense-meta text-muted-foreground">Loading phases…</p>
      </ProgramBoardShell>
    )
  }
  if (detailQuery.isError || detail == null) {
    return (
      <ProgramBoardShell {...shell}>
        <p className="m-0 text-dense-meta text-destructive">Failed to load program phases.</p>
      </ProgramBoardShell>
    )
  }

  return (
    <>
    <ProgramBoardShell {...shell}>
      <p className="m-0 max-w-3xl text-dense-caption text-muted-foreground">
        Plan status and Owner sign-off on one row per phase. Phase work runs in Cursor IDE Agent.
      </p>

      <PhaseGridFold
        programId={program.id}
        rowCount={joined.rows.length + joined.orphanQueue.length}
        unsignedCount={detail.phases.filter(p => !p.signed_off && p.sign_off?.required !== false).length}
      >
        <DenseDataTable wrapClassName="rounded-none border-0">
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-8" />
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Plan</DenseTableHead>
              <DenseTableHead>Delivery</DenseTableHead>
              <DenseTableHead>Signed at</DenseTableHead>
              <DenseTableHead>Owner</DenseTableHead>
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
      </PhaseGridFold>

      {allowSignOff && <PostCompletionPendingPanel programId={program.id} />}
    </ProgramBoardShell>

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
    </>
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
    return [...linked].sort((a, b) => {
      const aOpen = !isProgramSessionReleased(a)
      const bOpen = !isProgramSessionReleased(b)
      if (aOpen !== bOpen) return aOpen ? -1 : 1
      if (focusedProgramId != null) {
        if (a.id === focusedProgramId) return -1
        if (b.id === focusedProgramId) return 1
      }
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
      {lanePrograms.map((program, index) => (
        <div
          key={program.id}
          className={index > 0 ? 'border-t border-border/60 pt-4' : undefined}
        >
          <ProgramUnifiedBoard
            program={program}
            queue={queue}
            allowSignOff={allowSignOff}
            defaultOpen={!isProgramSessionReleased(program)}
          />
        </div>
      ))}
    </section>
  )
}
