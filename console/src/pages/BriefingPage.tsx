import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { AuditRecord, ClusterSummary, MatrixResponse, OpsContextResponse } from '@/api/types'
import { fetchClusterObservability, fetchRemediationJobs, fetchSessionSnapshotLatest } from '@/api/platform'
import { prepareBriefingForIde } from '@/api/briefing'
import {
  isLikelyCursorIdeBrowser,
  launchCursorBriefingAfterPrepare,
} from '@/lib/briefing/briefingDeliveryChannels'
import { BriefingMasterDetail } from '@/components/briefing/BriefingMasterDetail'
import { BriefingViewTabsSection } from '@/components/briefing/BriefingViewTabsSection'
import { BriefingWorkDigestPanel } from '@/components/briefing/BriefingWorkDigestPanel'
import { buildBriefingPack } from '@/lib/briefing/buildBriefingPack'
import { ensureSessionForPack } from '@/lib/briefing/ensureSessionForPack'
import {
  parseBriefingUrlState,
  readBriefingTaskModeContext,
  resolveBriefingScope,
  resolveTrackType,
  writeBriefingUrlState,
  type BriefingPackSize,
} from '@/lib/briefing/briefingUrlState'
import {
  defaultLaneForScopeTrack,
  lanesForScope,
  lanesForScopeTrack,
  trackTypesForScope,
  type BriefingScopeId,
  type ComponentLineId,
  type WorkTrackType,
} from '@/lib/briefing/briefingViewTabs'
import {
  computeScopeWorkSummary,
  laneLifecycleFromQueue,
  type BriefingLaneLifecycleFilter,
} from '@/lib/briefing/briefingStatus'
import type { NewLaneReference } from '@/components/briefing/TrackLaneSection'
import {
  DEFAULT_AGENT_DIALOGUE_LANGUAGE,
  type AgentDialogueLanguage,
} from '@/lib/briefing/agentDialogueLanguage'
import { computeSessionDelta, type SessionDelta } from '@/lib/briefing/sessionDiff'
import { loadSnapshot, saveSnapshot, type SessionSnapshot } from '@/lib/briefing/sessionSnapshot'
import {
  loadBriefingActiveSession,
  saveBriefingActiveSession,
} from '@/lib/briefing/briefingActiveSession'
import { TrackLaneSection } from '@/components/briefing/TrackLaneSection'
import { SessionDetailSection } from '@/components/briefing/SessionDetailSection'
import {
  type SessionLifecycle,
} from '@/components/briefing/SessionLaneCtaBar'
import {
  buildReconcileBriefingOptions,
  hasBlockingFindings,
  reconcileBriefing,
} from '@/lib/briefing/reconcileBriefing'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import {
  buildQueueForLane,
  laneById,
  type LaneId,
} from '@/lib/briefing/workLanes'
import { isEmptyLaneInit } from '@/lib/briefing/laneInitPack'
import { computeAllTracks } from '@/lib/briefing/workTracks'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useOperateQueue } from '@/hooks/useOperateQueue'

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
  onOpenAudit?: () => void
}

async function copyText(text: string): Promise<void> {
  await navigator.clipboard.writeText(text)
}

