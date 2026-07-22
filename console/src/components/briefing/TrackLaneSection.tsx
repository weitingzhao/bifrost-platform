import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Button, ConfirmDialog, IconActionButton, SegmentControl } from '@bifrost/ui'
import { Loader2, Plus, Trash2 } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import {
  BRIEFING_DPR_COLOR,
  BriefingProgressMeter,
  BriefingStatusBadge,
  BriefingStatusLamp,
  briefingDashedCardClass,
  briefingLaneListRowClass,
  briefingSolidCardClass,
} from '@/components/briefing/BriefingStatusChrome'
import { BriefingIconBadge, LANE_ICONS, TRACK_ICONS } from '@/lib/briefing/briefingIcons'
import {
  laneLifecycleFromQueue,
  lifecycleToBriefingStatus,
  type LaneLifecycle,
} from '@/lib/briefing/briefingStatus'
import {
  briefingScopeById,
  componentLineById,
  defaultLaneForScopeTrack,
  lanesForScope,
  lanesForScopeTrack,
  trackTypeById,
  type BriefingScopeId,
} from '@/lib/briefing/briefingViewTabs'
import {
  buildQueueForLane,
  COMPONENT_LINE_IDS,
  lanesForTrack,
  queueProgress,
  type ComponentLineId,
  type LaneId,
  type QueueItem,
  type WorkLane,
  type WorkTrackType,
} from '@/lib/briefing/workLanes'
import type { TrackId } from '@/lib/briefing/workTracks'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import {
  buildNewLaneInitPack,
  defaultTrackForLine,
  slugLaneId,
} from '@/lib/briefing/laneInitPack'
import { createLane, deleteLane, LANES_QUERY_KEY } from '@/api/lanes'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

export type NewLaneReference = {
  id: LaneId
  label: string
  description: string
}

interface TrackLaneSectionProps {
  scope?: BriefingScopeId
  /** @deprecated Prefer scope */
  componentLine?: ComponentLineId
  trackType?: WorkTrackType
  track: TrackId
  selectedLane: LaneId
  onSelectLane: (id: LaneId) => void
  /** Digest-driven lifecycle filter; null = show every lifecycle. */
  lifecycleFilter?: LaneLifecycle | null
  onClearLifecycleFilter?: () => void
  /** Increment to open the New Lane form (e.g. from Archive Session CTA). */
  newLaneOpenToken?: number
  /** Prefill New Lane description from a completed lane. */
  newLaneReference?: NewLaneReference | null
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
}

function LaneDeleteButton({
  lane,
  onDelete,
}: {
  lane: WorkLane
  onDelete: () => void
}) {
  return (
    <IconActionButton
      title="Delete lane"
      ariaLabel={`Delete lane ${lane.label}`}
      tone="danger"
      onClick={e => {
        e.stopPropagation()
        e.preventDefault()
        onDelete()
      }}
    >
      <Trash2 className="size-3.5" />
    </IconActionButton>
  )
}

function LaneCard({
  lane,
  selected,
  progress,
  lifecycle,
  onSelect,
  onDelete,
  showLineBadge = false,
}: {
  lane: WorkLane
  selected: boolean
  progress: ReturnType<typeof queueProgress>
  lifecycle: LaneLifecycle
  onSelect: () => void
  onDelete?: () => void
  showLineBadge?: boolean
}) {
  const status = lifecycleToBriefingStatus(lifecycle)
  const lineShort = componentLineById(lane.componentLine).shortLabel
  return (
    <div className={briefingSolidCardClass(selected)}>
      <div className="flex min-w-0 items-start gap-2.5">
        <button
          type="button"
          className="flex min-w-0 flex-1 items-start gap-2.5 border-0 bg-transparent p-0 text-left"
          onClick={onSelect}
        >
          <BriefingIconBadge icon={LANE_ICONS[lane.id]} selected={selected} />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2">
              <BriefingStatusLamp status={status} />
              <span
                className={[
                  'min-w-0 flex-1 truncate text-sm transition-colors',
                  selected
                    ? 'font-semibold text-[var(--foreground)]'
                    : 'font-medium text-[var(--muted-foreground)]',
                ].join(' ')}
              >
                {lane.label}
              </span>
              {showLineBadge && (
                <span className="shrink-0 rounded bg-[var(--border)] px-1.5 py-0.5 text-dense-caption font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
                  {lineShort}
                </span>
              )}
              <BriefingStatusBadge status={status} />
            </div>
            <p className="m-0 mt-1 line-clamp-2 break-words text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {lane.description}
            </p>
            {progress != null && (
              <BriefingProgressMeter
                done={progress.done}
                total={progress.total}
                percent={progress.percent}
                status={status}
              />
            )}
          </div>
        </button>
        {onDelete != null && <LaneDeleteButton lane={lane} onDelete={onDelete} />}
      </div>
    </div>
  )
}

