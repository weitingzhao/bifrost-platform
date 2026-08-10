import { useEffect, useState, type ReactNode } from 'react'
import { Button, SegmentControl } from '@bifrost/ui'
import { ChevronDown, ChevronRight, Languages, Package } from 'lucide-react'
import { CollapseExpandIcon } from '@/components/layout/CollapseExpandIcon'
import { collapseExpandAriaLabel } from '@/components/layout/collapseExpandAria'
import { BriefingProgramAutoPhases } from '@/components/briefing/BriefingProgramAutoPhases'
import { SessionLaneCtaBar, type SessionLifecycle } from '@/components/briefing/SessionLaneCtaBar'
import { MoveLaneBar } from '@/components/briefing/MoveLaneBar'
import { BriefingCommandChip } from '@/components/briefing/BriefingCommandChip'
import { withBriefingCommandHighlight } from '@/components/briefing/withBriefingCommandHighlight'
import { TaskQueuePanel } from '@/components/briefing/TaskQueuePanel'
import { BriefingReconcilePanel } from '@/components/briefing/BriefingReconcilePanel'
import type { LaneLifecycle } from '@/lib/briefing/briefingStatus'
import type { BriefingScopeId, ComponentLineId, WorkTrackType } from '@/lib/briefing/briefingViewTabs'
import type { BriefingPackSize } from '@/lib/briefing/briefingUrlState'
import {
  AGENT_DIALOGUE_LANGUAGE_OPTIONS,
  type AgentDialogueLanguage,
} from '@/lib/briefing/agentDialogueLanguage'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import type { QueueItem, WorkLane } from '@/lib/briefing/workLanes'
import type { AuditRecord } from '@/api/auditTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { ReconcileBriefingOptions } from '@/lib/briefing/reconcileBriefing'

export interface SessionDetailSectionProps {
  scope: BriefingScopeId
  trackType: WorkTrackType
  lane: WorkLane
  queue: QueueItem[]
  isInitMode: boolean
  intent: WorkIntent
  lifecycle: SessionLifecycle
  laneLifecycle: LaneLifecycle
  dataReady: boolean
  packBlocked: boolean
  canOperate: boolean
  preparingCursor: boolean
  sessionCopied: boolean
  launchStatus: string | null
  insideCursorBrowser?: boolean
  onCopySession: () => void
  onOpenInCursor: () => void
  onUseAsReferenceForNewLane?: () => void
  context: OpsContextResponse | undefined
  migrateTrackNext?: string | null
  auditRecords?: AuditRecord[]
  auditLoading?: boolean
  onOpenAudit?: () => void
  agentDialogueLanguage: AgentDialogueLanguage
  onAgentDialogueLanguageChange: (v: AgentDialogueLanguage) => void
  packSize: BriefingPackSize
  onPackSizeChange: (v: BriefingPackSize) => void
  packReconcileOptions: ReconcileBriefingOptions
  /** Collapsed by default — inspect generated pack text only. */
  packPreview: ReactNode
  /** After PATCH reclassification — sync Briefing Scope / Track Type filters. */
  onLaneMoved?: (line: ComponentLineId, trackType: WorkTrackType) => void
  /**
   * When false, hide Queue (Doing lanes redirect to Active Session).
   * Default true.
   */
  showWorkRow?: boolean
  /** Navigate to Active Session for this Doing lane. */
  onOpenActiveSession?: () => void
}

/**
 * Archive / Session focus zone (full-width row under Scope | Lanes).
 * CTA + pack knobs above; Task Queue below (plan / archive history).
 * Program sign-off lives on Active Session; Delivery Board is the read-only catalog.
 * Doing (active) lanes on Briefing redirect execute work to Active Session.
 */
