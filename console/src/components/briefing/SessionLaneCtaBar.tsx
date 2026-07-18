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

function LaneProblemBlock({ description }: { description: string }) {
  const trimmed = description.trim()
  if (trimmed === '') return null
  return (
    <div className="min-w-0 rounded-md border border-[var(--border)]/60 bg-[var(--background)]/70 px-2.5 py-2">
      <p className="m-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        Problem
      </p>
      <p className="m-0 mt-1 whitespace-pre-wrap break-words text-[var(--text-dense-label)] leading-snug text-[var(--foreground)] [overflow-wrap:anywhere]">
        {trimmed}
      </p>
    </div>
  )
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
  const packHint = isArchive
    ? 'Read-only reference — create a new lane to continue work.'
    : packReady
      ? insideCursorBrowser
        ? 'Pack ready — run /briefing in this chat (or re-prepare to refresh).'
        : 'Pack prepared / copied this session.'
      : insideCursorBrowser
        ? 'Prepare pack, then run /briefing in this chat.'
        : 'Open in Cursor (/briefing) or copy pack to start.'

  const sessionMeta = isArchive
    ? `${queueCount} archived item${queueCount !== 1 ? 's' : ''} · Intent: ${intentLabel}`
    : `Queue: ${queueSummary} · Intent: ${intentLabel}`

  /** Solid / bordered chrome so actions never read as plain text on tinted Session card. */
  const actionPrimaryClass =
    'h-8 border border-[var(--primary)]/40 bg-[var(--primary)] text-[var(--primary-foreground)] shadow-sm hover:bg-[var(--primary)]/90'
  const actionSecondaryClass =
    'h-8 border border-[var(--border)] bg-[var(--background)] text-[var(--foreground)] shadow-sm hover:bg-[var(--muted)]/60'

  const body = (
    <div className="flex min-w-0 flex-col gap-2.5">
      {/* 1. Status + title */}
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <BriefingStatusLamp status={lifecycleStatus} />
          {!embedded && (
            <p className="briefing-section-kicker m-0">
              {isArchive ? 'Archive · completed lane' : 'Session · selected lane'}
            </p>
          )}
          {isArchive ? (
            <BriefingStatusBadge status="done" label="Archive" />
          ) : (
            <>
              <BriefingStatusBadge
                status={lifecycleStatus}
                label={packReady ? 'Active' : 'Ready'}
              />
              {isInitMode && <BriefingStatusBadge status="new" label="Init" />}
            </>
          )}
        </div>
        <p
          className="m-0 mt-1.5 break-words text-sm font-semibold text-[var(--foreground)] [overflow-wrap:anywhere]"
          title={`${scopeLabel} · ${trackLabel} · ${lane.label}`}
        >
          {scopeLabel} · {trackLabel} · {lane.label}
        </p>
      </div>

      {/* 2. Owner-authored problem (distinct surface) */}
      <LaneProblemBlock description={lane.description} />

      {/* 3. Session facts only */}
      <p className="m-0 break-words text-[var(--text-dense-meta)] text-[var(--muted-foreground)] [overflow-wrap:anywhere]">
        {sessionMeta}
      </p>

      {/* 4. Actions + pack guidance */}
      <div className="min-w-0 border-t border-[var(--border)]/55 pt-2.5">
        <p className="m-0 mb-1.5 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
          Actions
        </p>
        <div className="flex min-w-0 flex-wrap items-center gap-2 rounded-md border border-[var(--border)]/60 bg-[var(--background)]/55 p-2">
          {isArchive ? (
            <>
              <Button
                type="button"
                size="sm"
                variant="default"
                className={actionPrimaryClass}
                disabled={!canOperate || onUseAsReferenceForNewLane == null}
                onClick={() => onUseAsReferenceForNewLane?.()}
                title={
                  !canOperate
                    ? 'Operator token required to create a lane'
                    : 'Open New Lane on the Lanes board with this completed lane as reference'
                }
              >
                Create lane from reference
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className={actionSecondaryClass}
                disabled={!dataReady}
                onClick={onCopySession}
                title="Copy read-only archive pack (history / audit) — does not start a work Session"
              >
                {sessionCopied ? 'Copied!' : 'Copy archive pack'}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className={actionSecondaryClass}
                onClick={() => {
                  window.location.hash = `delivery-board?lane_id=${encodeURIComponent(lane.id)}`
                }}
                title="Open Delivery Board catalog filtered to this lane (read-only — sign-off stays in Session)"
              >
                View Board catalog
              </Button>
            </>
          ) : (
            <>
              <Button
                type="button"
                size="sm"
                variant="default"
                className={actionPrimaryClass}
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
                className={actionSecondaryClass}
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
                variant="outline"
                size="sm"
                className={actionSecondaryClass}
                onClick={() => {
                  window.location.hash = `delivery-board?lane_id=${encodeURIComponent(lane.id)}`
                }}
                title="Open Delivery Board catalog filtered to this lane (read-only — sign-off stays in Session)"
              >
                Open Board
              </Button>
            </>
          )}
        </div>
        <p className="m-0 mt-1.5 break-words text-[var(--text-dense-caption)] text-[var(--muted-foreground)] [overflow-wrap:anywhere]">
          {withBriefingCommandHighlight(packHint)}
        </p>
      </div>

      {launchStatus != null && (
        <p className="m-0 break-words rounded-md border border-[var(--border)]/50 bg-[var(--secondary)]/40 px-2 py-1.5 text-[var(--text-dense-caption)] text-[var(--foreground)] [overflow-wrap:anywhere]">
          {withBriefingCommandHighlight(launchStatus)}
        </p>
      )}
    </div>
  )

  if (embedded) {
    return (
      <div
        className={
          isArchive
            ? 'w-full min-w-0 max-w-full rounded-md border border-[var(--border)]/70 bg-[var(--secondary)]/30 px-3 py-2.5'
            : 'w-full min-w-0 max-w-full rounded-md border border-[var(--primary)]/30 bg-[var(--primary)]/5 px-3 py-2.5'
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