function NewLaneInlineForm({
  line,
  trackType,
  reference,
  onClose,
  onCreated,
}: {
  /** When null (All scope), user must pick a target component line. */
  line: ComponentLineId | null
  trackType: WorkTrackType
  reference?: NewLaneReference | null
  onClose: () => void
  onCreated?: (laneId: string) => void
}) {
  const [targetLine, setTargetLine] = useState<ComponentLineId>(line ?? 'rocket')
  const [label, setLabel] = useState('')
  const [description, setDescription] = useState(() =>
    reference != null
      ? `Reference (completed): ${reference.label} (${reference.id})\n${reference.description}\n\nNext work direction: `
      : '',
  )
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const needsLinePicker = line == null
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()

  const handleCreate = useCallback(async () => {
    const trimmedLabel = label.trim()
    const trimmedDesc = description.trim()
    if (trimmedLabel === '' || trimmedDesc === '') return
    if (!canOperate) {
      setError('Authenticate as operator to create a lane')
      return
    }
    const id = slugLaneId(trimmedLabel)
    if (id.length < 2) {
      setError('Label must yield a valid kebab-case id')
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const created = await createLane({
        id,
        track: defaultTrackForLine(targetLine),
        component_line: targetLine,
        track_type: trackType,
        label: trimmedLabel,
        short_label: trimmedLabel.slice(0, 24),
        description: trimmedDesc,
        agent_mode: 'Ops',
        work_intent: 'feature',
      })
      await qc.invalidateQueries({ queryKey: LANES_QUERY_KEY })
      const pack = buildNewLaneInitPack(targetLine, trackType, trimmedDesc, created.id)
      try {
        await navigator.clipboard.writeText(pack)
      } catch {
        // clipboard optional
      }
      onCreated?.(created.id)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create lane')
    } finally {
      setSubmitting(false)
    }
  }, [label, description, targetLine, trackType, qc, canOperate, onCreated, onClose])

  return (
    <div className="col-span-full mt-1 min-w-0 max-w-full overflow-hidden rounded-lg border border-dashed border-[var(--primary)]/50 bg-[var(--primary)]/5 px-3 py-3 sm:px-4">
      <div className="flex min-w-0 items-center justify-between gap-2">
        <p className="m-0 text-[var(--text-dense-label)] font-semibold">New lane</p>
        <button
          type="button"
          className="shrink-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
          onClick={onClose}
        >
          Cancel
        </button>
      </div>
      {needsLinePicker && (
        <div className="mt-2 min-w-0">
          <p className="m-0 mb-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Target component line
          </p>
          <SegmentControl
            value={targetLine}
            onChange={v => setTargetLine(v as ComponentLineId)}
            options={COMPONENT_LINE_IDS.map(id => ({
              value: id,
              label: componentLineById(id).shortLabel,
            }))}
            size="xs"
            className="flex w-full min-w-0 max-w-full flex-wrap justify-start rounded-md"
          />
        </div>
      )}
      <input
        className="mt-2 w-full min-w-0 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:border-[var(--primary)] focus:outline-none"
        placeholder="Lane label (becomes kebab-case id)"
        value={label}
        onChange={e => setLabel(e.target.value)}
        autoFocus
      />
      <textarea
        ref={inputRef}
        className="mt-2 w-full min-w-0 resize-none rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:border-[var(--primary)] focus:outline-none"
        rows={2}
        placeholder="Describe this work direction — what problem does it solve, what will it deliver?"
        value={description}
        onChange={e => setDescription(e.target.value)}
      />
      {error != null && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-destructive">{error}</p>
      )}
      <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2">
        <Button
          type="button"
          size="sm"
          className="shrink-0"
          disabled={submitting || label.trim() === '' || description.trim() === ''}
          onClick={() => void handleCreate()}
        >
          {submitting ? (
            <>
              <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> Creating…
            </>
          ) : (
            <>
              <Plus className="mr-1 h-3.5 w-3.5" /> Create via API
            </>
          )}
        </Button>
        <span className="min-w-0 flex-1 break-words text-[var(--text-dense-caption)] text-[var(--muted-foreground)] [overflow-wrap:anywhere]">
          {reference != null
            ? `Reference: ${reference.label} · writes config/lanes.yaml · copies Init Pack`
            : 'Writes config/lanes.yaml · copies Init Pack to clipboard'}
        </span>
      </div>
    </div>
  )
}

