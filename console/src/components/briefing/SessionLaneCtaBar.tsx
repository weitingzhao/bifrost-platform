import { Button } from '@bifrost/ui'
import {
  BriefingStatusBadge,
  BriefingStatusLamp,
} from '@/components/briefing/BriefingStatusChrome'
import { withBriefingCommandHighlight } from '@/components/briefing/BriefingCommandChip'
import { openInCursorButtonLabel } from '@/lib/briefing/briefingDeliveryChannels'
import type { BriefingWorkStatus } from '@/lib/briefing/briefingStatus'
import type { LaneLifecycle } from '@/lib/briefing/briefingStatus'
import type { BriefingScopeId, WorkTrackType } from '@/lib/briefing/briefingViewTabs'
import { briefingScopeById, trackTypeById } from '@/lib/briefing/briefingViewTabs'
import type { WorkLane } from '@/lib/briefing/workLanes'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import { workIntentById } from '@/lib/briefing/workIntents'

export type SessionLifecycle = 'ready' | 'active'

interface SessionLaneCtaBarProps {
  scope: BriefingScopeId
  trackType: WorkTrackType
  lane: WorkLane
  queueCount: number
  /** Empty Ready lane → Init Mode pack. */
  isInitMode: boolean
  intent: WorkIntent
  lifecycle: SessionLifecycle
  /** Queue-derived lane lifecycle — complete lanes are archive-only (no work Session). */
  laneLifecycle: LaneLifecycle
  dataReady: boolean
  packBlocked: boolean
  canOperate: boolean
  preparingCursor: boolean
  sessionCopied: boolean
  launchStatus: string | null
  /** When true, prepare pack only — do not imply spawning a new Cursor Agent. */
  insideCursorBrowser?: boolean
  onCopySession: () => void
  onOpenInCursor: () => void
  /** Open New Lane form on the Lanes board, prefilled from this completed lane. */
  onUseAsReferenceForNewLane?: () => void
  /** When true, omit outer page-section chrome (used inside SessionDetailSection). */
  embedded?: boolean
}

