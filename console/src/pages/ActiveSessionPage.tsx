import { useEffect, useMemo, useState } from 'react'
import { Button, EmptyState } from '@bifrost/ui'
import { Orbit } from 'lucide-react'
import type { AuditRecord } from '@/api/auditTypes'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { ProgramSummary } from '@/api/programsTypes'
import {
  BriefingStatusBadge,
  BriefingStatusLamp,
} from '@/components/briefing/BriefingStatusChrome'
import { briefingLaneListRowClass } from '@/components/briefing/briefingStatusChromeClasses'
import { ActiveSessionPhaseBoard } from '@/components/briefing/ActiveSessionPhaseBoard'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  isGatesComplete,
  isProgramSessionReleased,
  laneLifecycleFromQueue,
  lifecycleToBriefingStatus,
} from '@/lib/briefing/briefingStatus'
import {
  parseActiveSessionFocus,
  writeActiveSessionFocus,
} from '@/lib/briefing/deliveryPipelineNav'
import { computeAllTracks } from '@/lib/briefing/workTracks'
import {
  allWorkLanes,
  buildQueueForLane,
  type LaneId,
  type QueueItem,
  type WorkLane,
} from '@/lib/briefing/workLanes'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { useDeliveryProgramClosure } from '@/hooks/useDeliveryProgramClosure'
import { loadBriefingActiveSession } from '@/lib/briefing/briefingActiveSession'
import { isOpsIssueLane, opsOpenIssueCount } from '@/lib/briefing/activeSessionLaneMode'

interface ActiveSessionPageProps {
  context: OpsContextResponse | undefined
  contextLoading: boolean
  matrices: MatrixResponse[]
  matrixLoading: boolean
  clusterSummary: ClusterSummary | undefined
  auditRecords: AuditRecord[]
  auditLoading: boolean
  onOpenAudit?: () => void
  onOpenBriefing: (opts?: { lane?: LaneId }) => void
  onOpenDeliveryBoard?: (opts?: { laneId?: LaneId }) => void
  onOpenCluster?: () => void
  onOpenOperateQueue?: () => void
  onOpenControlRoom?: () => void
}

interface DoingLaneRow {
  lane: WorkLane
  queue: QueueItem[]
  progress: { done: number; total: number } | null
}

function queueProgress(queue: QueueItem[]): { done: number; total: number } | null {
  if (queue.length === 0) return null
  const done = queue.filter(q => q.status === 'done' || q.status === 'closed').length
  return { done, total: queue.length }
}

function laneAllPhasesSigned(queue: QueueItem[]): boolean {
  if (queue.length === 0) return false
  return queue.every(q => q.status === 'done' || q.status === 'closed')
}