function laneLifecycle(queue: QueueItem[]): LaneLifecycle {
  return laneLifecycleFromQueue(queue)
}

interface LaneWithQueue {
  lane: WorkLane
  queue: QueueItem[]
  progress: ReturnType<typeof queueProgress>
  lifecycle: LaneLifecycle
}

function EmptyLaneCard({
  lane,
  selected,
  onSelect,
  onDelete,
  showLineBadge = false,
}: {
  lane: WorkLane
  selected: boolean
  onSelect: () => void
  onDelete?: () => void
  showLineBadge?: boolean
}) {
  const lineShort = componentLineById(lane.componentLine).shortLabel
  return (
    <div className={briefingDashedCardClass(selected)}>
      <div className="flex min-w-0 items-start gap-2.5">
        <button
          type="button"
          className="flex min-w-0 flex-1 items-start gap-2.5 border-0 bg-transparent p-0 text-left"
          onClick={onSelect}
        >
          <BriefingIconBadge icon={LANE_ICONS[lane.id]} selected={selected} />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center gap-2">
              <BriefingStatusLamp status="ready" />
              <span
                className={[
                  'min-w-0 flex-1 truncate text-sm transition-colors',
                  selected
                    ? 'font-semibold text-[var(--foreground)]'
                    : 'font-medium text-[var(--muted-foreground)]',
                ].join(' ')}
              >
                {lane.label}
              </span>
              {showLineBadge && (
                <span className="shrink-0 rounded bg-[var(--border)] px-1.5 py-0.5 text-dense-caption font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
                  {lineShort}
                </span>
              )}
              <BriefingStatusBadge status="ready" />
            </div>
            <p className="m-0 mt-1 line-clamp-2 break-words text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {lane.description}
            </p>
            <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              No queue yet — select to generate a pack and start
            </p>
          </div>
        </button>
        {onDelete != null && <LaneDeleteButton lane={lane} onDelete={onDelete} />}
      </div>
    </div>
  )
}

function NewLaneEntry({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      className="col-span-full flex flex-col items-center justify-center gap-1.5 rounded-lg border-2 border-dashed border-[var(--primary)]/55 bg-[var(--primary)]/8 px-3 py-5 text-[var(--primary)] transition-colors hover:border-[var(--primary)] hover:bg-[var(--primary)]/14"
      onClick={onClick}
    >
      <span className="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--primary)]/15">
        <Plus className="h-5 w-5" strokeWidth={2} />
      </span>
      <span className="text-[var(--text-dense-label)] font-semibold">New Lane</span>
      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        Start a new work direction
      </span>
    </button>
  )
}

/**
 * Tag cards live in the Briefing master pane (~320–400px).
 * Viewport `lg:` must not force 4 columns here — the pane stays narrow while
 * the page is wide, which previously crushed cards into vertical text strips.
 */
const LANE_TAG_GRID = 'grid min-w-0 grid-cols-1 gap-2'

type LaneViewMode = 'tag' | 'list'

