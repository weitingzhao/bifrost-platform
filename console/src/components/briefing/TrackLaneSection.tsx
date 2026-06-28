import { useState } from 'react'
import { StatusLamp } from '@/components/StatusLamp'
import { BriefingIconBadge, LANE_ICONS, TRACK_ICONS } from '@/lib/briefing/briefingIcons'
import {
  buildQueueForLane,
  laneById,
  lanesForTrack,
  queueProgress,
  type LaneId,
  type QueueItem,
  type QueueItemStatus,
  type WorkLane,
} from '@/lib/briefing/workLanes'
import type { TrackId } from '@/lib/briefing/workTracks'
import type { ClusterSummary, MatrixResponse, OpsContextResponse } from '@/api/types'

interface TrackLaneSectionProps {
  track: TrackId
  selectedLane: LaneId
  onSelectLane: (id: LaneId) => void
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
}

function queueItemReach(status: QueueItemStatus): 'ok' | 'degraded' | 'fail' | 'unknown' {
  switch (status) {
    case 'done':
    case 'closed':
      return 'ok'
    case 'in_progress':
    case 'next':
      return 'degraded'
    case 'issue':
    case 'blocked':
      return 'fail'
    default:
      return 'unknown'
  }
}

function statusLabel(status: QueueItemStatus): string {
  return status.replace('_', ' ')
}

function ProgressBar({ done, total, percent }: { done: number; total: number; percent: number }) {
  return (
    <div className="mt-1.5">
      <div className="flex items-center justify-between text-dense-caption text-[var(--muted-foreground)]">
        <span>{done}/{total}</span>
        <span>{percent}%</span>
      </div>
      <div className="mt-0.5 h-1 w-full overflow-hidden rounded-full bg-[var(--border)]">
        <div
          className="h-full rounded-full bg-[var(--primary)] transition-all"
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  )
}

function LaneCard({
  lane,
  selected,
  progress,
  onSelect,
}: {
  lane: WorkLane
  selected: boolean
  progress: ReturnType<typeof queueProgress>
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      className={[
        'rounded-lg border px-3 py-2 text-left transition-colors',
        selected
          ? 'border-[var(--primary)] bg-[var(--secondary)]'
          : 'border-[var(--border)] bg-[var(--card)] hover:bg-[var(--secondary)]',
      ].join(' ')}
      onClick={onSelect}
    >
      <div className="flex items-center gap-2">
        <BriefingIconBadge icon={LANE_ICONS[lane.id]} selected={selected} size="sm" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">{lane.label}</span>
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {lane.shortLabel}
            </span>
          </div>
          <p className="m-0 mt-1 line-clamp-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {lane.description}
          </p>
          {progress != null && (
            <ProgressBar done={progress.done} total={progress.total} percent={progress.percent} />
          )}
        </div>
      </div>
    </button>
  )
}

/**
 * Parse spine note that uses circled-number milestones: "preamble ① foo ② bar ③ baz"
 * Returns { preamble, milestones[] } — milestones empty if no ①②③ pattern found.
 */
function parseNoteMilestones(note: string): { preamble: string; milestones: string[] } {
  const circled = /[\u2460-\u2473]/g
  const matches = [...note.matchAll(circled)]
  if (matches.length === 0) return { preamble: note.trim(), milestones: [] }

  const preamble = note.slice(0, matches[0].index).trim()
  const milestones: string[] = []
  for (let i = 0; i < matches.length; i++) {
    const start = matches[i].index! + 1
    const end = i + 1 < matches.length ? matches[i + 1].index! : note.length
    const text = note.slice(start, end).trim()
    if (text !== '') milestones.push(text)
  }
  return { preamble, milestones }
}