export function ActiveSessionPage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  clusterSummary,
  auditRecords,
  auditLoading,
  onOpenAudit,
  onOpenBriefing,
  onOpenDeliveryBoard,
  onOpenCluster,
  onOpenOperateQueue,
  onOpenControlRoom,
}: ActiveSessionPageProps) {
  const { canAdmin } = usePlatformAuth()
  const operateQueueQuery = useOperateQueue()
  const { programsReleasedFor, openProgramsFor, programsReady, programs } = useDeliveryProgramClosure()
  const initialFocus = useMemo(() => parseActiveSessionFocus(), [])
  const [selectedLaneId, setSelectedLaneId] = useState<LaneId | null>(
    () => initialFocus.laneId ?? null,
  )

  const dataReady = !contextLoading && !matrixLoading

  const doingLanes = useMemo((): DoingLaneRow[] => {
    if (!dataReady || !programsReady) return []
    return allWorkLanes()
      .map(lane => {
        const queue = buildQueueForLane(lane.id, context, matrices, clusterSummary, programs)
        return { lane, queue, progress: queueProgress(queue) }
      })
      .filter(
        row =>
          laneLifecycleFromQueue(row.queue, {
            programsReleased: programsReleasedFor(row.lane.id),
          }) === 'active',
      )
  }, [dataReady, programsReady, context, matrices, clusterSummary, programsReleasedFor, programs])

  useEffect(() => {
    if (doingLanes.length === 0) {
      if (selectedLaneId != null) setSelectedLaneId(null)
      return
    }
    if (selectedLaneId != null && doingLanes.some(r => r.lane.id === selectedLaneId)) return
    const preferred =
      initialFocus.laneId != null && doingLanes.some(r => r.lane.id === initialFocus.laneId)
        ? initialFocus.laneId
        : doingLanes[0].lane.id
    setSelectedLaneId(preferred)
  }, [doingLanes, selectedLaneId, initialFocus.laneId])

  useEffect(() => {
    if (selectedLaneId == null) return
    writeActiveSessionFocus({
      laneId: selectedLaneId,
      programId: initialFocus.programId,
    })
  }, [selectedLaneId, initialFocus.programId])

  const selectedRow = useMemo(
    () => doingLanes.find(r => r.lane.id === selectedLaneId) ?? null,
    [doingLanes, selectedLaneId],
  )

  const migrateTrackNext = useMemo(() => {
    const tracks = computeAllTracks(
      context,
      matrices,
      clusterSummary?.failing_pods,
      clusterSummary?.reachability,
      operateQueueQuery.data?.open,
    )
    return tracks.find(t => t.id === 'migrate')?.nextStep ?? null
  }, [context, matrices, clusterSummary, operateQueueQuery.data?.open])

  const selectedQueueDone = selectedRow != null && laneAllPhasesSigned(selectedRow.queue)
  const selectedProgramsReleased =
    selectedRow != null ? programsReleasedFor(selectedRow.lane.id) !== false : true
  const selectedComplete = selectedQueueDone && selectedProgramsReleased

  const selectedLinkedProgramCount = useMemo(() => {
    if (selectedRow == null) return 0
    return programs.filter(
      p => p.lane_id === selectedRow.lane.id && !isProgramSessionReleased(p),
    ).length
  }, [selectedRow, programs])

  const selectedOpsIssueMode =
    selectedRow != null &&
    (isOpsIssueLane(selectedRow.lane) ||
      (selectedLinkedProgramCount === 0 && selectedRow.lane.trackType === 'maintain'))

  if (!dataReady || !programsReady) {
    return (
      <section className="page-section panel-elevated flex min-h-[12rem] flex-col items-center justify-center gap-1 px-4 py-8 text-center">
        <p className="briefing-section-kicker m-0">In Flight</p>
        <h2 className="m-0 text-sm font-semibold">Loading Doing lanes…</h2>
        <p className="m-0 max-w-sm text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Waiting for spine and matrix so queue progress can be tracked.
        </p>
      </section>
    )
  }

  if (doingLanes.length === 0) {
    return (
      <section className="page-section panel-elevated px-4 py-6">
        <EmptyState
          icon={<Orbit />}
          title="No active sessions — start work from Briefing"
          description="Pack and Launch a Ready or Planned lane, then continue execution here."
          action={
            <Button size="sm" variant="outline" onClick={() => onOpenBriefing()}>
              Open Briefing
            </Button>
          }
        />
      </section>
    )
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <section className="page-section panel-elevated w-full min-w-0 px-3 py-2.5">
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <div className="min-w-0">
            <p className="briefing-section-kicker m-0">Doing</p>
            <h2 className="m-0 mt-0.5 text-sm font-semibold">Active lanes</h2>
          </div>
          <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {doingLanes.length} in progress — select a lane, then work the board below
            {selectedOpsIssueMode
              ? ' (issue queue + unblock actions).'
              : ' (full-width phase table).'}
          </p>
        </div>
        <div className="mt-2.5 flex min-w-0 flex-wrap gap-1.5">
          {doingLanes.map(({ lane, queue, progress }) => {
            const selected = lane.id === selectedLaneId
            const status = lifecycleToBriefingStatus('active')
            const opsMode = isOpsIssueLane(lane)
            const openIssues = opsMode ? opsOpenIssueCount(queue) : null
            return (
              <button
                key={lane.id}
                type="button"
                onClick={() => setSelectedLaneId(lane.id)}
                className={briefingLaneListRowClass(selected)}
              >
                <BriefingStatusLamp status={status} />
                <span
                  className={[
                    'max-w-[16rem] truncate text-[var(--text-dense-meta)] sm:max-w-[22rem]',
                    selected
                      ? 'font-semibold text-[var(--foreground)]'
                      : 'font-medium text-[var(--muted-foreground)]',
                  ].join(' ')}
                  title={lane.label}
                >
                  {lane.label}
                </span>
                {openIssues != null ? (
                  <span className="shrink-0 font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {openIssues} open
                  </span>
                ) : (
                  progress != null && (
                    <span className="shrink-0 font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                      {progress.done}/{progress.total}
                    </span>
                  )
                )}
                <BriefingStatusBadge status={status} />
              </button>
            )
          })}
        </div>
      </section>

      {selectedRow == null ? (
        <section className="page-section panel-elevated px-4 py-6">
          <EmptyState
            title="Select a Doing lane"
            description="Choose a lane above to track its queue and delivery sign-off."
          />
        </section>
      ) : (
        <ActiveSessionDetail
          lane={selectedRow.lane}
          queue={selectedRow.queue}
          progress={selectedRow.progress}
          canAdmin={canAdmin}
          context={context}
          migrateTrackNext={migrateTrackNext}
          auditRecords={auditRecords}
          auditLoading={auditLoading}
          onOpenAudit={onOpenAudit}
          focusedProgramId={
            initialFocus.programId ?? loadBriefingActiveSession()?.programId
          }
          selectedComplete={selectedComplete}
          signoffPending={selectedQueueDone && !selectedProgramsReleased}
          openPrograms={openProgramsFor(selectedRow.lane.id)}
          opsIssueMode={selectedOpsIssueMode}
          onOpenBriefing={() => onOpenBriefing({ lane: selectedRow.lane.id })}
          onOpenDeliveryBoard={
            onOpenDeliveryBoard != null
              ? () => onOpenDeliveryBoard({ laneId: selectedRow.lane.id })
              : undefined
          }
          onOpenCluster={onOpenCluster}
          onOpenOperateQueue={onOpenOperateQueue}
          onOpenControlRoom={onOpenControlRoom}
        />
      )}
    </div>
  )
}

