import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { AuditRecord, ClusterSummary, MatrixResponse, OpsContextResponse } from '@/api/types'
import { fetchClusterObservability, fetchRemediationJobs, fetchSessionSnapshotLatest } from '@/api/platform'
import { Button, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell, SegmentControl } from '@bifrost/ui'
import { StatusLamp } from '@/components/StatusLamp'
import { SessionDeltaPanel } from '@/components/briefing/SessionDeltaPanel'
import { NightlyBriefingPanel } from '@/components/briefing/NightlyBriefingPanel'
import { TrackCardsSection } from '@/components/briefing/TrackCardsSection'
import { BuildPhaseGatePanel } from '@/components/briefing/BuildPhaseGatePanel'
import { BriefingPhase1SignoffPanel } from '@/components/briefing/BriefingPhase1SignoffPanel'
import { BriefingPhase2SignoffPanel } from '@/components/briefing/BriefingPhase2SignoffPanel'
import { BriefingPhase3SignoffPanel } from '@/components/briefing/BriefingPhase3SignoffPanel'
import { BriefingPhase4SignoffPanel } from '@/components/briefing/BriefingPhase4SignoffPanel'
import { BriefingRoadmapStatusStrip } from '@/components/briefing/BriefingRoadmapStatusStrip'
import { BriefingSessionResultsPanel } from '@/components/briefing/BriefingSessionResultsPanel'
import { BriefingFoldableSection } from '@/components/briefing/BriefingFoldableSection'
import { buildBriefingAlignmentPack } from '@/lib/briefing/buildBriefingAlignmentPack'
import { buildBriefingPack } from '@/lib/briefing/buildBriefingPack'
import {
  parseBriefingUrlState,
  writeBriefingUrlState,
  type BriefingPackSize,
} from '@/lib/briefing/briefingUrlState'
import {
  AGENT_DIALOGUE_LANGUAGE_OPTIONS,
  DEFAULT_AGENT_DIALOGUE_LANGUAGE,
  type AgentDialogueLanguage,
} from '@/lib/briefing/agentDialogueLanguage'
import { computeSessionDelta, isEmptyDelta, type SessionDelta } from '@/lib/briefing/sessionDiff'
import { loadSnapshot, saveSnapshot, type SessionSnapshot } from '@/lib/briefing/sessionSnapshot'
import {
  agentDeskPrefillDisabledReason,
  BRIEFING_AGENT_DESK_DELIVERY_HINT,
  BRIEFING_IDE_DELIVERY_HINT,
  isAgentDeskSuitedIntent,
} from '@/lib/briefing/briefingDeliveryChannels'
import { saveBriefingActiveSession } from '@/lib/briefing/briefingActiveSession'
import {
  buildBriefingAutomationHandoff,
  formatAutomationHandoffJson,
} from '@/lib/briefing/briefingAutomationHandoff'
import { CONSOLE_UI_PROGRESS, type UiItemStatus } from '@/lib/briefing/uiProgressSnapshot'
import { TrackLaneSection } from '@/components/briefing/TrackLaneSection'
import { BriefingSyncLoopPanel } from '@/components/briefing/BriefingSyncLoopPanel'
import { BriefingReconcilePanel } from '@/components/briefing/BriefingReconcilePanel'
import { BriefingHealthBanner } from '@/components/briefing/BriefingHealthBanner'
import {
  buildReconcileBriefingOptions,
  deriveFocusHeadline,
  hasBlockingFindings,
  reconcileBriefing,
} from '@/lib/briefing/reconcileBriefing'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import { WORK_INTENT_OPTIONS } from '@/lib/briefing/workIntents'
import {
  buildQueueForLane,
  defaultLaneForTrack,
  laneById,
  type LaneId,
} from '@/lib/briefing/workLanes'
import { computeAllTracks, type TrackId } from '@/lib/briefing/workTracks'
import { summarizeCluster } from '@/lib/cluster/clusterHealth'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'
import { CATALOG_VERSION } from '@/lib/environments-catalog'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

