import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { AuditRecord, ClusterSummary, MatrixResponse, OpsContextResponse } from '@/api/types'
import { fetchRemediationJobs, fetchSessionSnapshotLatest } from '@/api/platform'
import { Button } from '@bifrost/ui'
import { BriefingFoldableSection } from '@/components/briefing/BriefingFoldableSection'
import { BriefingSessionResultsPanel } from '@/components/briefing/BriefingSessionResultsPanel'
import { NightlyBriefingPanel } from '@/components/briefing/NightlyBriefingPanel'
import { SessionDeltaPanel } from '@/components/briefing/SessionDeltaPanel'
import { buildBriefingAlignmentPack } from '@/lib/briefing/buildBriefingAlignmentPack'
import { computeSessionDelta, isEmptyDelta, type SessionDelta } from '@/lib/briefing/sessionDiff'
import { loadSnapshot, type SessionSnapshot } from '@/lib/briefing/sessionSnapshot'

export type AgentDeskSessionOpsPanelsProps = {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
  platformHealthy: boolean | undefined
  auditRecords: AuditRecord[]
  onOpenBriefing?: () => void
  onOpenBriefingReconciliation?: () => void
  onOpenDeliveryBoard?: () => void
}

/**
 * Session closure / drift / delta panels relocated from Agent Briefing.
 * Briefing stays focused on planned · doing · new; Desk owns runner lifecycle ops.
 */
export function AgentDeskSessionOpsPanels({
  context,
  matrices,
  clusterSummary,
  platformHealthy,
  auditRecords,
  onOpenBriefing,
  onOpenBriefingReconciliation,
  onOpenDeliveryBoard,
}: AgentDeskSessionOpsPanelsProps) {
  const [localSnapshot] = useState(() => loadSnapshot())
  const [sessionDelta, setSessionDelta] = useState<SessionDelta | null>(null)
  const [showAlignmentPack, setShowAlignmentPack] = useState(false)
  const [alignmentCopied, setAlignmentCopied] = useState(false)

  const serverSnapshotQuery = useQuery({
    queryKey: ['session-snapshot', 'latest'],
    queryFn: async () => {
      const res = await fetchSessionSnapshotLatest()
      return (res.snapshot ?? null) as SessionSnapshot | null
    },
    staleTime: 60_000,
  })

  const remediationJobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 30_000,
  })

  const previousSnapshot = useMemo((): SessionSnapshot | null => {
    if (serverSnapshotQuery.data != null) return serverSnapshotQuery.data
    return localSnapshot
  }, [serverSnapshotQuery.data, localSnapshot])

  const remediationJobs = remediationJobsQuery.data?.jobs ?? []
  const dataReady =
    context != null && !remediationJobsQuery.isLoading && !serverSnapshotQuery.isLoading

  useEffect(() => {
    if (!dataReady || previousSnapshot == null) return
    setSessionDelta(
      computeSessionDelta(
        previousSnapshot,
        { context, matrices, clusterSummary, platformHealthy },
        auditRecords,
        remediationJobs,
      ),
    )
  }, [
    dataReady,
    previousSnapshot,
    context,
    matrices,
    clusterSummary,
    platformHealthy,
    auditRecords,
    remediationJobs,
  ])

  const alignmentPack = useMemo(
    () =>
      buildBriefingAlignmentPack({
        context,
        matrices,
        clusterSummary,
        platformHealthy,
      }),
    [context, matrices, clusterSummary, platformHealthy],
  )

  async function handleCopyAlignment() {
    try {
      await navigator.clipboard.writeText(alignmentPack)
      setAlignmentCopied(true)
      window.setTimeout(() => setAlignmentCopied(false), 2000)
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Session ops moved here from Agent Briefing — Desk owns runner closure and drift review;
        Briefing stays on planned / doing / new work.
        {onOpenBriefing != null && (
          <>
            {' '}
            <button
              type="button"
              className="font-medium text-[var(--foreground)] underline-offset-2 hover:underline"
              onClick={onOpenBriefing}
            >
              Open Briefing
            </button>
          </>
        )}
        {onOpenDeliveryBoard != null && (
          <>
            {' · '}
            <button
              type="button"
              className="font-medium text-[var(--foreground)] underline-offset-2 hover:underline"
              onClick={onOpenDeliveryBoard}
            >
              Delivery Board
            </button>
            {' for program sign-off'}
          </>
        )}
        {onOpenBriefingReconciliation != null && (
          <>
            {' · '}
            <button
              type="button"
              className="font-medium text-[var(--foreground)] underline-offset-2 hover:underline"
              onClick={onOpenBriefingReconciliation}
            >
              Briefing Reconciliation
            </button>
            {' for sync loop'}
          </>
        )}
      </p>

      <BriefingFoldableSection
        kicker="Closure"
        title="Session results"
        description="Closed Ops-runner briefing sessions (S9 write-back). IDE work stays in Cursor — no auto-close required."
        defaultExpanded={false}
      >
        <BriefingSessionResultsPanel />
      </BriefingFoldableSection>

      <BriefingFoldableSection
        kicker="Automation"
        title="Nightly agent report & drift proposals"
        description="Layer 1–4 scan from agent host. Owner approval required for fixes."
        defaultExpanded={false}
      >
        <NightlyBriefingPanel />
      </BriefingFoldableSection>

      <BriefingFoldableSection
        kicker="Automation"
        title="Since your last session"
        description={
          previousSnapshot != null
            ? `Baseline: ${new Date(previousSnapshot.savedAt).toLocaleString()}${serverSnapshotQuery.data != null ? ' (server)' : ' (local)'}`
            : 'First session — snapshot saved when you copy a Briefing pack.'
        }
        defaultExpanded={false}
        badge={sessionDelta != null && !isEmptyDelta(sessionDelta) ? 'CHANGES' : undefined}
        badgeVariant="info"
      >
        <SessionDeltaPanel
          delta={sessionDelta}
          hasBaseline={previousSnapshot != null}
        />
      </BriefingFoldableSection>

      <BriefingFoldableSection
        kicker="Meta"
        title="Align Briefing with the system"
        description="Not for ops / release work. Use only when Briefing itself drifted from Console tabs or platform-api routes."
        defaultExpanded={false}
      >
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setShowAlignmentPack(true)}
          >
            Generate alignment task
          </Button>
          {showAlignmentPack && (
            <Button type="button" variant="outline" size="sm" onClick={() => void handleCopyAlignment()}>
              {alignmentCopied ? 'Copied!' : 'Copy alignment pack'}
            </Button>
          )}
        </div>
        {showAlignmentPack && (
          <pre className="llm-content-pre mt-3 max-h-64 overflow-auto font-mono-tabular text-[var(--text-dense-caption)]">
            {alignmentPack}
          </pre>
        )}
      </BriefingFoldableSection>
    </div>
  )
}
