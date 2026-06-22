import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { AuditRecord, ClusterSummary, MatrixResponse, OpsContextResponse } from '@/api/types'
import { fetchClusterObservability, fetchRemediationJobs } from '@/api/platform'
import { Button, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell, SegmentControl } from '@bifrost/ui'
import { StatusLamp } from '@/components/StatusLamp'
import { SessionDeltaPanel } from '@/components/briefing/SessionDeltaPanel'
import { NightlyBriefingPanel } from '@/components/briefing/NightlyBriefingPanel'
import { TrackCardsSection } from '@/components/briefing/TrackCardsSection'
import { buildBriefingAlignmentPack } from '@/lib/briefing/buildBriefingAlignmentPack'
import { buildBriefingPack } from '@/lib/briefing/buildBriefingPack'
import {
  AGENT_DIALOGUE_LANGUAGE_OPTIONS,
  DEFAULT_AGENT_DIALOGUE_LANGUAGE,
  type AgentDialogueLanguage,
} from '@/lib/briefing/agentDialogueLanguage'
import { computeSessionDelta, type SessionDelta } from '@/lib/briefing/sessionDiff'
import { loadSnapshot, saveSnapshot } from '@/lib/briefing/sessionSnapshot'
import { CONSOLE_UI_PROGRESS, type UiItemStatus } from '@/lib/briefing/uiProgressSnapshot'
import { TrackLaneSection } from '@/components/briefing/TrackLaneSection'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import {
  buildQueueForLane,
  defaultLaneForTrack,
  laneById,
  type LaneId,
} from '@/lib/briefing/workLanes'
import { computeAllTracks, type TrackId } from '@/lib/briefing/workTracks'
import { summarizeCluster } from '@/lib/cluster/clusterHealth'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'

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
  onOpenAgentDesk?: (jobId?: string) => void
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
}: BriefingPageProps) {
  const [selectedTrack, setSelectedTrack] = useState<TrackId>('build')
  const [selectedLane, setSelectedLane] = useState<LaneId>(() =>
    defaultLaneForTrack('build'),
  )
  const [initialLaneSynced, setInitialLaneSynced] = useState(false)
  const [showSessionPack, setShowSessionPack] = useState(false)
  const [showAlignmentPack, setShowAlignmentPack] = useState(false)
  const [sessionCopied, setSessionCopied] = useState(false)
  const [alignmentCopied, setAlignmentCopied] = useState(false)
  const [agentDialogueLanguage, setAgentDialogueLanguage] = useState<AgentDialogueLanguage>(
    DEFAULT_AGENT_DIALOGUE_LANGUAGE,
  )

  const [previousSnapshot] = useState(() => loadSnapshot())
  const [sessionDelta, setSessionDelta] = useState<SessionDelta | null>(null)

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
    setSelectedLane(defaultLaneForTrack(selectedTrack, context, matrices, clusterSummary))
    setInitialLaneSynced(true)
  }, [
    dataReady,
    initialLaneSynced,
    selectedTrack,
    context,
    matrices,
    clusterSummary,
  ])

  const intent: WorkIntent = laneById(selectedLane).workIntent

  const laneQueue = useMemo(
    () => buildQueueForLane(selectedLane, context, matrices, clusterSummary),
    [selectedLane, context, matrices, clusterSummary],
  )

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

  function handleSaveSnapshot() {
    saveSnapshot(
      { context, matrices, clusterSummary, platformHealthy },
      auditRecords,
      remediationJobs,
    )
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
    handleSaveSnapshot()
    setSessionCopied(true)
    window.setTimeout(() => setSessionCopied(false), 2000)
  }

  async function handleCopyAlignment() {
    await copyText(alignmentPack)
    setAlignmentCopied(true)
    window.setTimeout(() => setAlignmentCopied(false), 2000)
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <NightlyBriefingPanel onOpenAgentDesk={onOpenAgentDesk} />
      <SessionDeltaPanel
        delta={sessionDelta}
        hasBaseline={previousSnapshot != null}
        onOpenAgentDesk={onOpenAgentDesk}
      />

      <TrackCardsSection
        tracks={trackSummaries}
        selectedTrack={selectedTrack}
        onSelectTrack={(id) => {
          setSelectedTrack(id)
          setSelectedLane(defaultLaneForTrack(id, context, matrices, clusterSummary))
          setShowSessionPack(false)
        }}
      />

      <TrackLaneSection
        track={selectedTrack}
        selectedLane={selectedLane}
        onSelectLane={(id) => {
          setSelectedLane(id)
          setShowSessionPack(false)
        }}
        context={context}
        matrices={matrices}
        clusterSummary={clusterSummary}
      />

      <section className="page-section panel-elevated px-4 py-3">
        <p className="briefing-section-kicker m-0">2 · Session briefing</p>
        <div className="mt-1 flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <h2 className="m-0 text-sm font-semibold">Generate briefing for your work</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Paste into a <strong>new</strong> Cursor chat before starting the task you selected
              above. Content includes live status, UI progress, spine + matrix, intent-specific
              read-first / opening prompt, and a required first-reply protocol (confirm understanding
              + task list for you to pick).
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
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              size="sm"
              disabled={!dataReady}
              onClick={() => setShowSessionPack(true)}
            >
              {dataReady ? 'Generate session briefing' : 'Loading spine & matrix…'}
            </Button>
            {showSessionPack && (
              <Button variant="outline" size="sm" onClick={() => void handleCopySession()}>
                {sessionCopied ? 'Copied!' : 'Copy session pack'}
              </Button>
            )}
          </div>
        </div>

        {showSessionPack && (
          <LlmPackPreview
            charCount={sessionPack.length}
            metaLabel={`track: ${selectedTrack} · lane: ${selectedLane} · lang: ${agentDialogueLanguage}`}
            pack={sessionPack}
            footer="The Agent must first reply in your selected language with: (1) briefing understanding for confirmation, (2) a numbered task list, (3) a Source Audit table (provenance per fact + contradiction report) — wait for your selection before implementing."
          />
        )}
      </section>

      <section className="page-section panel-elevated px-4 py-3">
        <h2 className="m-0 text-sm font-semibold">Live snapshot</h2>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Embedded in both packs below when you generate.
        </p>
        <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <SnapshotTile
            label="Platform API"
            reach={platformHealthy === true ? 'ok' : platformHealthy === false ? 'fail' : 'unknown'}
            detail={platformHealthy === true ? 'healthy' : 'unreachable'}
          />
          <SnapshotTile
            label="Spine focus"
            reach={context != null ? 'ok' : 'unknown'}
            detail={contextLoading ? 'Loading…' : (context?.focus.headline ?? '—')}
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

      <section className="page-section panel-elevated overflow-hidden">
        <header className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
          <h2 className="m-0 text-sm font-semibold">Console UI progress</h2>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {CONSOLE_UI_PROGRESS.filter(r => r.status === 'done').length} done ·{' '}
            {CONSOLE_UI_PROGRESS.filter(r => r.status === 'partial').length} partial ·{' '}
            {CONSOLE_UI_PROGRESS.filter(r => r.status === 'planned').length} planned
          </span>
        </header>
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
      </section>

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
}: {
  label: string
  reach: 'ok' | 'degraded' | 'fail' | 'unknown'
  detail: string
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
    </div>
  )
}