function LaneBandHeader({
  status,
  title,
  count,
  hint,
  trailing,
  /** Optional split counts for Backlog — maturity order ready → planned. */
  splitCounts,
}: {
  status: 'doing' | 'planned' | 'done'
  title: string
  count: number
  hint?: string
  trailing?: ReactNode
  splitCounts?: { planned: number; ready: number }
}) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <BriefingStatusLamp status={status} />
      <span className="text-[var(--text-dense-meta)] font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
        {title}
      </span>
      {splitCounts != null ? (
        <span className="inline-flex items-center gap-1.5 font-mono text-[var(--text-dense-caption)] tabular-nums">
          <BriefingStatusBadge status="ready" label={`${splitCounts.ready}`} />
          <span className={BRIEFING_DPR_COLOR.ready}>ready</span>
          <span className="text-[var(--muted-foreground)]/40">·</span>
          <BriefingStatusBadge status="planned" label={`${splitCounts.planned}`} />
          <span className={BRIEFING_DPR_COLOR.planned}>planned</span>
        </span>
      ) : (
        <BriefingStatusBadge status={status} label={`${count}`} />
      )}
      {hint != null && splitCounts == null && (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">{hint}</span>
      )}
      {trailing != null && <div className="ml-auto flex items-center gap-2">{trailing}</div>}
    </div>
  )
}

function LaneListRow({
  lane,
  progress,
  lifecycle,
  selected,
  onSelect,
  onDelete,
  showLineBadge,
  emptyHint,
}: {
  lane: WorkLane
  progress: ReturnType<typeof queueProgress>
  lifecycle: LaneLifecycle
  selected: boolean
  onSelect: () => void
  onDelete?: () => void
  showLineBadge: boolean
  /** When lifecycle is empty, show ready hint instead of progress. */
  emptyHint?: boolean
}) {
  const status = lifecycleToBriefingStatus(lifecycle)
  const lineShort = componentLineById(lane.componentLine).shortLabel
  return (
    <div className={briefingLaneListRowClass(selected, { emptyHint })}>
      <button
        type="button"
        onClick={onSelect}
        className="flex min-w-0 flex-1 items-center gap-2 border-0 bg-transparent p-0 text-left"
      >
        <BriefingStatusLamp status={status} />
        <span
          className={[
            'min-w-0 flex-1 truncate text-[var(--text-dense-meta)] transition-colors',
            selected
              ? 'font-semibold text-[var(--foreground)]'
              : 'font-medium text-[var(--muted-foreground)]',
          ].join(' ')}
        >
          {lane.label}
        </span>
        {showLineBadge && (
          <span className="shrink-0 rounded bg-[var(--border)] px-1.5 py-0.5 text-dense-caption font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
            {lineShort}
          </span>
        )}
        {emptyHint ? (
          <span className="shrink-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            No queue yet
          </span>
        ) : progress != null ? (
          <span className="shrink-0 font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {progress.done}/{progress.total}
          </span>
        ) : null}
        <BriefingStatusBadge status={status} />
      </button>
      {onDelete != null && <LaneDeleteButton lane={lane} onDelete={onDelete} />}
    </div>
  )
}

function NewLaneListEntry({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded-md border-2 border-dashed border-[var(--primary)]/55 bg-[var(--primary)]/8 px-2.5 py-2.5 text-left text-[var(--primary)] transition-colors hover:border-[var(--primary)] hover:bg-[var(--primary)]/14"
    >
      <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--primary)]/15">
        <Plus className="h-3.5 w-3.5" strokeWidth={2} />
      </span>
      <span className="min-w-0 flex-1 text-[var(--text-dense-meta)] font-semibold">
        New Lane
      </span>
      <span className="shrink-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        Start a direction
      </span>
    </button>
  )
}