interface BriefingPageProps {
  context: OpsContextResponse | undefined
  contextLoading: boolean
  matrices: MatrixResponse[]
  matrixLoading: boolean
  clusterSummary: ClusterSummary | undefined
  clusterLoading: boolean
  platformHealthy: boolean | undefined
  auditRecords: AuditRecord[]
  auditLoading: boolean
  onOpenAgentDesk?: (arg?: string | { prefill: string }) => void
  onOpenAudit?: () => void
}

async function copyText(text: string): Promise<void> {
  await navigator.clipboard.writeText(text)
}

function statusLamp(status: UiItemStatus) {
  if (status === 'done') return 'ok' as const
  if (status === 'partial') return 'degraded' as const
  return 'unknown' as const
}

export function BriefingPage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  clusterSummary,
  clusterLoading,
  platformHealthy,
  auditRecords,
  auditLoading,
  onOpenAgentDesk,
  onOpenAudit,
}: BriefingPageProps) {
  const initialUrl = useMemo(() => parseBriefingUrlState(), [])
  const [selectedTrack, setSelectedTrack] = useState<TrackId>(initialUrl.track ?? 'build')
  const [selectedLane, setSelectedLane] = useState<LaneId>(() => {
    if (initialUrl.lane != null) return initialUrl.lane
    return defaultLaneForTrack(initialUrl.track ?? 'build')
  })
  const [intentOverride, setIntentOverride] = useState<WorkIntent | null>(
    initialUrl.intent ?? null,
  )
  const [packSize, setPackSize] = useState<BriefingPackSize>(initialUrl.pack ?? 'compact')
  const [initialLaneSynced, setInitialLaneSynced] = useState(false)
  const [showSessionPack, setShowSessionPack] = useState(false)
  const [showAlignmentPack, setShowAlignmentPack] = useState(false)
  const [sessionCopied, setSessionCopied] = useState(false)
  const [automationCopied, setAutomationCopied] = useState(false)
  const [alignmentCopied, setAlignmentCopied] = useState(false)
  const [agentDialogueLanguage, setAgentDialogueLanguage] = useState<AgentDialogueLanguage>(
    DEFAULT_AGENT_DIALOGUE_LANGUAGE,
  )

  const { canOperate } = usePlatformAuth()
  const [localSnapshot] = useState(() => loadSnapshot())
  const [sessionDelta, setSessionDelta] = useState<SessionDelta | null>(null)
  const sessionPackAnchorRef = useRef<HTMLDivElement>(null)

  const serverSnapshotQuery = useQuery({
    queryKey: ['session-snapshot', 'latest'],
    queryFn: async () => {
      const res = await fetchSessionSnapshotLatest()
      return (res.snapshot ?? null) as SessionSnapshot | null
    },
    staleTime: 60_000,
  })

  const previousSnapshot = useMemo((): SessionSnapshot | null => {
    if (serverSnapshotQuery.data != null) return serverSnapshotQuery.data
    return localSnapshot
  }, [serverSnapshotQuery.data, localSnapshot])

  const remediationJobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 30_000,
  })

  const remediationJobs = remediationJobsQuery.data?.jobs ?? []

  const dataReady = !contextLoading && !matrixLoading && !auditLoading && !remediationJobsQuery.isLoading

  const trackSummaries = useMemo(() => {
    const clusterFailingPods = clusterSummary?.failing_pods
    const clusterReach = clusterSummary?.reachability
    return computeAllTracks(context, matrices, clusterFailingPods, clusterReach)
  }, [context, matrices, clusterSummary])

  useEffect(() => {
    if (!dataReady || initialLaneSynced) return
    if (initialUrl.lane == null) {
      setSelectedLane(defaultLaneForTrack(selectedTrack, context, matrices, clusterSummary))
    }
    setInitialLaneSynced(true)
  }, [
    dataReady,
    initialLaneSynced,
    initialUrl.lane,
    selectedTrack,
    context,
    matrices,
    clusterSummary,
  ])

  const laneDefaultIntent = laneById(selectedLane).workIntent
  const intent: WorkIntent = intentOverride ?? laneDefaultIntent

  useEffect(() => {
    writeBriefingUrlState({
      track: selectedTrack,
      lane: selectedLane,
      intent:
        intentOverride != null && intentOverride !== laneDefaultIntent
          ? intentOverride
          : undefined,
      pack: packSize === 'compact' ? undefined : packSize,
    })
  }, [selectedTrack, selectedLane, intentOverride, laneDefaultIntent, packSize])

  const laneQueue = useMemo(
    () => buildQueueForLane(selectedLane, context, matrices, clusterSummary),
    [selectedLane, context, matrices, clusterSummary],
  )

  const migrateTrackNext = useMemo(
    () => trackSummaries.find(t => t.id === 'migrate')?.nextStep ?? null,
    [trackSummaries],
  )

  const packReconcileOptions = useMemo(
    () =>
      buildReconcileBriefingOptions({
        context,
        selectedLane,
        laneQueue,
        migrateTrackNext,
      }),
    [laneQueue, selectedLane, migrateTrackNext, context],
  )

  const packFindings = useMemo(
    () => reconcileBriefing(context, packReconcileOptions),
    [context, packReconcileOptions],
  )

  const packBlocked = hasBlockingFindings(packFindings)

  const derivedFocusHeadline = useMemo(() => deriveFocusHeadline(context), [context])
  const focusHeadlineDrift =
    context != null &&
    derivedFocusHeadline.length > 0 &&
    context.focus.headline !== derivedFocusHeadline

  const catalogVersionSynced =
    context?.meta?.catalog_version != null &&
    context.meta.catalog_version === CATALOG_VERSION

  useEffect(() => {
    if (!dataReady || previousSnapshot == null) return
    const delta = computeSessionDelta(
      previousSnapshot,
      { context, matrices, clusterSummary, platformHealthy },
      auditRecords,
      remediationJobs,
    )
    setSessionDelta(delta)
  }, [dataReady, previousSnapshot, context, matrices, clusterSummary, platformHealthy, auditRecords, remediationJobs])

  async function handleSaveSnapshot() {
    await saveSnapshot(
      { context, matrices, clusterSummary, platformHealthy },
      auditRecords,
      remediationJobs,
    )
    void serverSnapshotQuery.refetch()
  }

  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
  })

  const snapshotInput = useMemo(
    () => ({
      context,
      matrices,
      clusterSummary,
      clusterObservability: observabilityQuery.data,
      platformHealthy,
    }),
    [context, matrices, clusterSummary, observabilityQuery.data, platformHealthy],
  )

  const sessionPack = useMemo(
    () =>
      buildBriefingPack({
        intent,
        packSize,
        sessionDelta,
        trackSummaries,
        selectedTrack,
        selectedLane,
        laneQueue,
        agentDialogueLanguage,
        ...snapshotInput,
      }),
    [
      intent,
      packSize,
      sessionDelta,
      trackSummaries,
      selectedTrack,
      selectedLane,
      laneQueue,
      agentDialogueLanguage,
      snapshotInput,
    ],
  )

  const alignmentPack = useMemo(
    () => buildBriefingAlignmentPack(snapshotInput),
    [snapshotInput],
  )

  const activeTrack = trackSummaries.find(t => t.id === selectedTrack) ?? trackSummaries[0]
  const activeLane = laneById(selectedLane)
  const clusterLine = summarizeCluster(clusterSummary)

  async function handleCopySession() {
    await copyText(sessionPack)
    await handleSaveSnapshot()
    setSessionCopied(true)
    window.setTimeout(() => setSessionCopied(false), 2000)
  }

  function handleSendToAgentDesk() {
    void handleSaveSnapshot()
    saveBriefingActiveSession({
      track: selectedTrack,
      lane: selectedLane,
      intent,
      packSize,
      startedAt: new Date().toISOString(),
    })
    onOpenAgentDesk?.({ prefill: sessionPack })
  }

  async function handleCopyAutomationHandoff() {
    const handoff = buildBriefingAutomationHandoff({
      pack: sessionPack,
      track: selectedTrack,
      lane: selectedLane,
      intent,
      packSize,
    })
    await copyText(formatAutomationHandoffJson(handoff))
    setAutomationCopied(true)
    window.setTimeout(() => setAutomationCopied(false), 2000)
  }

  async function handleCopyAlignment() {
    await copyText(alignmentPack)
    setAlignmentCopied(true)
    window.setTimeout(() => setAlignmentCopied(false), 2000)
  }

  const uiProgressDone = CONSOLE_UI_PROGRESS.filter(r => r.status === 'done').length
  const packHasWarnings = packFindings.some(f => f.severity === 'warning')
  const agentDeskSuited = isAgentDeskSuitedIntent(intent)
  const agentDeskDisabledReason = agentDeskPrefillDisabledReason(intent)

  function handleGenerateSessionPack() {
    setShowSessionPack(true)
    window.requestAnimationFrame(() => {
      sessionPackAnchorRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    })
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <BriefingRoadmapStatusStrip />

      <TrackCardsSection
        tracks={trackSummaries}
        selectedTrack={selectedTrack}
        onSelectTrack={(id) => {
          setSelectedTrack(id)
          setSelectedLane(defaultLaneForTrack(id, context, matrices, clusterSummary))
          setIntentOverride(null)
          setShowSessionPack(false)
        }}
      />

      {selectedTrack === 'build' && <BuildPhaseGatePanel />}

      <TrackLaneSection
        track={selectedTrack}
        selectedLane={selectedLane}
        onSelectLane={(id) => {
          setSelectedLane(id)
          setIntentOverride(null)
          setShowSessionPack(false)
        }}
        context={context}
        matrices={matrices}
        clusterSummary={clusterSummary}
        migrateTrackNext={migrateTrackNext}
        auditRecords={auditRecords}
        auditLoading={auditLoading}
        onOpenAudit={onOpenAudit}
      />

      <section className="page-section panel-elevated px-4 py-3">
        <p className="briefing-section-kicker m-0">2 · Session briefing</p>
        <div className="mt-1 flex min-w-0 flex-col gap-3">
          <div className="min-w-0">
            <h2 className="m-0 text-sm font-semibold">Generate briefing for your work</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              <strong>Primary:</strong> copy the pack into a <strong>new Cursor IDE</strong> chat
              (multi-repo workspace, rules, MCP). The pack includes live status, UI progress, spine
              + matrix, read-first lists, and a first-reply protocol — confirm understanding, task
              list, Source Audit — before you pick what to implement.
            </p>
            <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              <strong>Optional:</strong> prefill Agent Desk only for short Ops-runner tasks (cluster
              debug, drift-style prompts). It does not replace IDE for feature or frontend work.
            </p>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <span className="text-dense-meta text-muted-foreground">
                Agent dialogue language
              </span>
              <SegmentControl
                value={agentDialogueLanguage}
                onChange={(v) => {
                  setAgentDialogueLanguage(v as AgentDialogueLanguage)
                  setShowSessionPack(false)
                }}
                options={AGENT_DIALOGUE_LANGUAGE_OPTIONS.map(opt => ({
                  value: opt.id,
                  label: opt.label,
                }))}
                size="sm"
              />
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <span className="text-dense-meta text-muted-foreground">Work intent</span>
              <SegmentControl
                value={intent}
                onChange={(v) => {
                  const next = v as WorkIntent
                  setIntentOverride(next === laneDefaultIntent ? null : next)
                  setShowSessionPack(false)
                }}
                options={WORK_INTENT_OPTIONS.map(opt => ({
                  value: opt.id,
                  label: opt.shortLabel,
                }))}
                size="sm"
              />
              {intentOverride != null && intentOverride !== laneDefaultIntent && (
                <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  Override (lane default: {laneDefaultIntent})
                </span>
              )}
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <span className="text-dense-meta text-muted-foreground">Pack size</span>
              <SegmentControl
                value={packSize}
                onChange={(v) => {
                  setPackSize(v as BriefingPackSize)
                  setShowSessionPack(false)
                }}
                options={[
                  { value: 'compact', label: 'Compact' },
                  { value: 'full', label: 'Full' },
                ]}
                size="sm"
              />
            </div>
            <p className="m-0 mt-2 text-[var(--text-dense-meta)]">
              Track:{' '}
              <span className="font-medium text-[var(--foreground)] capitalize">
                {activeTrack.id}
              </span>
              {' · '}
              Lane:{' '}
              <span className="font-medium text-[var(--foreground)]">
                {activeLane.label}
              </span>
              <span className="text-[var(--muted-foreground)]">
                {' '}
                ({laneQueue.length} queue item{laneQueue.length !== 1 ? 's' : ''})
              </span>
            </p>
            <BriefingReconcilePanel
              context={context}
              options={packReconcileOptions}
              variant="pack"
            />
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              size="sm"
              disabled={!dataReady || packBlocked}
              onClick={handleGenerateSessionPack}
              title={
                packBlocked
                  ? 'Resolve pack reconcile blockers before generating'
                  : undefined
              }
            >
              {packBlocked
                ? 'Pack blocked (reconcile)'
                : dataReady
                  ? showSessionPack
                    ? 'Regenerate session briefing'
                    : 'Generate session briefing'
                  : 'Loading spine & matrix…'}
            </Button>
            {showSessionPack && (
              <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
                Pack ready — copy below or scroll to preview
              </span>
            )}
          </div>
        </div>

        {showSessionPack && (
          <div
            ref={sessionPackAnchorRef}
            className="mt-3 flex w-full min-w-0 scroll-mt-4 flex-col gap-3 border-t border-[var(--border)] pt-3"
          >
            <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:gap-6">
              <div className="min-w-0 flex-1">
                <p className="briefing-section-kicker m-0">Cursor IDE · recommended</p>
                <div className="mt-1.5 flex flex-wrap gap-2">
                  <Button type="button" size="sm" onClick={() => void handleCopySession()}>
                    {sessionCopied ? 'Copied!' : 'Copy session pack'}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => void handleCopyAutomationHandoff()}
                  >
                    {automationCopied ? 'Copied!' : 'Copy automation handoff'}
                  </Button>
                </div>
                <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  {BRIEFING_IDE_DELIVERY_HINT}
                </p>
              </div>
              <div className="min-w-0 flex-1 sm:max-w-md">
                <p className="briefing-section-kicker m-0">Agent Desk · optional</p>
                <div className="mt-1.5 flex flex-wrap items-center gap-2">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={
                      !canOperate || onOpenAgentDesk == null || agentDeskDisabledReason != null
                    }
                    title={
                      !canOperate
                        ? 'Operator token required'
                        : agentDeskDisabledReason ?? undefined
                    }
                    onClick={handleSendToAgentDesk}
                  >
                    Prefill Agent Desk
                  </Button>
                  {agentDeskSuited && (
                    <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                      Ops runner
                    </span>
                  )}
                </div>
                <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  {agentDeskDisabledReason ?? BRIEFING_AGENT_DESK_DELIVERY_HINT}
                </p>
              </div>
            </div>
          </div>
        )}

        {showSessionPack && (
          <LlmPackPreview
            charCount={sessionPack.length}
            metaLabel={`track: ${selectedTrack} · lane: ${selectedLane} · intent: ${intent} · pack: ${packSize} · lang: ${agentDialogueLanguage}`}
            pack={sessionPack}
            footer="Paste into Cursor IDE for the full first-reply protocol. The Agent must reply in your selected language with: (1) briefing understanding for confirmation, (2) a numbered task list, (3) a Source Audit table — wait for your selection before implementing."
          />
        )}
      </section>

      <BriefingPhase1SignoffPanel />
      <BriefingPhase2SignoffPanel />
      <BriefingPhase3SignoffPanel />

      <BriefingFoldableSection
        kicker="Closure"
        title="Session results"
        description="Closed Ops-runner briefing sessions (S9 write-back). IDE work stays in Cursor — no auto-close required."
        defaultExpanded={false}
      >
        <BriefingSessionResultsPanel />
      </BriefingFoldableSection>

      <BriefingPhase4SignoffPanel />

      <section className="page-section panel-elevated px-4 py-3">
        <h2 className="m-0 text-sm font-semibold">Live snapshot</h2>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Embedded in both packs below when you generate. SYNC (pack reconcile) and HEALTH (operational
          probes) are independent axes — see Doctrine → Briefing Reconciliation.
        </p>
        <BriefingHealthBanner
          matrices={matrices}
          clusterSummary={clusterSummary}
          platformHealthy={platformHealthy}
          loading={matrixLoading || clusterLoading}
        />
        <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <SnapshotTile
            label="Platform API"
            reach={platformHealthy === true ? 'ok' : platformHealthy === false ? 'fail' : 'unknown'}
            detail={platformHealthy === true ? 'healthy' : 'unreachable'}
          />
          <SnapshotTile
            label="Catalog version"
            reach={
              contextLoading
                ? 'unknown'
                : catalogVersionSynced
                  ? 'ok'
                  : context?.meta?.catalog_version != null
                    ? 'degraded'
                    : 'unknown'
            }
            detail={contextLoading ? 'Loading…' : CATALOG_VERSION}
            subdetail={
              catalogVersionSynced
                ? 'Spine + environments-catalog aligned'
                : context?.meta?.catalog_version != null
                  ? `Spine: ${context.meta.catalog_version} — run check_spine_catalog.sh`
                  : undefined
            }
          />
          <SnapshotTile
            label="Spine focus"
            reach={
              focusHeadlineDrift
                ? 'degraded'
                : context != null
                  ? 'ok'
                  : 'unknown'
            }
            detail={
              contextLoading
                ? 'Loading…'
                : (context?.focus.headline ?? '—')
            }
            subdetail={
              focusHeadlineDrift
                ? `Derived: ${derivedFocusHeadline} (D-D drift — run wave SYNC or patch headline)`
                : undefined
            }
          />
          <SnapshotTile
            label="Cluster"
            reach={clusterLine.reach}
            detail={clusterLoading ? 'Loading…' : clusterLine.label}
          />
          <SnapshotTile
            label="Prod matrix"
            reach={
              matrices.find(m => m.environment === 'prod') != null
                ? summarizeMatrix(matrices.find(m => m.environment === 'prod')!).worstReach
                : 'unknown'
            }
            detail={
              matrixLoading
                ? 'Loading…'
                : (() => {
                    const prod = matrices.find(m => m.environment === 'prod')
                    if (prod == null) return '—'
                    const s = summarizeMatrix(prod)
                    return `ok ${s.ok} · fail ${s.fail}`
                  })()
            }
          />
        </div>
      </section>

      <BriefingFoldableSection
        kicker="Automation"
        title="Briefing sync loop"
        description="Detect → propose → approve → fix. Nightly drift pipeline status."
        defaultExpanded={false}
        badge={packBlocked ? 'BLOCKED' : packHasWarnings ? 'WARN' : 'CLEAR'}
        badgeVariant={packBlocked ? 'warning' : packHasWarnings ? 'info' : 'success'}
      >
        <BriefingSyncLoopPanel
          context={context}
          reconcileFindings={packFindings}
          onOpenAgentDesk={onOpenAgentDesk}
        />
      </BriefingFoldableSection>

      <BriefingFoldableSection
        kicker="Automation"
        title="Nightly agent report & drift proposals"
        description="Layer 1–4 scan from agent host. Owner approval required for fixes."
        defaultExpanded={false}
      >
        <NightlyBriefingPanel onOpenAgentDesk={onOpenAgentDesk} />
      </BriefingFoldableSection>

      <BriefingFoldableSection
        kicker="Automation"
        title="Since your last session"
        description={
          previousSnapshot != null
            ? `Baseline: ${new Date(previousSnapshot.savedAt).toLocaleString()}${serverSnapshotQuery.data != null ? ' (server)' : ' (local)'}`
            : 'First session — snapshot saved on briefing copy.'
        }
        defaultExpanded={false}
        badge={sessionDelta != null && !isEmptyDelta(sessionDelta) ? 'CHANGES' : undefined}
        badgeVariant="info"
      >
        <SessionDeltaPanel
          delta={sessionDelta}
          hasBaseline={previousSnapshot != null}
          onOpenAgentDesk={onOpenAgentDesk}
        />
      </BriefingFoldableSection>

      <BriefingFoldableSection
        title="Console UI progress"
        description={`${uiProgressDone}/${CONSOLE_UI_PROGRESS.length} done — derived from Console nav registry + overrides.`}
        defaultExpanded={false}
        className="page-section panel-elevated overflow-hidden px-0 py-3"
      >
        <div className="px-3">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Area</DenseTableHead>
                <DenseTableHead>Feature</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Notes</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {CONSOLE_UI_PROGRESS.map(row => (
                <DenseTableRow key={`${row.area}-${row.item}`}>
                  <DenseTableCell className="font-mono-tabular">{row.area}</DenseTableCell>
                  <DenseTableCell>{row.item}</DenseTableCell>
                  <DenseTableCell>
                    <StatusLamp value={statusLamp(row.status)} kind="reach" />{' '}
                    <span className="font-mono-tabular">{row.status}</span>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                    {row.notes}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </div>
      </BriefingFoldableSection>

      <section className="briefing-maintain-panel page-section px-4 py-3">
        <p className="briefing-section-kicker m-0">Meta · Platform maintenance</p>
        <div className="mt-1 flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <h2 className="m-0 text-sm font-semibold">Align Briefing with the system</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              <strong>Not</strong> for ops / debug / release work. Use this only when you need an
              Agent to update <em>this Briefing feature</em> — sync{' '}
              <code className="text-[var(--text-dense-meta)]">uiProgressSnapshot</code>, read-first
              lists, and docs with the live Console tabs and platform-api routes.
            </p>
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Typical trigger: after shipping a new Console tab or API endpoint.
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={!dataReady}
              onClick={() => setShowAlignmentPack(true)}
            >
              {dataReady ? 'Generate alignment task' : 'Loading…'}
            </Button>
            {showAlignmentPack && (
              <Button variant="outline" size="sm" onClick={() => void handleCopyAlignment()}>
                {alignmentCopied ? 'Copied!' : 'Copy alignment pack'}
              </Button>
            )}
          </div>
        </div>

        {showAlignmentPack && (
          <LlmPackPreview
            charCount={alignmentPack.length}
            metaLabel="briefing_alignment"
            pack={alignmentPack}
            footer="Paste into a new Cursor chat dedicated to Briefing drift — separate from your day-to-day work session."
          />
        )}
      </section>
    </div>
  )
}

function LlmPackPreview({
  charCount,
  metaLabel,
  pack,
  footer,
}: {
  charCount: number
  metaLabel: string
  pack: string
  footer: string
}) {
  return (
    <div className="llm-content-panel mt-3">
      <div className="llm-content-panel-toolbar">
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {charCount.toLocaleString()} chars · {metaLabel}
        </span>
      </div>
      <pre className="llm-content-pre font-mono-tabular">{pack}</pre>
      <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{footer}</p>
    </div>
  )
}

function SnapshotTile({
  label,
  reach,
  detail,
  subdetail,
}: {
  label: string
  reach: 'ok' | 'degraded' | 'fail' | 'unknown'
  detail: string
  subdetail?: string
}) {
  return (
    <div className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <div className="flex items-center gap-2">
        <StatusLamp value={reach} kind="reach" />
        <span className="text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          {label}
        </span>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense)]">{detail}</p>
      {subdetail != null && (
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--warning)]">{subdetail}</p>
      )}
    </div>
  )
}