export function SessionLaneCtaBar({
  scope,
  trackType,
  lane,
  queueCount,
  isInitMode,
  intent,
  lifecycle,
  laneLifecycle,
  dataReady,
  packBlocked,
  canOperate,
  preparingCursor,
  sessionCopied,
  launchStatus,
  insideCursorBrowser = false,
  onCopySession,
  onOpenInCursor,
  onUseAsReferenceForNewLane,
  embedded = false,
}: SessionLaneCtaBarProps) {
  const scopeLabel = briefingScopeById(scope).shortLabel
  const trackLabel = trackTypeById(trackType).label
  const intentLabel = workIntentById(intent).shortLabel
  const isArchive = laneLifecycle === 'complete'
  const packReady = !isArchive && lifecycle === 'active'
  const lifecycleStatus: BriefingWorkStatus = isArchive
    ? 'done'
    : packReady
      ? 'doing'
      : 'ready'
  const queueSummary = isInitMode
    ? 'empty · Init Mode'
    : `${queueCount} queue item${queueCount !== 1 ? 's' : ''}`
  const openLabel = openInCursorButtonLabel({
    preparing: preparingCursor,
    dataReady,
    packBlocked,
    insideCursor: insideCursorBrowser,
    packReady,
  })
  const primaryVariant = packReady ? 'outline' : 'default'

  const metaHint = isArchive
    ? 'Archive only — no work Session. Use as reference when creating a New Lane.'
    : packReady
      ? insideCursorBrowser
        ? 'Pack ready — run /briefing in this chat (or re-prepare to refresh)'
        : 'Pack prepared / copied this session'
      : insideCursorBrowser
        ? 'Prepare pack, then run /briefing in this chat'
        : 'Open in Cursor (/briefing) or copy pack to start'

  const body = isArchive ? (
    <>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <BriefingStatusLamp status={lifecycleStatus} />
            {!embedded && <p className="briefing-section-kicker m-0">Archive · completed lane</p>}
            <BriefingStatusBadge status="done" label="Archive" />
          </div>
          <p className="m-0 mt-1.5 text-sm font-semibold text-[var(--foreground)]">
            {scopeLabel} · {trackLabel} · {lane.label}
          </p>
          <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Queue: {queueSummary}
            {' · '}
            Intent from lane: {intentLabel}
            {' · '}
            {metaHint}
          </p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Button
            type="button"
            size="sm"
            disabled={!canOperate || onUseAsReferenceForNewLane == null}
            onClick={() => onUseAsReferenceForNewLane?.()}
            title={
              !canOperate
                ? 'Operator token required to create a lane'
                : 'Open New Lane on the Lanes board with this completed lane as reference'
            }
          >
            New Lane (reference)
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!dataReady}
            onClick={onCopySession}
            title="Copy read-only archive pack (history / audit) — does not start a work Session"
          >
            {sessionCopied ? 'Copied!' : 'Copy archive pack'}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              window.location.hash = `delivery-board?lane_id=${encodeURIComponent(lane.id)}`
            }}
            title="Open Delivery Board catalog filtered to this lane (read-only — sign-off stays in Session)"
          >
            Open Board
          </Button>
        </div>
      </div>
      {launchStatus != null && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--foreground)]">
          {withBriefingCommandHighlight(launchStatus)}
        </p>
      )}
    </>
  ) : (
    <>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <BriefingStatusLamp status={lifecycleStatus} />
            {!embedded && <p className="briefing-section-kicker m-0">Session · selected lane</p>}
            <BriefingStatusBadge
              status={lifecycleStatus}
              label={packReady ? 'Active' : 'Ready'}
            />
            {isInitMode && <BriefingStatusBadge status="new" label="Init" />}
          </div>
          <p className="m-0 mt-1.5 text-sm font-semibold text-[var(--foreground)]">
            {scopeLabel} · {trackLabel} · {lane.label}
          </p>
          <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Queue: {queueSummary}
            {' · '}
            Intent from lane: {intentLabel}
            {' · '}
            {withBriefingCommandHighlight(metaHint)}
          </p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Button
            type="button"
            size="sm"
            variant={primaryVariant}
            disabled={!canOperate || preparingCursor || packBlocked || !dataReady}
            onClick={onOpenInCursor}
            title={
              !canOperate
                ? 'Operator token required'
                : packBlocked
                  ? 'Resolve pack reconcile blockers before opening'
                  : !dataReady
                    ? 'Loading spine & matrix…'
                    : packReady
                      ? insideCursorBrowser
                        ? 'Rewrite active-pack.md (pack already ready — run /briefing in this chat)'
                        : 'Prepare again and open a new Cursor Agent with /briefing'
                      : insideCursorBrowser
                        ? 'Write pack for /briefing in this Cursor chat (no new Agent deep link)'
                        : 'Prepare pack + open Cursor with /briefing'
            }
          >
            {openLabel}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!dataReady || packBlocked}
            onClick={onCopySession}
            title={
              packBlocked
                ? 'Resolve pack reconcile blockers before copying'
                : !dataReady
                  ? 'Loading spine & matrix…'
                  : 'Fallback: copy pack and paste into a new Cursor chat'
            }
          >
            {!dataReady
              ? 'Loading…'
              : packBlocked
                ? 'Pack blocked'
                : sessionCopied
                  ? 'Copied!'
                  : isInitMode
                    ? 'Copy Init pack'
                    : 'Copy session pack'}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              window.location.hash = `delivery-board?lane_id=${encodeURIComponent(lane.id)}`
            }}
            title="Open Delivery Board catalog filtered to this lane (read-only — sign-off stays in Session)"
          >
            Open Board
          </Button>
        </div>
      </div>
      {launchStatus != null && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--foreground)]">
          {withBriefingCommandHighlight(launchStatus)}
        </p>
      )}
    </>
  )

  if (embedded) {
    return (
      <div
        className={
          isArchive
            ? 'rounded-md border border-[var(--border)]/70 bg-[var(--secondary)]/30 px-3 py-2.5'
            : 'rounded-md border border-[var(--primary)]/30 bg-[var(--primary)]/5 px-3 py-2.5'
        }
      >
        {body}
      </div>
    )
  }

  return (
    <section
      className={
        isArchive
          ? 'page-section panel-elevated border-[var(--border)]/70 px-4 py-3'
          : 'page-section panel-elevated border-[var(--primary)]/30 px-4 py-3'
      }
    >
      {body}
    </section>
  )
}
