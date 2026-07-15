import { useState, type ReactNode } from 'react'
import { SegmentControl } from '@bifrost/ui'
import { ChevronDown, ChevronRight, Languages, Package } from 'lucide-react'
import {
  SessionLaneCtaBar,
  type SessionLifecycle,
} from '@/components/briefing/SessionLaneCtaBar'
import { TaskQueuePanel } from '@/components/briefing/TaskQueuePanel'
import { BriefingReconcilePanel } from '@/components/briefing/BriefingReconcilePanel'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { BriefingScopeId, WorkTrackType } from '@/lib/briefing/briefingViewTabs'
import type { BriefingPackSize } from '@/lib/briefing/briefingUrlState'
import {
  AGENT_DIALOGUE_LANGUAGE_OPTIONS,
  type AgentDialogueLanguage,
} from '@/lib/briefing/agentDialogueLanguage'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import type { QueueItem, WorkLane } from '@/lib/briefing/workLanes'
import type { AuditRecord, OpsContextResponse } from '@/api/types'
import type { ReconcileBriefingOptions } from '@/lib/briefing/reconcileBriefing'

export interface SessionDetailSectionProps {
  scope: BriefingScopeId
  trackType: WorkTrackType
  lane: WorkLane
  queue: QueueItem[]
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
}

/**
 * Session focus zone: CTA + pack knobs + reconcile status + queue.
 * Agent Desk handoff stays on Agent Desk — not a Briefing Session control.
 */
export function SessionDetailSection({
  scope,
  trackType,
  lane,
  queue,
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
}: SessionDetailSectionProps) {
  const { canAdmin } = usePlatformAuth()
  const [previewOpen, setPreviewOpen] = useState(false)

  return (
    <section className="page-section panel-elevated border-[var(--primary)]/25 px-3 py-2.5">
      <p className="briefing-section-kicker m-0">Session</p>
      <h2 className="m-0 mt-0.5 text-sm font-semibold">Selected lane detail</h2>
      <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        Copy the pack into Cursor IDE, then work the queue below.
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
          dataReady={dataReady}
          packBlocked={packBlocked}
          canOperate={canOperate}
          launchingIde={launchingIde}
          sessionCopied={sessionCopied}
          launchStatus={launchStatus}
          onCopySession={onCopySession}
          onLaunchIde={onLaunchIde}
          embedded
        />
      </div>

      {/* Pack knobs + reconcile status — always visible, not buried in Options */}
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

        <div className="min-w-0 flex-1">
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

      {previewOpen && <div className="mt-2">{packPreview}</div>}

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
    </section>
  )
}