function ActiveSessionDetail({
  lane,
  queue,
  progress,
  canAdmin,
  context,
  migrateTrackNext,
  auditRecords,
  auditLoading,
  onOpenAudit,
  focusedProgramId,
  selectedComplete,
  signoffPending,
  openPrograms,
  opsIssueMode,
  onOpenBriefing,
  onOpenDeliveryBoard,
  onOpenCluster,
  onOpenOperateQueue,
  onOpenControlRoom,
}: {
  lane: WorkLane
  queue: QueueItem[]
  progress: { done: number; total: number } | null
  canAdmin: boolean
  context: OpsContextResponse | undefined
  migrateTrackNext: string | null
  auditRecords: AuditRecord[]
  auditLoading: boolean
  onOpenAudit?: () => void
  focusedProgramId?: string
  selectedComplete: boolean
  signoffPending: boolean
  openPrograms: ProgramSummary[]
  opsIssueMode: boolean
  onOpenBriefing: () => void
  onOpenDeliveryBoard?: () => void
  onOpenCluster?: () => void
  onOpenOperateQueue?: () => void
  onOpenControlRoom?: () => void
}) {
  const readyForSignOff = !opsIssueMode && queue.some(q => q.status === 'ready_for_signoff')
  const openProgramLabels = openPrograms.map(p => p.label ?? p.title ?? p.id)
  const closeHint =
    openPrograms.length === 0 || openPrograms.some(p => !isGatesComplete(p))
      ? 'sign-gates'
      : 'no-handoff'
  const openIssues = opsIssueMode ? opsOpenIssueCount(queue) : null

  return (
    <div className="flex w-full min-w-0 max-w-full flex-col gap-3">
      <section className="page-section panel-elevated w-full min-w-0 max-w-full overflow-x-hidden border-[var(--primary)]/25 px-3 py-2.5">
        <p className="briefing-section-kicker m-0">Execute</p>
        <div className="mt-0.5 flex flex-wrap items-center gap-2">
          <h2 className="m-0 text-sm font-semibold">{lane.label}</h2>
          <BriefingStatusBadge status="doing" />
          {openIssues != null ? (
            <span className="font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {openIssues} open
            </span>
          ) : (
            progress != null && (
              <span className="font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                {progress.done}/{progress.total}
              </span>
            )
          )}
        </div>
        <p className="m-0 mt-1 break-words text-[var(--text-dense-caption)] text-[var(--muted-foreground)] [overflow-wrap:anywhere]">
          {opsIssueMode
            ? 'Diagnose open issues, then unblock via Cluster / Operate / Control Room. Pack lives on Briefing. No program phase Sign-off on this lane — it clears when probes are healthy.'
            : 'Track queue progress and Owner sign-off. Pack / Launch lives on Briefing.'}
        </p>

        {!opsIssueMode && !canAdmin && readyForSignOff && (
          <div className="mt-2 rounded-md border border-[var(--warning)]/40 bg-[var(--warning)]/10 px-2.5 py-1.5">
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--foreground)]">
              Admin auth required for sign-off. Authenticate as Admin, then approve ready phases.
            </p>
          </div>
        )}
        {!opsIssueMode && canAdmin && readyForSignOff && (
          <div className="mt-2 rounded-md border border-[var(--success)]/35 bg-[var(--success)]/10 px-2.5 py-1.5">
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--foreground)]">
              Ready for sign-off — review the phase table and approve completed phases.
            </p>
          </div>
        )}

        {opsIssueMode && openIssues != null && openIssues > 0 && (
          <div className="mt-2 rounded-md border border-[var(--destructive)]/35 bg-[var(--destructive)]/10 px-2.5 py-1.5">
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--foreground)]">
              {openIssues} blocked issue{openIssues === 1 ? '' : 's'} — use row actions below (Open
              Cluster / Operate / Copy for Agent). Mission CAUTION can coexist with Doing until
              probes clear.
            </p>
          </div>
        )}

        {!opsIssueMode && signoffPending && (
          <div className="mt-2 rounded-md border border-[var(--color-lamp-yellow)]/35 bg-[var(--color-lamp-yellow)]/10 px-2.5 py-1.5">
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--foreground)]">
              Queue items are complete — still open:{' '}
              {openProgramLabels.length > 0
                ? openProgramLabels.join(' · ')
                : 'linked Delivery program'}
              {closeHint === 'sign-gates'
                ? '. Sign remaining gates before close.'
                : '. Record no-handoff / close on that program (a lane can have more than one).'}
            </p>
          </div>
        )}
        {!opsIssueMode && selectedComplete && (
          <div className="mt-2 flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)]/60 bg-[var(--secondary)]/20 px-2.5 py-1.5">
            <p className="m-0 flex-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Queue items are complete — review Delivery archive for this lane.
            </p>
            {onOpenDeliveryBoard != null && (
              <Button size="sm" variant="outline" onClick={onOpenDeliveryBoard}>
                Open Delivery
              </Button>
            )}
          </div>
        )}

        <div className="mt-2.5 flex flex-wrap gap-2">
          {opsIssueMode && onOpenCluster != null && (
            <Button size="sm" variant="default" onClick={onOpenCluster}>
              Open Cluster
            </Button>
          )}
          {opsIssueMode && onOpenOperateQueue != null && (
            <Button size="sm" variant="secondary" onClick={onOpenOperateQueue}>
              Open Operate Queue
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onOpenBriefing}>
            Re-prepare in Briefing
          </Button>
        </div>
      </section>

      <ActiveSessionPhaseBoard
        lane={lane}
        queue={queue}
        focusedProgramId={focusedProgramId}
        allowSignOff={!opsIssueMode}
        context={context}
        canAdmin={canAdmin}
        migrateTrackNext={migrateTrackNext}
        auditRecords={auditRecords}
        auditLoading={auditLoading}
        onOpenAudit={onOpenAudit}
        onOpenCluster={onOpenCluster}
        onOpenOperateQueue={onOpenOperateQueue}
        onOpenControlRoom={onOpenControlRoom}
      />
    </div>
  )
}
