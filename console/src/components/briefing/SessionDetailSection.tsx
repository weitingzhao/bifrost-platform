import { useState, type ReactNode } from 'react'
import { SegmentControl } from '@bifrost/ui'
import { ChevronDown, ChevronRight, Languages, Package } from 'lucide-react'
import { SessionLaneCtaBar, type SessionLifecycle } from '@/components/briefing/SessionLaneCtaBar'
import { MoveLaneBar } from '@/components/briefing/MoveLaneBar'
import {
  BriefingCommandChip,
  withBriefingCommandHighlight,
} from '@/components/briefing/BriefingCommandChip'
import { TaskQueuePanel } from '@/components/briefing/TaskQueuePanel'
import { SessionProgramDeliveryPanel } from '@/components/briefing/SessionProgramDeliveryPanel'
import { BriefingReconcilePanel } from '@/components/briefing/BriefingReconcilePanel'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
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
  focusedProgramId?: string
  /** After PATCH reclassification — sync Briefing Scope / Track Type filters. */
  onLaneMoved?: (line: ComponentLineId, trackType: WorkTrackType) => void
}

/**
 * Session focus zone: CTA + pack knobs + reconcile status + queue + program sign-off.
 * Completed lanes keep queue archive; program sign-off / Approve remain available here.
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
  focusedProgramId,
  onLaneMoved,
}: SessionDetailSectionProps) {
  const { canAdmin } = usePlatformAuth()
  const [previewOpen, setPreviewOpen] = useState(false)
  const isArchive = laneLifecycle === 'complete'

  return (
    <section
      className={
        isArchive
          ? 'page-section panel-elevated w-full min-w-0 max-w-full overflow-x-hidden border-[var(--border)]/60 px-3 py-2.5'
          : 'page-section panel-elevated w-full min-w-0 max-w-full overflow-x-hidden border-[var(--primary)]/25 px-3 py-2.5'
      }
    >
      <p className="briefing-section-kicker m-0">{isArchive ? 'Archive' : 'Session'}</p>
      <h2 className="m-0 mt-0.5 text-sm font-semibold">
        {isArchive ? 'Completed lane' : 'Selected lane detail'}
      </h2>
      <p className="m-0 mt-1 break-words text-[var(--text-dense-caption)] text-[var(--muted-foreground)] [overflow-wrap:anywhere]">
        {isArchive ? (
          <>
            Read-only history for this completed lane. Delivery sign-off and post-completion review
            remain available below. Create a new lane to start new work.
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
            'Open in Cursor (/briefing) or copy the pack, then work the queue below.',
          )
        )}
      </p>

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
      </div>

      {!isArchive && (
        <MoveLaneBar lane={lane} canOperate={canOperate} onMoved={onLaneMoved} />
      )}

      {!isArchive && (
        <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1.5 border-t border-[var(--border)]/60 pt-2">
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
            {previewOpen ? 'Hide pack' : 'Pack preview'}
          </button>
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

      <TaskQueuePanel
        items={queue}
        lane={lane}
        context={context}
        canAdmin={canAdmin && !isArchive}
        migrateTrackNext={migrateTrackNext}
        auditRecords={auditRecords}
        auditLoading={auditLoading}
        onOpenAudit={onOpenAudit}
      />

      <SessionProgramDeliveryPanel laneId={lane.id} focusedProgramId={focusedProgramId} />
    </section>
  )
}