function QueueItemRow({ item }: { item: QueueItem }) {
  const hasDetail =
    (item.note != null && item.note !== '') ||
    (item.prerequisites != null && item.prerequisites.length > 0)
  const [expanded, setExpanded] = useState(false)

  const parsed = item.note != null ? parseNoteMilestones(item.note) : null
  const hasMilestones = parsed != null && parsed.milestones.length > 0

  return (
    <li className="border-b border-[var(--border)] last:border-b-0">
      <button
        type="button"
        className="flex w-full items-start gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--secondary)]/40"
        disabled={!hasDetail}
        onClick={() => hasDetail && setExpanded(!expanded)}
      >
        <StatusLamp value={queueItemReach(item.status)} kind="reach" />
        <div className="min-w-0 flex-1">
          <p className="m-0 text-[var(--text-dense)]">{item.label}</p>
          <code className="mt-0.5 inline-block rounded bg-[var(--secondary)] px-1 py-px font-mono text-dense-caption text-[var(--muted-foreground)]">
            {item.id}
          </code>
          {!expanded && parsed != null && parsed.preamble !== '' && (
            <p className="m-0 mt-0.5 line-clamp-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {parsed.preamble}
            </p>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {item.progress != null && item.progress.total > 0 && (
            <span className="font-mono text-dense-caption text-[var(--muted-foreground)]">
              {item.progress.done}/{item.progress.total}
            </span>
          )}
          <span className="font-mono text-dense-caption uppercase text-[var(--muted-foreground)]">
            {statusLabel(item.status)}
          </span>
          {hasDetail && (
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {expanded ? '▾' : '▸'}
            </span>
          )}
        </div>
      </button>

      {expanded && (
        <div className="border-t border-[var(--border)] bg-[var(--background)] px-3 py-2 pl-8">
          {parsed != null && parsed.preamble !== '' && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--foreground)]">
              {parsed.preamble}
            </p>
          )}

          {hasMilestones && (
            <div className="mt-2">
              <p className="m-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
                Milestones
              </p>
              <ol className="m-0 mt-1 flex list-none flex-col gap-1 p-0">
                {parsed.milestones.map((ms, i) => {
                  const done = item.progress != null && i < item.progress.done
                  return (
                    <li key={i} className="flex items-start gap-2">
                      <span className={`mt-px text-[var(--text-dense-caption)] ${done ? 'text-[var(--success)]' : 'text-[var(--muted-foreground)]'}`}>
                        {done ? '✓' : '○'}
                      </span>
                      <span className={`text-[var(--text-dense-meta)] ${done ? 'text-[var(--muted-foreground)] line-through' : ''}`}>
                        {ms}
                      </span>
                    </li>
                  )
                })}
              </ol>
            </div>
          )}

          {!hasMilestones && item.note != null && item.note !== '' && parsed?.preamble === item.note.trim() && null}

          {item.prerequisites != null && item.prerequisites.length > 0 && (
            <div className="mt-2">
              <p className="m-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
                Prerequisites
              </p>
              <ul className="m-0 mt-1 flex list-none flex-col gap-0.5 p-0">
                {item.prerequisites.map((pre, i) => {
                  const met = pre.includes('✓') || pre.toLowerCase().includes('closed')
                  return (
                    <li key={i} className="flex items-start gap-2">
                      <span className={`mt-px text-[var(--text-dense-caption)] ${met ? 'text-[var(--success)]' : 'text-[var(--muted-foreground)]'}`}>
                        {met ? '✓' : '○'}
                      </span>
                      <span className={`text-[var(--text-dense-meta)] ${met ? 'text-[var(--muted-foreground)]' : ''}`}>
                        {pre}
                      </span>
                    </li>
                  )
                })}
              </ul>
            </div>
          )}
        </div>
      )}
    </li>
  )
}

function TaskQueuePanel({ items, lane }: { items: QueueItem[]; lane: WorkLane }) {
  if (items.length === 0) {
    return (
      <p className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        No queue items for {lane.label}. Spine track data may still be loading.
      </p>
    )
  }

  return (
    <div className="mt-3 overflow-hidden rounded-lg border border-[var(--border)]">
      <header className="flex items-center justify-between border-b border-[var(--border)] bg-[var(--background)] px-3 py-2">
        <div className="flex items-center gap-2">
          <BriefingIconBadge icon={LANE_ICONS[lane.id]} size="sm" />
          <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Task queue · {lane.shortLabel}
          </h3>
        </div>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {items.length} item{items.length > 1 ? 's' : ''}
        </span>
      </header>
      <ul className="m-0 flex list-none flex-col p-0">
        {items.map(item => (
          <QueueItemRow key={item.id} item={item} />
        ))}
      </ul>
    </div>
  )
}

export function TrackLaneSection({
  track,
  selectedLane,
  onSelectLane,
  context,
  matrices,
  clusterSummary,
}: TrackLaneSectionProps) {
  const lanes = lanesForTrack(track)
  const activeLane = laneById(selectedLane)
  const queue = buildQueueForLane(selectedLane, context, matrices, clusterSummary)

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <p className="briefing-section-kicker m-0">1b · Work lane</p>
      <div className="mt-1 flex items-center gap-2">
        <BriefingIconBadge icon={TRACK_ICONS[track]} size="sm" />
        <h2 className="m-0 text-sm font-semibold capitalize">
          {track} — pick a specific direction
        </h2>
      </div>
      <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Each lane has its own <strong>task tracking queue</strong> below. This scopes your Session briefing.
      </p>

      <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {lanes.map(lane => {
          const laneQueue = buildQueueForLane(lane.id, context, matrices, clusterSummary)
          const progress = queueProgress(laneQueue)
          return (
            <LaneCard
              key={lane.id}
              lane={lane}
              selected={selectedLane === lane.id}
              progress={progress}
              onSelect={() => onSelectLane(lane.id)}
            />
          )
        })}
      </div>

      <TaskQueuePanel items={queue} lane={activeLane} />
    </section>
  )
}