export function SessionDetailSection({
  scope,
  trackType,
  lane,
  queue,
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
  context,
  migrateTrackNext,
  auditRecords = [],
  auditLoading,
  onOpenAudit,
  agentDialogueLanguage,
  onAgentDialogueLanguageChange,
  packSize,
  onPackSizeChange,
  packReconcileOptions,
  packPreview,
  onLaneMoved,
  showWorkRow = true,
  onOpenActiveSession,
}: SessionDetailSectionProps) {
  const [previewOpen, setPreviewOpen] = useState(false)
  const [sectionOpen, setSectionOpen] = useState(true)
  const isArchive = laneLifecycle === 'complete'
  const isDoingRedirect = laneLifecycle === 'active' && !showWorkRow
  const sectionKicker = isArchive ? 'Archive' : isDoingRedirect ? 'In progress' : 'Session'
  const sectionTitle = isArchive
    ? 'Completed lane'
    : isDoingRedirect
      ? lane.label
      : 'Selected lane detail'

  useEffect(() => {
    setSectionOpen(true)
    setPreviewOpen(false)
  }, [lane.id])

  return (
    <div className="flex w-full min-w-0 max-w-full flex-col gap-3">
    <section
      className={
        isArchive
          ? 'page-section panel-elevated w-full min-w-0 max-w-full overflow-x-hidden border-[var(--border)]/60 px-3 py-2.5'
          : 'page-section panel-elevated w-full min-w-0 max-w-full overflow-x-hidden border-[var(--primary)]/25 px-3 py-2.5'
      }
    >
      <button
        type="button"
        className="flex w-full min-w-0 items-start gap-2 text-left"
        aria-expanded={sectionOpen}
        aria-label={collapseExpandAriaLabel(sectionOpen, sectionKicker.toLowerCase())}
        onClick={() => setSectionOpen(v => !v)}
      >
        <div className="min-w-0 flex-1">
          <p className="briefing-section-kicker m-0">{sectionKicker}</p>
          <h2 className="m-0 mt-0.5 text-sm font-semibold">{sectionTitle}</h2>
          {!sectionOpen && (
            <p className="m-0 mt-1 truncate text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {lane.label}
            </p>
          )}
        </div>
        <CollapseExpandIcon open={sectionOpen} className="mt-0.5" />
      </button>

      {sectionOpen && (
        <>
      <p className="m-0 mt-1 break-words text-[var(--text-dense-caption)] text-[var(--muted-foreground)] [overflow-wrap:anywhere]">
              {isDoingRedirect ? (
                <>
                  {queue.length > 0 &&
                  queue.every(q => q.status === 'done' || q.status === 'closed')
                    ? 'Queue items are complete — continue in Active Session for program sign-off and close.'
                    : 'This lane is in progress — continue in Active Session for queue tracking and Owner sign-off. Use Re-prepare below only if the pack needs a refresh.'}
                </>
              ) : isArchive ? (
          <>
            Read-only history for this completed lane. Program sign-off is on Active Session;
            Delivery Board is the catalog. Create a new lane to start new work.
          </>
        ) : insideCursorBrowser ? (
          lifecycle === 'active' ? (
            <>
              Pack ready — run <BriefingCommandChip /> in this chat. Use Re-prepare only to refresh
              the file.
            </>
          ) : (
            <>
              Prepare pack, then run <BriefingCommandChip /> in this chat — do not deep-link a new
              Agent from Browser.
            </>
          )
        ) : (
          withBriefingCommandHighlight(
            showWorkRow
              ? 'Open in Cursor (/briefing) or copy the pack, then work the plan queue below.'
              : 'Open in Cursor (/briefing) or copy the pack to start work.',
          )
        )}
      </p>

      {isDoingRedirect && onOpenActiveSession != null && (
        <div className="mt-2.5">
          <Button size="sm" onClick={onOpenActiveSession}>
            Continue in Active Session
          </Button>
        </div>
      )}

      {!isDoingRedirect && (
      <div className="mt-2.5">
        <SessionLaneCtaBar
          scope={scope}
          trackType={trackType}
          lane={lane}
          queueCount={queue.length}
          isInitMode={isInitMode}
          intent={intent}
          lifecycle={lifecycle}
          laneLifecycle={laneLifecycle}
          dataReady={dataReady}
          packBlocked={packBlocked}
          canOperate={canOperate}
          preparingCursor={preparingCursor}
          sessionCopied={sessionCopied}
          launchStatus={launchStatus}
          insideCursorBrowser={insideCursorBrowser}
          onCopySession={onCopySession}
          onOpenInCursor={onOpenInCursor}
          onUseAsReferenceForNewLane={onUseAsReferenceForNewLane}
          embedded
        />
        {lifecycle === 'active' && onOpenActiveSession != null && (
          <div className="mt-2">
            <Button size="sm" variant="outline" onClick={onOpenActiveSession}>
              Continue in Active Session
            </Button>
          </div>
        )}
      </div>
      )}

      {!isArchive && !isDoingRedirect && (
        <MoveLaneBar lane={lane} canOperate={canOperate} onMoved={onLaneMoved} />
      )}

      <BriefingProgramAutoPhases laneId={lane.id} />

      {!isArchive && (
        <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1.5 border-t border-[var(--border)]/60 pt-2">
          {!isDoingRedirect && (
            <>
          <div
            className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border)]/50 bg-[var(--muted)]/20 px-1.5 py-0.5"
            title="Agent dialogue language"
          >
            <Languages
              className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]"
              strokeWidth={2}
              aria-hidden
            />
            <SegmentControl
              ariaLabel="Agent dialogue language"
              value={agentDialogueLanguage}
              onChange={v => onAgentDialogueLanguageChange(v as AgentDialogueLanguage)}
              options={AGENT_DIALOGUE_LANGUAGE_OPTIONS.map(opt => ({
                value: opt.id,
                label: opt.id === 'zh' ? '中文' : 'EN',
              }))}
              size="xs"
            />
          </div>

          <div
            className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border)]/50 bg-[var(--muted)]/20 px-1.5 py-0.5"
            title="Session pack size"
          >
            <Package
              className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]"
              strokeWidth={2}
              aria-hidden
            />
            <SegmentControl
              ariaLabel="Session pack size"
              value={packSize}
              onChange={v => onPackSizeChange(v as BriefingPackSize)}
              options={[
                { value: 'compact', label: 'Compact' },
                { value: 'full', label: 'Full' },
              ]}
              size="xs"
            />
          </div>

          <div className="order-last min-w-0 basis-full">
            <BriefingReconcilePanel
              context={context}
              options={packReconcileOptions}
              variant="pack"
              compact
            />
          </div>
            </>
          )}

          <button
            type="button"
            className="inline-flex items-center gap-1 text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            aria-expanded={previewOpen}
            onClick={() => setPreviewOpen(v => !v)}
          >
            {previewOpen ? (
              <ChevronDown className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
            ) : (
              <ChevronRight className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
            )}
            {previewOpen ? 'Hide pack' : isDoingRedirect ? 'Pack preview (re-prepare)' : 'Pack preview'}
          </button>

          {isDoingRedirect && (
            <Button size="sm" variant="ghost" onClick={onOpenInCursor} disabled={!canOperate || preparingCursor}>
              {preparingCursor ? 'Preparing…' : 'Re-prepare pack'}
            </Button>
          )}
        </div>
      )}

      {isArchive && (
        <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-[var(--border)]/60 pt-2">
          <button
            type="button"
            className="inline-flex items-center gap-1 text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            aria-expanded={previewOpen}
            onClick={() => setPreviewOpen(v => !v)}
          >
            {previewOpen ? (
              <ChevronDown className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
            ) : (
              <ChevronRight className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
            )}
            {previewOpen ? 'Hide archive pack' : 'Preview archive pack'}
          </button>
        </div>
      )}

      {previewOpen && <div className="mt-2 min-w-0 max-w-full overflow-x-auto">{packPreview}</div>}
        </>
      )}
    </section>

    {showWorkRow && (
    <div className="min-w-0">
      <TaskQueuePanel
        items={queue}
        lane={lane}
        context={context}
        canAdmin={false}
        migrateTrackNext={migrateTrackNext}
        auditRecords={auditRecords}
        auditLoading={auditLoading}
        onOpenAudit={onOpenAudit}
      />
    </div>
    )}
    </div>
  )
}
