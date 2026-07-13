import { Button } from '@bifrost/ui'
import {
  BriefingStatusBadge,
  BriefingStatusLamp,
} from '@/components/briefing/BriefingStatusChrome'
import type { BriefingWorkStatus } from '@/lib/briefing/briefingStatus'
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
  dataReady: boolean
  packBlocked: boolean
  canOperate: boolean
  launchingIde: boolean
  sessionCopied: boolean
  launchStatus: string | null
  onCopySession: () => void
  onLaunchIde: () => void
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
  dataReady,
  packBlocked,
  canOperate,
  launchingIde,
  sessionCopied,
  launchStatus,
  onCopySession,
  onLaunchIde,
  embedded = false,
}: SessionLaneCtaBarProps) {
  const scopeLabel = briefingScopeById(scope).shortLabel
  const trackLabel = trackTypeById(trackType).label
  const intentLabel = workIntentById(intent).shortLabel
  const lifecycleStatus: BriefingWorkStatus = lifecycle === 'active' ? 'doing' : 'ready'
  const queueSummary = isInitMode
    ? 'empty · Init Mode'
    : `${queueCount} queue item${queueCount !== 1 ? 's' : ''}`

  const body = (
    <>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <BriefingStatusLamp status={lifecycleStatus} />
            {!embedded && <p className="briefing-section-kicker m-0">Session · selected lane</p>}
            <BriefingStatusBadge
              status={lifecycleStatus}
              label={lifecycle === 'active' ? 'Active' : 'Ready'}
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
            {lifecycle === 'active'
              ? 'Pack copied / launched this session'
              : 'Copy pack into a new Cursor IDE chat to start'}
          </p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Button
            type="button"
            size="sm"
            disabled={!dataReady || packBlocked}
            onClick={onCopySession}
            title={
              packBlocked
                ? 'Resolve pack reconcile blockers before copying'
                : !dataReady
                  ? 'Loading spine & matrix…'
                  : undefined
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
            disabled={!canOperate || launchingIde || packBlocked || !dataReady}
            onClick={onLaunchIde}
            title={!canOperate ? 'Operator token required' : undefined}
          >
            {launchingIde ? 'Launching…' : 'Launch IDE Agent'}
          </Button>
        </div>
      </div>
      {launchStatus != null && (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--foreground)]">
          {launchStatus}
        </p>
      )}
    </>
  )

  if (embedded) {
    return (
      <div className="rounded-md border border-[var(--primary)]/30 bg-[var(--primary)]/5 px-3 py-2.5">
        {body}
      </div>
    )
  }

  return (
    <section className="page-section panel-elevated border-[var(--primary)]/30 px-4 py-3">
      {body}
    </section>
  )
}