export function BriefingPage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  clusterSummary,
  clusterLoading: _clusterLoading,
  platformHealthy,
  auditRecords,
  auditLoading,
  onOpenAudit,
}: BriefingPageProps) {
  const initialUrl = useMemo(() => {
    const parsed = parseBriefingUrlState()
    if (parsed.taskModeContext == null) {
      const ctx = readBriefingTaskModeContext()
      if (ctx != null) parsed.taskModeContext = ctx
    }
    return parsed
  }, [])
  const [selectedScope, setSelectedScope] = useState<BriefingScopeId>(() =>
    resolveBriefingScope(initialUrl),
  )
  const [selectedTrackType, setSelectedTrackType] = useState<WorkTrackType>(() =>
    resolveTrackType(initialUrl),
  )
  const [selectedLane, setSelectedLane] = useState<LaneId>(() => {
    if (initialUrl.lane != null) return initialUrl.lane
    return defaultLaneForScopeTrack(resolveBriefingScope(initialUrl), resolveTrackType(initialUrl))
  })
  const selectedTrack = laneById(selectedLane).track
  /** Intent always from selected lane — no UI override on Briefing page. */
  const intent: WorkIntent = laneById(selectedLane).workIntent
  const [packSize, setPackSize] = useState<BriefingPackSize>(initialUrl.pack ?? 'compact')
  const [initialLaneSynced, setInitialLaneSynced] = useState(false)
  const [sessionCopied, setSessionCopied] = useState(false)
  const [packPreviewExpanded, setPackPreviewExpanded] = useState(false)
  const [lifecycleFilter, setLifecycleFilter] = useState<BriefingLaneLifecycleFilter | null>(null)
  const [sessionLifecycle, setSessionLifecycle] = useState<SessionLifecycle>(() => {
    const active = loadBriefingActiveSession()
    if (active == null) return 'ready'
    const initialLane =
      initialUrl.lane ??
      defaultLaneForScopeTrack(resolveBriefingScope(initialUrl), resolveTrackType(initialUrl))
    return active.lane === initialLane ? 'active' : 'ready'
  })
  const [preparingCursor, setPreparingCursor] = useState(false)
  const [launchStatus, setLaunchStatus] = useState<string | null>(null)
  const [agentDialogueLanguage, setAgentDialogueLanguage] = useState<AgentDialogueLanguage>(
    DEFAULT_AGENT_DIALOGUE_LANGUAGE,
  )
  const [insideCursorBrowser] = useState(() => isLikelyCursorIdeBrowser())
  const [newLaneOpenToken, setNewLaneOpenToken] = useState(0)
  const [newLaneReference, setNewLaneReference] = useState<NewLaneReference | null>(null)

  const { canOperate } = usePlatformAuth()
  const operateQueueQuery = useOperateQueue()
  const [localSnapshot] = useState(() => loadSnapshot())
  const [sessionDelta, setSessionDelta] = useState<SessionDelta | null>(null)

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
    return computeAllTracks(
      context,
      matrices,
      clusterFailingPods,
      clusterReach,
      operateQueueQuery.data?.open,
    )
  }, [context, matrices, clusterSummary, operateQueueQuery.data?.open])

  /**
   * Same lane-queue truth as TrackLaneSection.
   * With a Summary lifecycle filter, include all track types so counts match portfolio digests.
   */
  const scopeWorkSummary = useMemo(() => {
    const lanes =
      lifecycleFilter != null
        ? lanesForScope(selectedScope)
        : lanesForScopeTrack(selectedScope, selectedTrackType)
    const queues = lanes.map(lane => {
      const queue = buildQueueForLane(lane.id, context, matrices, clusterSummary)
      return {
        label: lane.label,
        queue,
        lifecycle: laneLifecycleFromQueue(queue),
      }
    })
    const visible =
      lifecycleFilter == null
        ? queues
        : queues.filter(q => q.lifecycle === lifecycleFilter)
    return computeScopeWorkSummary(
      visible.map(({ label, queue }) => ({ label, queue })),
    )
  }, [selectedScope, selectedTrackType, lifecycleFilter, context, matrices, clusterSummary])

  /** Config / lane changes invalidate the "Active" session marker until re-copy. */
  function invalidateSessionPackUi() {
    setSessionLifecycle('ready')
    setSessionCopied(false)
    setPackPreviewExpanded(false)
    setLaunchStatus(null)
  }

  /** When digest filter changes, keep selected lane inside the visible set. */
  useEffect(() => {
    if (lifecycleFilter == null) return
    const lanes = lanesForScope(selectedScope)
    const matching = lanes.filter(lane => {
      const life = laneLifecycleFromQueue(
        buildQueueForLane(lane.id, context, matrices, clusterSummary),
      )
      return life === lifecycleFilter
    })
    if (matching.length === 0) return
    if (matching.some(l => l.id === selectedLane)) return
    setSelectedLane(matching[0].id)
    setSessionLifecycle('ready')
    setSessionCopied(false)
    setPackPreviewExpanded(false)
    setLaunchStatus(null)
  }, [
    lifecycleFilter,
    selectedScope,
    selectedLane,
    context,
    matrices,
    clusterSummary,
  ])

  useEffect(() => {
    if (!dataReady || initialLaneSynced) return
    if (initialUrl.lane == null) {
      setSelectedLane(defaultLaneForScopeTrack(selectedScope, selectedTrackType))
    }
    setInitialLaneSynced(true)
  }, [
    dataReady,
    initialLaneSynced,
    initialUrl.lane,
    selectedScope,
    selectedTrackType,
  ])

  useEffect(() => {
    writeBriefingUrlState({
      view: selectedScope,
      trackType: selectedTrackType,
      track: selectedTrack,
      lane: selectedLane,
      // Stop writing intent override from Briefing page; keep URL parse compat for deep links.
      intent: undefined,
      pack: packSize === 'compact' ? undefined : packSize,
    })
  }, [selectedScope, selectedTrackType, selectedTrack, selectedLane, packSize])

  function applyAllScope(clearFilter: boolean) {
    setSelectedScope('all')
    const types = trackTypesForScope('all')
    const tt = types.includes(selectedTrackType) ? selectedTrackType : types[0] ?? 'build'
    setSelectedTrackType(tt)
    setSelectedLane(defaultLaneForScopeTrack('all', tt))
    if (clearFilter) setLifecycleFilter(null)
    invalidateSessionPackUi()
  }

  function handleSelectLifecycleFilter(filter: BriefingLaneLifecycleFilter | null) {
    if (filter == null) {
      setLifecycleFilter(null)
      invalidateSessionPackUi()
      return
    }
    setLifecycleFilter(filter)
    setSelectedScope('all')
    const types = trackTypesForScope('all')
    const tt = types.includes(selectedTrackType) ? selectedTrackType : types[0] ?? 'build'
    setSelectedTrackType(tt)
    invalidateSessionPackUi()
  }

  function handleSelectHotLine(line: ComponentLineId) {
    setSelectedScope(line)
    const types = trackTypesForScope(line)
    const tt = types.includes(selectedTrackType) ? selectedTrackType : types[0] ?? 'build'
    setSelectedTrackType(tt)
    setLifecycleFilter('active')
    invalidateSessionPackUi()
  }

  function handleClearLifecycleFilter() {
    setLifecycleFilter(null)
    invalidateSessionPackUi()
  }

  const laneQueue = useMemo(
    () => buildQueueForLane(selectedLane, context, matrices, clusterSummary),
    [selectedLane, context, matrices, clusterSummary],
  )
  const selectedLaneLifecycle = useMemo(
    () => laneLifecycleFromQueue(laneQueue),
    [laneQueue],
  )
  const isArchiveLane = selectedLaneLifecycle === 'complete'

  /** Completed archive must never keep a work-Session ACTIVE marker. */
  useEffect(() => {
    if (!isArchiveLane) return
    setSessionLifecycle('ready')
    setLaunchStatus(null)
  }, [isArchiveLane, selectedLane])

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

  const taskModeCtx = readBriefingTaskModeContext()
  const linkedProgramId = taskModeCtx?.programId?.trim() || undefined
  /** Briefing Copy/Launch archives a Session Job bound to the playbook briefing phase. */
  const linkedPhaseId = linkedProgramId != null ? 'briefing' : undefined

  const sessionPack = useMemo(
    () =>
      buildBriefingPack({
        intent,
        packSize,
        sessionDelta,
        trackSummaries,
        selectedTrack,
        selectedLane,
        selectedScope,
        laneQueue,
        agentDialogueLanguage,
        taskModeContext: readBriefingTaskModeContext(),
        programId: linkedProgramId,
        phaseId: linkedPhaseId,
        ...snapshotInput,
      }),
    [
      intent,
      packSize,
      sessionDelta,
      trackSummaries,
      selectedTrack,
      selectedLane,
      selectedScope,
      laneQueue,
      agentDialogueLanguage,
      linkedProgramId,
      linkedPhaseId,
      snapshotInput,
    ],
  )

  const buildAnchoredPack = (sessionId: string | undefined) =>
    buildBriefingPack({
      intent,
      packSize,
      sessionDelta,
      trackSummaries,
      selectedTrack,
      selectedLane,
      selectedScope,
      laneQueue,
      agentDialogueLanguage,
      taskModeContext: readBriefingTaskModeContext(),
      sessionId,
      programId: linkedProgramId,
      phaseId: linkedPhaseId,
      ...snapshotInput,
    })

  const activeLane = laneById(selectedLane)

  function handleUseAsReferenceForNewLane() {
    const lane = laneById(selectedLane)
    setLifecycleFilter(null)
    setNewLaneReference({
      id: lane.id,
      label: lane.label,
      description: lane.description,
    })
    setNewLaneOpenToken(t => t + 1)
    setLaunchStatus(
      `Opening New Lane form with reference: ${lane.label}. Completed lane stays archive-only.`,
    )
  }

  async function handleOpenInCursor() {
    if (!canOperate) return
    if (isArchiveLane) {
      setLaunchStatus(
        'Completed lane is archive only — use New Lane (reference) to start work.',
      )
      return
    }
    setPreparingCursor(true)
    setLaunchStatus(null)
    try {
      const { pack, sessionId } = await ensureSessionForPack({
        programId: linkedProgramId,
        phaseId: linkedPhaseId,
        laneId: selectedLane,
        buildPack: buildAnchoredPack,
      })
      await handleSaveSnapshot()
      await prepareBriefingForIde({
        session_pack: pack,
        session_id: sessionId,
        program_id: linkedProgramId,
        phase_id: linkedPhaseId,
        lane: selectedLane,
        intent,
      })
      saveBriefingActiveSession({
        track: selectedTrack,
        lane: selectedLane,
        intent,
        packSize,
        startedAt: new Date().toISOString(),
      })
      setSessionLifecycle('active')
      const launch = launchCursorBriefingAfterPrepare()
      setLaunchStatus(launch.status)
    } catch (err) {
      setLaunchStatus(err instanceof Error ? err.message : 'Prepare failed')
    } finally {
      setPreparingCursor(false)
    }
  }

  async function handleCopySession() {
    try {
      const { pack } = await ensureSessionForPack({
        programId: isArchiveLane ? undefined : linkedProgramId,
        phaseId: isArchiveLane ? undefined : linkedPhaseId,
        laneId: selectedLane,
        buildPack: buildAnchoredPack,
      })
      await copyText(pack)
      if (isArchiveLane) {
        setSessionCopied(true)
        setLaunchStatus('Archive pack copied — read-only history; does not start a work Session.')
        window.setTimeout(() => setSessionCopied(false), 2000)
        return
      }
      await handleSaveSnapshot()
      saveBriefingActiveSession({
        track: selectedTrack,
        lane: selectedLane,
        intent,
        packSize,
        startedAt: new Date().toISOString(),
      })
      setSessionLifecycle('active')
      setSessionCopied(true)
      setLaunchStatus(null)
      window.setTimeout(() => setSessionCopied(false), 2000)
    } catch (err) {
      setLaunchStatus(err instanceof Error ? err.message : 'Copy session failed')
    }
  }


  const laneIsInitMode = isEmptyLaneInit(laneQueue)

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <BriefingWorkDigestPanel
        compact
        context={context}
        matrices={matrices}
        clusterSummary={clusterSummary}
        loading={contextLoading || matrixLoading}
        lifecycleFilter={lifecycleFilter}
        onSelectLifecycleFilter={handleSelectLifecycleFilter}
        onSelectHotLine={handleSelectHotLine}
        onClearFilters={handleClearLifecycleFilter}
        onFocusAllScope={() => applyAllScope(true)}
      />

      <BriefingMasterDetail
        master={
          <>
            <BriefingViewTabsSection
              selectedScope={selectedScope}
              selectedTrackType={selectedTrackType}
              onSelectScope={scope => {
                setSelectedScope(scope)
                const types = trackTypesForScope(scope)
                const tt = types.includes(selectedTrackType) ? selectedTrackType : types[0] ?? 'build'
                setSelectedTrackType(tt)
                setSelectedLane(defaultLaneForScopeTrack(scope, tt))
                invalidateSessionPackUi()
              }}
              onSelectTrackType={tt => {
                setSelectedTrackType(tt)
                setSelectedLane(defaultLaneForScopeTrack(selectedScope, tt))
                invalidateSessionPackUi()
              }}
              scopeWorkSummary={scopeWorkSummary}
              lifecycleFilter={lifecycleFilter}
              onClearLifecycleFilter={handleClearLifecycleFilter}
              context={context}
              matrices={matrices}
              clusterSummary={clusterSummary}
            />

            <TrackLaneSection
              scope={selectedScope}
              trackType={selectedTrackType}
              track={selectedTrack}
              selectedLane={selectedLane}
              onSelectLane={(id) => {
                setSelectedLane(id)
                invalidateSessionPackUi()
              }}
              lifecycleFilter={lifecycleFilter}
              onClearLifecycleFilter={handleClearLifecycleFilter}
              newLaneOpenToken={newLaneOpenToken}
              newLaneReference={newLaneReference}
              context={context}
              matrices={matrices}
              clusterSummary={clusterSummary}
            />
          </>
        }
        detail={
          !dataReady ? (
            <section className="page-section panel-elevated flex min-h-[12rem] flex-col items-center justify-center gap-1 px-4 py-8 text-center">
              <p className="briefing-section-kicker m-0">Session</p>
              <h2 className="m-0 text-sm font-semibold">Waiting for data</h2>
              <p className="m-0 max-w-sm text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Loading spine, matrix, and audit so the Session pack can be built. Pick a Doing or
                Backlog lane on the left — Completed lanes open as archive only.
              </p>
            </section>
          ) : (
            <SessionDetailSection
              scope={selectedScope}
              trackType={selectedTrackType}
              lane={activeLane}
              queue={laneQueue}
              isInitMode={laneIsInitMode}
              intent={intent}
              lifecycle={sessionLifecycle}
              laneLifecycle={selectedLaneLifecycle}
              dataReady={dataReady}
              packBlocked={packBlocked}
              canOperate={canOperate}
              preparingCursor={preparingCursor}
              sessionCopied={sessionCopied}
              launchStatus={launchStatus}
              insideCursorBrowser={insideCursorBrowser}
              onCopySession={() => void handleCopySession()}
              onOpenInCursor={() => void handleOpenInCursor()}
              onUseAsReferenceForNewLane={handleUseAsReferenceForNewLane}
              context={context}
              migrateTrackNext={migrateTrackNext}
              auditRecords={auditRecords}
              auditLoading={auditLoading}
              onOpenAudit={onOpenAudit}
              agentDialogueLanguage={agentDialogueLanguage}
              onAgentDialogueLanguageChange={v => {
                setAgentDialogueLanguage(v)
                invalidateSessionPackUi()
              }}
              packSize={packSize}
              onPackSizeChange={v => {
                setPackSize(v)
                invalidateSessionPackUi()
              }}
              packReconcileOptions={packReconcileOptions}
              packPreview={
                <LlmPackPreview
                  charCount={sessionPack.length}
                  metaLabel={`track: ${selectedTrack} · lane: ${selectedLane} · intent: ${intent} · pack: ${packSize} · lang: ${agentDialogueLanguage}${laneIsInitMode ? ' · init' : ''}${isArchiveLane ? ' · archive' : ''}`}
                  pack={sessionPack}
                  expanded={packPreviewExpanded}
                  onToggleExpanded={() => setPackPreviewExpanded(v => !v)}
                  footer={
                    isArchiveLane
                      ? 'Archive pack (read-only). Completed lanes do not start a work Session — use New Lane (reference) instead.'
                      : 'Open in Cursor (/briefing) or paste the pack into Cursor IDE for the first-reply protocol. The Agent must reply in your selected language with: (1) briefing understanding for confirmation, (2) a numbered task list, (3) Source Audit (full pack) — wait for your selection before implementing.'
                  }
                />
              }
            />
          )
        }
      />
    </div>
  )
}


function LlmPackPreview({
  charCount,
  metaLabel,
  pack,
  footer,
  expanded = true,
  onToggleExpanded,
}: {
  charCount: number
  metaLabel: string
  pack: string
  footer: string
  expanded?: boolean
  onToggleExpanded?: () => void
}) {
  const previewLines = pack.split('\n').slice(0, 4).join('\n')
  const collapsible = onToggleExpanded != null

  return (
    <div className="llm-content-panel mt-1">
      <div className="llm-content-panel-toolbar flex flex-wrap items-center justify-between gap-2">
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {charCount.toLocaleString()} chars · {metaLabel}
        </span>
        {collapsible && (
          <button
            type="button"
            className="text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            onClick={onToggleExpanded}
          >
            {expanded ? 'Collapse preview' : 'Expand preview'}
          </button>
        )}
      </div>
      {expanded ? (
        <pre className="llm-content-pre font-mono-tabular">{pack}</pre>
      ) : (
        <pre className="llm-content-pre max-h-24 overflow-hidden font-mono-tabular opacity-80">
          {previewLines}
          {pack.split('\n').length > 4 ? '\n…' : ''}
        </pre>
      )}
      <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{footer}</p>
    </div>
  )
}