function CompletedLanesGroup({
  items,
  selectedLane,
  onSelectLane,
  onRequestDelete,
  showLineBadge = false,
  viewMode,
}: {
  items: LaneWithQueue[]
  selectedLane: LaneId
  onSelectLane: (id: LaneId) => void
  onRequestDelete?: (lane: WorkLane) => void
  showLineBadge?: boolean
  viewMode: LaneViewMode
}) {
  const [expanded, setExpanded] = useState(false)

  if (items.length === 0) return null

  return (
    <div className="rounded-md border border-[var(--border)]/60 bg-[var(--secondary)]/20 px-2.5 py-2">
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded-md px-1 py-0.5 text-left transition-colors hover:bg-[var(--secondary)]"
        onClick={() => setExpanded(v => !v)}
      >
        <BriefingStatusLamp status="done" />
        <span className="text-[var(--text-dense-meta)] font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Completed
        </span>
        <BriefingStatusBadge status="done" label={`${items.length}`} />
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Archive · Program sign-off in Session · Board is catalog
        </span>
        <span className="ml-auto text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {expanded ? '▾' : '▸'}
        </span>
      </button>
      {expanded && viewMode === 'tag' && (
        <div className={`mt-2 ${LANE_TAG_GRID} opacity-70`}>
          {items.map(({ lane, progress, lifecycle }) => (
            <LaneCard
              key={lane.id}
              lane={lane}
              selected={selectedLane === lane.id}
              progress={progress}
              lifecycle={lifecycle}
              onSelect={() => onSelectLane(lane.id)}
              onDelete={
                onRequestDelete != null ? () => onRequestDelete(lane) : undefined
              }
              showLineBadge={showLineBadge}
            />
          ))}
        </div>
      )}
      {expanded && viewMode === 'list' && (
        <ul className="m-0 mt-2 flex list-none flex-col gap-1 p-0 opacity-80">
          {items.map(({ lane, progress, lifecycle }) => (
            <li key={lane.id}>
              <LaneListRow
                lane={lane}
                progress={progress}
                lifecycle={lifecycle}
                selected={selectedLane === lane.id}
                onSelect={() => onSelectLane(lane.id)}
                onDelete={
                  onRequestDelete != null ? () => onRequestDelete(lane) : undefined
                }
                showLineBadge={showLineBadge}
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/**
 * Layer 3 lane picker — pure filter board (no Session detail / queue).
 * Highlight chrome applies only to the selected lane card.
 */
export function TrackLaneSection({
  scope,
  componentLine,
  trackType,
  track,
  selectedLane,
  onSelectLane,
  lifecycleFilter = null,
  onClearLifecycleFilter,
  newLaneOpenToken = 0,
  newLaneReference = null,
  context,
  matrices,
  clusterSummary,
}: TrackLaneSectionProps) {
  const resolvedScope: BriefingScopeId | undefined = scope ?? componentLine
  /** Summary lifecycle filter → all track types (match portfolio Ready/Planned/Doing counts). */
  const crossTrack = lifecycleFilter != null
  const lanes =
    resolvedScope != null
      ? crossTrack
        ? lanesForScope(resolvedScope)
        : trackType != null
          ? lanesForScopeTrack(resolvedScope, trackType)
          : lanesForTrack(track)
      : lanesForTrack(track)
  const [showNewLane, setShowNewLane] = useState(false)
  const [laneViewMode, setLaneViewMode] = useState<LaneViewMode>('list')
  const [activeReference, setActiveReference] = useState<NewLaneReference | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<WorkLane | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const showLineBadge = resolvedScope === 'all' || crossTrack
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()

  const requestDelete = useCallback(
    (lane: WorkLane) => {
      if (!canOperate) return
      setDeleteError(null)
      setDeleteTarget(lane)
    },
    [canOperate],
  )

  const confirmDelete = useCallback(async () => {
    if (deleteTarget == null) return
    setDeleting(true)
    setDeleteError(null)
    try {
      await deleteLane(deleteTarget.id)
      await qc.invalidateQueries({ queryKey: LANES_QUERY_KEY })
      if (selectedLane === deleteTarget.id) {
        const remaining = lanes.filter(l => l.id !== deleteTarget.id)
        const fallback =
          remaining[0]?.id ??
          (trackType != null && resolvedScope != null
            ? defaultLaneForScopeTrack(resolvedScope, trackType)
            : remaining[0]?.id)
        if (fallback != null && fallback !== selectedLane) {
          onSelectLane(fallback)
        }
      }
      setDeleteTarget(null)
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : String(err))
    } finally {
      setDeleting(false)
    }
  }, [
    deleteTarget,
    qc,
    selectedLane,
    lanes,
    trackType,
    resolvedScope,
    onSelectLane,
  ])

  const deleteHandler = canOperate ? requestDelete : undefined

  const trackTypeLabel = crossTrack
    ? 'All tracks'
    : trackType != null
      ? trackTypeById(trackType).label
      : track

  const laneItems: LaneWithQueue[] = useMemo(
    () =>
      lanes.map(lane => {
        const q = buildQueueForLane(lane.id, context, matrices, clusterSummary)
        return { lane, queue: q, progress: queueProgress(q), lifecycle: laneLifecycle(q) }
      }),
    [lanes, context, matrices, clusterSummary],
  )

  const filteredLaneItems = useMemo(() => {
    if (lifecycleFilter == null) return laneItems
    return laneItems.filter(item => item.lifecycle === lifecycleFilter)
  }, [laneItems, lifecycleFilter])

  const groups = useMemo(() => {
    const g: Record<LaneLifecycle, LaneWithQueue[]> = {
      active: [],
      planned: [],
      empty: [],
      complete: [],
    }
    for (const item of filteredLaneItems) g[item.lifecycle].push(item)
    return g
  }, [filteredLaneItems])

  const doingLanes = groups.active
  /** Backlog maturity: Ready (empty) before Planned. */
  const backlogLanes = [...groups.empty, ...groups.planned]
  const hasCompleted = groups.complete.length > 0
  const noDoing = doingLanes.length === 0
  const noBacklog = backlogLanes.length === 0
  const allComplete =
    lifecycleFilter == null && noDoing && noBacklog && hasCompleted
  const backlogOnly = lifecycleFilter == null && noDoing && !noBacklog
  const canCreateLane =
    resolvedScope != null &&
    trackType != null &&
    (lifecycleFilter == null || lifecycleFilter === 'empty' || lifecycleFilter === 'planned')
  const newLaneTargetLine: ComponentLineId | null =
    resolvedScope != null && resolvedScope !== 'all' ? resolvedScope : null
  const filterActive = lifecycleFilter != null
  const filterEmpty = filterActive && filteredLaneItems.length === 0
  const filterChipLabel =
    lifecycleFilter === 'active'
      ? 'Doing'
      : lifecycleFilter === 'planned'
        ? 'Planned'
        : lifecycleFilter === 'empty'
          ? 'Ready'
          : lifecycleFilter === 'complete'
            ? 'Done'
            : null
  const showDoingBand =
    lifecycleFilter == null || lifecycleFilter === 'active' ? doingLanes.length > 0 : false
  const showBacklogBand =
    lifecycleFilter == null
      ? backlogLanes.length > 0 || canCreateLane
      : lifecycleFilter === 'planned' || lifecycleFilter === 'empty'
        ? backlogLanes.length > 0 || canCreateLane
        : false
  const showCompletedBand =
    hasCompleted && (lifecycleFilter == null || lifecycleFilter === 'complete')

  useEffect(() => {
    if (newLaneOpenToken <= 0) return
    if (!canCreateLane) return
    setActiveReference(newLaneReference)
    setShowNewLane(true)
  }, [newLaneOpenToken, newLaneReference, canCreateLane])

  return (
    <section className="page-section panel-elevated px-3 py-2.5">
      <p className="briefing-section-kicker m-0">Lanes</p>
      <div className="mt-1 flex flex-wrap items-center gap-2">
        <BriefingIconBadge
          icon={trackType != null ? trackTypeById(trackType).icon : TRACK_ICONS[track]}
          size="sm"
        />
        <h2 className="m-0 text-sm font-semibold">
          {trackTypeLabel}
          {resolvedScope != null ? ` · ${briefingScopeById(resolvedScope).label}` : ''}
        </h2>
        {filterChipLabel != null && (
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md border border-[var(--primary)]/40 bg-[var(--primary)]/10 px-2 py-0.5 text-[var(--text-dense-caption)] font-medium"
            onClick={() => onClearLifecycleFilter?.()}
            title="Clear digest filter"
          >
            Filter · {filterChipLabel} ✕
          </button>
        )}
        <div className="ml-auto">
          <SegmentControl
            value={laneViewMode}
            onChange={v => setLaneViewMode(v as LaneViewMode)}
            options={[
              { value: 'tag', label: 'Tag' },
              { value: 'list', label: 'List' },
            ]}
            size="xs"
          />
        </div>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        {filterActive
          ? `Showing ${filterChipLabel?.toLowerCase()} only across all track types (matches Summary). Clear filter for Track Type board.`
          : 'Doing / Backlog → work Session. Completed = archive (reference for New Lane only).'}
      </p>

      {filterEmpty && (
        <div className="mt-3 rounded-md border border-dashed border-[var(--border)] px-3 py-2.5 text-center">
          <p className="m-0 text-sm font-medium text-[var(--foreground)]">
            No {filterChipLabel?.toLowerCase()} lanes in this scope
          </p>
          <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Change Work Scope / Track Type, or clear the digest filter.
          </p>
        </div>
      )}

      {allComplete && (
        <div className="mt-3 rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-3 py-2.5 text-center">
          <p className="m-0 text-sm font-medium text-[var(--foreground)]">All lanes complete</p>
          <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Start a new lane under Backlog. Program sign-off stays in Session; Delivery Board is the
            read-only catalog. Completed below is an archive view of lanes.
          </p>
        </div>
      )}

      {backlogOnly && (
        <div className="mt-3 rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-3 py-2.5 text-center">
          <p className="m-0 text-sm font-medium text-[var(--foreground)]">Nothing in Doing</p>
          <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Pick a Backlog lane below, or create a new one.
          </p>
        </div>
      )}

      <div className="mt-3 flex flex-col gap-4">
        {showDoingBand && (
          <div className="flex flex-col gap-2">
            <LaneBandHeader
              status="doing"
              title="Doing"
              count={doingLanes.length}
              hint="In progress"
            />
            {laneViewMode === 'tag' ? (
              <div className={LANE_TAG_GRID}>
                {doingLanes.map(({ lane, progress, lifecycle }) => (
                  <LaneCard
                    key={lane.id}
                    lane={lane}
                    selected={selectedLane === lane.id}
                    progress={progress}
                    lifecycle={lifecycle}
                    onSelect={() => onSelectLane(lane.id)}
                    onDelete={
                      deleteHandler != null ? () => deleteHandler(lane) : undefined
                    }
                    showLineBadge={showLineBadge}
                  />
                ))}
              </div>
            ) : (
              <ul className="m-0 flex list-none flex-col gap-1 p-0">
                {doingLanes.map(({ lane, progress, lifecycle }) => (
                  <li key={lane.id}>
                    <LaneListRow
                      lane={lane}
                      progress={progress}
                      lifecycle={lifecycle}
                      selected={selectedLane === lane.id}
                      onSelect={() => onSelectLane(lane.id)}
                      onDelete={
                        deleteHandler != null ? () => deleteHandler(lane) : undefined
                      }
                      showLineBadge={showLineBadge}
                    />
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}

        {showBacklogBand && (
          <div className="flex flex-col gap-2">
            <LaneBandHeader
              status="planned"
              title="Backlog"
              count={backlogLanes.length}
              splitCounts={{
                planned: groups.planned.length,
                ready: groups.empty.length,
              }}
            />
            {laneViewMode === 'tag' ? (
              <div className={LANE_TAG_GRID}>
                {groups.empty.map(({ lane }) => (
                  <EmptyLaneCard
                    key={lane.id}
                    lane={lane}
                    selected={selectedLane === lane.id}
                    onSelect={() => onSelectLane(lane.id)}
                    onDelete={
                      deleteHandler != null ? () => deleteHandler(lane) : undefined
                    }
                    showLineBadge={showLineBadge}
                  />
                ))}
                {groups.planned.map(({ lane, progress, lifecycle }) => (
                  <LaneCard
                    key={lane.id}
                    lane={lane}
                    selected={selectedLane === lane.id}
                    progress={progress}
                    lifecycle={lifecycle}
                    onSelect={() => onSelectLane(lane.id)}
                    onDelete={
                      deleteHandler != null ? () => deleteHandler(lane) : undefined
                    }
                    showLineBadge={showLineBadge}
                  />
                ))}
                {canCreateLane && !showNewLane && (
                  <NewLaneEntry
                    onClick={() => {
                      setActiveReference(null)
                      setShowNewLane(true)
                    }}
                  />
                )}
                {canCreateLane && showNewLane && trackType != null && (
                  <NewLaneInlineForm
                    key={activeReference?.id ?? 'new-lane'}
                    line={newLaneTargetLine}
                    trackType={trackType}
                    reference={activeReference}
                    onClose={() => {
                      setShowNewLane(false)
                      setActiveReference(null)
                    }}
                    onCreated={id => onSelectLane(id)}
                  />
                )}
              </div>
            ) : (
              <ul className="m-0 flex list-none flex-col gap-1 p-0">
                {groups.empty.map(({ lane, progress, lifecycle }) => (
                  <li key={lane.id}>
                    <LaneListRow
                      lane={lane}
                      progress={progress}
                      lifecycle={lifecycle}
                      selected={selectedLane === lane.id}
                      onSelect={() => onSelectLane(lane.id)}
                      onDelete={
                        deleteHandler != null ? () => deleteHandler(lane) : undefined
                      }
                      showLineBadge={showLineBadge}
                      emptyHint
                    />
                  </li>
                ))}
                {groups.planned.map(({ lane, progress, lifecycle }) => (
                  <li key={lane.id}>
                    <LaneListRow
                      lane={lane}
                      progress={progress}
                      lifecycle={lifecycle}
                      selected={selectedLane === lane.id}
                      onSelect={() => onSelectLane(lane.id)}
                      onDelete={
                        deleteHandler != null ? () => deleteHandler(lane) : undefined
                      }
                      showLineBadge={showLineBadge}
                    />
                  </li>
                ))}
                {canCreateLane && !showNewLane && (
                  <li>
                    <NewLaneListEntry
                      onClick={() => {
                        setActiveReference(null)
                        setShowNewLane(true)
                      }}
                    />
                  </li>
                )}
                {canCreateLane && showNewLane && trackType != null && (
                  <li>
                    <NewLaneInlineForm
                      key={activeReference?.id ?? 'new-lane'}
                      line={newLaneTargetLine}
                      trackType={trackType}
                      reference={activeReference}
                      onClose={() => {
                        setShowNewLane(false)
                        setActiveReference(null)
                      }}
                      onCreated={id => onSelectLane(id)}
                    />
                  </li>
                )}
              </ul>
            )}
          </div>
        )}

        {showCompletedBand && (
          <CompletedLanesGroup
            key={`complete-${resolvedScope ?? 'all'}-${trackType ?? track}-${selectedLane}-${lifecycleFilter ?? 'all'}`}
            items={groups.complete}
            selectedLane={selectedLane}
            onSelectLane={onSelectLane}
            onRequestDelete={deleteHandler}
            showLineBadge={showLineBadge}
            viewMode={laneViewMode}
          />
        )}
      </div>

      <ConfirmDialog
        open={deleteTarget != null}
        title="Delete lane"
        message={
          deleteTarget != null
            ? `Remove “${deleteTarget.label}” (${deleteTarget.id}) from the Briefing catalog? This writes lanes.yaml and cannot be undone from the UI.`
            : ''
        }
        confirmLabel="Confirm delete"
        confirming={deleting}
        onConfirm={() => {
          void confirmDelete()
        }}
        onCancel={() => {
          if (!deleting) {
            setDeleteTarget(null)
            setDeleteError(null)
          }
        }}
      />
      {deleteError != null && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-destructive">{deleteError}</p>
      )}
    </section>
  )
}
