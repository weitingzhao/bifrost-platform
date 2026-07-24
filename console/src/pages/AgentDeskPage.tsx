import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag, SegmentControl, StatusLamp } from '@bifrost/ui'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronRight, Square } from 'lucide-react'
import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { AuditRecord } from '@/api/auditTypes'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { RemediationJob } from '@/api/remediationTypes'
import { cancelRemediationJob, fetchRemediationHealth, fetchRemediationJobs, startRemediation } from '@/api/remediation'
import { fetchAgentBridge } from '@/api/agentOps'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { CloseBriefingSessionDialog } from '@/components/briefing/CloseBriefingSessionDialog'
import { FlightDirectorBriefingPanel } from '@/components/briefing/FlightDirectorBriefingPanel'
import { BriefingFoldableSection } from '@/components/briefing/BriefingFoldableSection'
import { AgentDeskSessionOpsPanels } from '@/components/agent/AgentDeskSessionOpsPanels'
import { RemediationPanel } from '@/components/cluster/RemediationPanel'
import { AgentTaskCatalogPanel } from '@/components/agent/AgentTaskCatalogPanel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { PageToolbar } from '@/components/layout/PageToolbar'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePendingDecisionBriefs } from '@/hooks/useDecisionBriefs'
import type { OperateQueueItem } from '@/api/operateQueueTypes'
import { recordOperateQueueExecution, OPERATE_QUEUE_QUERY_KEY } from '@/api/operateQueue'
import { catalogTaskById } from '@/lib/agent/agentTaskCatalog'
import { buildBriefingDeepLink } from '@/lib/briefing/briefingUrlState'
import { isLaneId } from '@/lib/briefing/workLanes'
import { buildHandoffAgentPrompt } from '@/lib/operate/handoff'
import {
  attachJobToBriefingSession,
  loadBriefingActiveSession,
} from '@/lib/briefing/briefingActiveSession'
import {
  formatRemediationJobWhen,
  groupRemediationJobsByScope,
  remediationJobStatusLabel,
  remediationTimelineCellStatus,
} from '@/lib/remediation/remediationJobDisplay'

interface AgentDeskPageProps {
  context: OpsContextResponse | undefined
  matrices?: MatrixResponse[]
  clusterSummary?: ClusterSummary
  platformHealthy?: boolean
  auditRecords?: AuditRecord[]
  initialJobId?: string | null
  prefillPrompt?: string | null
  focusHandoffId?: string | null
  focusDecisionBriefs?: boolean
  onInitialJobConsumed?: () => void
  onPrefillConsumed?: () => void
  onFocusHandoffConsumed?: () => void
  onFocusDecisionBriefsConsumed?: () => void
  onOpenBriefing?: () => void
  onOpenCluster?: () => void
  onOpenMcpContract?: () => void
  onOpenAgentProtocol?: () => void
  onOpenAgentSystem?: () => void
  onOpenOperatorPlane?: () => void
  onOpenTrustAutonomy?: () => void
  onOpenDeliveryBoard?: () => void
  onOpenBriefingReconciliation?: () => void
}

type AgentScope = string
type AgentDeskView = 'operate' | 'observe' | 'review'

interface QuickPrompt {
  id: string
  label: string
  prompt: string
  scope?: AgentScope
}

const QUICK_PROMPTS: QuickPrompt[] = [
  {
    id: 'health',
    label: 'Cluster health',
    prompt:
      'Run a read-only cluster health check. Summarize node readiness, failing pods, and service readiness. Do not take destructive actions without operator approval.',
  },
  {
    id: 'spine',
    label: 'Spine focus',
    prompt:
      'Summarize the current ops spine focus, active automate tracks, and the single best next Owner action.',
  },
  {
    id: 'drift',
    label: 'Nightly drift brief',
    prompt:
      'Review the latest nightly drift scan context (read-only). Summarize Layer 1–3 findings from live scans and reports. Do NOT apply fixes or run Layer 4 auto-fix — report only.',
  },
  {
    id: 'release',
    label: 'Platform release',
    prompt:
      'Deploy latest changes to prod. Scan all repos (incl. bifrost-platform-plugin) for uncommitted changes, commit and push, run STG → Prod platform pipeline, then Phase G install-ib-gateway if plugin changed.',
    scope: 'release',
  },
  {
    id: 'deliver-stg-recover',
    label: 'Deliver STG Recover',
    prompt:
      'Last bifrost-deliver-stg PipelineRun failed. Use get_delivery_run_logs and get_stg_smoke first. If STG smoke is green, this is stale-fail — fix rollout/GitOps (not nodes), then start_pipeline_run bifrost-deliver-stg. D10: no live trading.',
    scope: 'deliver-stg-recover',
  },
]

function statusVariant(s: string | undefined): 'success' | 'warning' | 'neutral' | 'danger' {
  if (s === 'ok') return 'success'
  if (s === 'unavailable') return 'danger'
  if (s === 'not_configured') return 'neutral'
  return 'warning'
}

function runnerReachability(status: string | undefined): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (status === 'ok') return 'ok'
  if (status === 'unavailable') return 'fail'
  return 'unknown'
}

/** Collapsed Infrastructure summary for runner(s) — shows HA state when a standby exists. */
function runnerSummary(bridge: AgentBridgeResponse): string {
  const runners = bridge.runners ?? []
  if (runners.length >= 2) {
    const up = runners.filter(r => r.status === 'ok').length
    if (up === runners.length) return `Runners ${up}/${runners.length} (HA)`
    if (up === 0) return 'Runners down'
    return `Runners ${up}/${runners.length} — failover`
  }
  if (runners.length === 1) {
    return runners[0].status === 'ok' ? 'Runner ok (no standby)' : `Runner ${runners[0].status}`
  }
  return bridge.remediation_runner.status === 'ok'
    ? 'Runner ok'
    : `Runner ${bridge.remediation_runner.status}`
}

export function AgentDeskPage({
  context,
  matrices = [],
  clusterSummary,
  platformHealthy,
  auditRecords = [],
  initialJobId,
  prefillPrompt,
  focusHandoffId,
  focusDecisionBriefs = false,
  onInitialJobConsumed,
  onPrefillConsumed,
  onFocusHandoffConsumed,
  onFocusDecisionBriefsConsumed,
  onOpenBriefing,
  onOpenCluster,
  onOpenMcpContract,
  onOpenAgentProtocol,
  onOpenAgentSystem,
  onOpenOperatorPlane,
  onOpenTrustAutonomy,
  onOpenDeliveryBoard,
  onOpenBriefingReconciliation,
}: AgentDeskPageProps) {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [composerText, setComposerText] = useState('')
  const [selectedScope, setSelectedScope] = useState<AgentScope>('agent-desk')
  const [jobId, setJobId] = useState<string | null>(null)
  const [initialJob, setInitialJob] = useState<RemediationJob | null>(null)
  const [panelOpen, setPanelOpen] = useState(false)
  const [userPrompts, setUserPrompts] = useState<Record<string, string>>({})
  const [stopConfirm, setStopConfirm] = useState<{ jobId: string; label: string } | null>(null)
  const [closeSessionJob, setCloseSessionJob] = useState<RemediationJob | null>(null)
  const [trackedJob, setTrackedJob] = useState<RemediationJob | null>(null)
  const [handoffLinkError, setHandoffLinkError] = useState<string | null>(null)
  const [deskView, setDeskView] = useState<AgentDeskView>(
    initialJobId != null ? 'observe' : 'operate',
  )
  const briefingActiveSession = loadBriefingActiveSession()
  const operateQueueQuery = useOperateQueue()
  const decisionBriefsQuery = usePendingDecisionBriefs()

  useEffect(() => {
    if (initialJobId == null) return
    setJobId(initialJobId)
    setPanelOpen(true)
    setDeskView('observe')
    onInitialJobConsumed?.()
  }, [initialJobId, onInitialJobConsumed])

  useEffect(() => {
    if (initialJob != null) setTrackedJob(initialJob)
  }, [initialJob])

  useEffect(() => {
    if (prefillPrompt == null || prefillPrompt === '') return
    setComposerText(prefillPrompt)
    if (initialJobId == null) setDeskView('operate')
    onPrefillConsumed?.()
  }, [initialJobId, prefillPrompt, onPrefillConsumed])

  useEffect(() => {
    if (focusHandoffId == null || focusHandoffId === '') return
    setDeskView('operate')
  }, [focusHandoffId])

  useEffect(() => {
    if (!focusDecisionBriefs) return
    setDeskView('operate')
    const frame = window.requestAnimationFrame(() => {
      const el = document.querySelector('[data-decision-brief-id]')
      if (el instanceof HTMLElement) {
        el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
      }
      onFocusDecisionBriefsConsumed?.()
    })
    return () => window.cancelAnimationFrame(frame)
  }, [focusDecisionBriefs, onFocusDecisionBriefsConsumed])

  const healthQuery = useQuery({
    queryKey: ['remediation', 'health'],
    queryFn: fetchRemediationHealth,
    refetchInterval: 60_000,
  })

  const bridgeQuery = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 60_000,
  })

  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: panelOpen ? 15_000 : 60_000,
  })

  const jobGroups = useMemo(
    () => groupRemediationJobsByScope(jobsQuery.data?.jobs ?? []),
    [jobsQuery.data?.jobs],
  )

  const startMutation = useMutation({
    mutationFn: async ({ prompt, scope }: { prompt: string; scope: AgentScope; handoff?: OperateQueueItem }) => {
      const spineNote =
        context?.focus?.headline != null ? `Spine focus: ${context.focus.headline}\n\n` : ''
      return startRemediation({
        scope,
        prompt: `${spineNote}${prompt.trim()}`,
      })
    },
    onSuccess: (job, { prompt, handoff }) => {
      setUserPrompts(prev => ({ ...prev, [job.id]: prompt }))
      setInitialJob(job)
      setTrackedJob(job)
      setJobId(job.id)
      setPanelOpen(true)
      setDeskView('observe')
      setComposerText('')
      attachJobToBriefingSession(job.id)
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      if (handoff != null) {
        setHandoffLinkError(null)
        void recordOperateQueueExecution(handoff.id, job.id)
          .then(() => qc.invalidateQueries({ queryKey: OPERATE_QUEUE_QUERY_KEY }))
          .catch(error => setHandoffLinkError((error as Error).message))
      }
    },
  })

  const cancelMutation = useMutation({
    mutationFn: cancelRemediationJob,
    onSuccess: job => {
      setInitialJob(job)
      setStopConfirm(null)
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
    },
  })

  const runnerStatus = healthQuery.data?.status
  const runnerHealthy = runnerStatus === 'ok'
  const runnerHasCursorKey = healthQuery.data?.cursor_api_key === true
  const runnerBlocked = !runnerHealthy
  const runnerWarnCursor = runnerHealthy && !runnerHasCursorKey

  const bridge = bridgeQuery.data
  const gitBridgeStatus = bridge?.git_bridge?.status
  const nowSummary = useMemo(() => {
    const cutoff = Date.now() - 86_400_000
    const jobs = jobsQuery.data?.jobs ?? []
    return {
      activeTasks: jobs.filter(
        job => job.status === 'running' && job.phase !== 'awaiting_approval',
      ).length,
      pendingApprovals: jobs.filter(
        job => job.status === 'running' && job.phase === 'awaiting_approval',
      ).length,
      openHandoffs: operateQueueQuery.data?.open.length ?? 0,
      blockingFailures: jobs.filter(
        job => job.status === 'failed' && Date.parse(job.updated_at) >= cutoff,
      ).length,
    }
  }, [jobsQuery.data?.jobs, operateQueueQuery.data?.open.length])

  const handleSend = useCallback(
    (text: string, scopeOverride?: AgentScope) => {
      const trimmed = text.trim()
      if (trimmed === '' || !canOperate || runnerBlocked) return
      startMutation.mutate({ prompt: trimmed, scope: scopeOverride ?? selectedScope })
    },
    [canOperate, runnerBlocked, startMutation, selectedScope],
  )

  const activeUserPrompt = jobId != null ? userPrompts[jobId] : undefined

  const handleOpenHandoffSource = useCallback(
    (item: OperateQueueItem) => {
      if (item.source === 'post_completion') {
        const lane = item.source_lane_id != null && isLaneId(item.source_lane_id)
          ? item.source_lane_id
          : undefined
        window.location.assign(buildBriefingDeepLink({ lane, program: item.program_id }))
      }
      else onOpenDeliveryBoard?.()
    },
    [onOpenBriefing, onOpenDeliveryBoard],
  )

  const handlePrepareHandoffAgent = useCallback((item: OperateQueueItem) => {
    setSelectedScope('agent-desk')
    setComposerText(
      [
        `Execute the approved operate handoff for program ${item.program_id}.`,
        `Handoff: ${item.title}`,
        item.description != null && item.description !== '' ? `Context: ${item.description}` : '',
        item.pending_id != null && item.pending_id !== '' ? `Source item: ${item.pending_id}` : '',
        'Verify the source context before acting. Do not enable live trading (D10). Do not close the handoff until execution is complete and verified.',
      ].filter(Boolean).join('\n'),
    )
    setDeskView('operate')
  }, [])

  const handleStartHandoffAgent = useCallback((item: OperateQueueItem) => {
    if (item.agent_task_id == null) {
      handlePrepareHandoffAgent(item)
      return
    }
    const task = catalogTaskById(item.agent_task_id)
    if (task == null || !canOperate || runnerBlocked) {
      handlePrepareHandoffAgent(item)
      return
    }
    const prompt = buildHandoffAgentPrompt(item)
    startMutation.mutate({ prompt, scope: task.scope, handoff: item })
  }, [canOperate, handlePrepareHandoffAgent, runnerBlocked, startMutation])

  const handleObserveHandoffJob = useCallback((targetJobId: string) => {
    setJobId(targetJobId)
    setPanelOpen(true)
    setDeskView('observe')
  }, [])

  return (
    <div className={`agent-desk-shell${panelOpen ? ' agent-desk-shell--panel-open' : ''}`}>
      <div className="agent-desk-main flex min-w-0 flex-col gap-3">

        {/* Status + view chrome — page identity lives in ConsoleHeader breadcrumb */}
        <section className="agent-desk-hero">
          <PageToolbar align="between">
            <div className="agent-desk-status-bar">
              <StatusLamp value={runnerReachability(runnerStatus)} kind="reach" />
              <DenseTag variant={statusVariant(runnerStatus)}>Runner</DenseTag>
              <DenseTag variant={statusVariant(gitBridgeStatus)}>
                Git Bridge
                {gitBridgeStatus === 'ok' &&
                bridge?.git_bridge?.dirty_repos != null &&
                bridge.git_bridge.dirty_repos > 0
                  ? ` · ${bridge.git_bridge.dirty_repos} dirty ${bridge.git_bridge.dirty_repos === 1 ? 'repo' : 'repos'}`
                  : ''}
              </DenseTag>
            </div>
            <div className="flex items-center gap-1.5">
              {onOpenBriefing != null && (
                <Button type="button" variant="ghost" size="sm" onClick={onOpenBriefing}>
                  Briefing
                </Button>
              )}
              {onOpenCluster != null && (
                <Button type="button" variant="ghost" size="sm" onClick={onOpenCluster}>
                  Cluster
                </Button>
              )}
            </div>
          </PageToolbar>
          {context?.focus?.headline != null && (
            <p className="agent-desk-spine-hint mt-1">
              {context.focus.headline}
            </p>
          )}
          <div className="mt-2 flex flex-wrap items-center justify-between gap-2 border-t border-[var(--border)]/60 pt-2">
            <SegmentControl
              ariaLabel="Agent Desk view"
              value={deskView}
              onChange={value => setDeskView(value as AgentDeskView)}
              options={[
                {
                  value: 'operate',
                  label:
                    decisionBriefsQuery.pendingCount > 0
                      ? `Operate (${decisionBriefsQuery.pendingCount})`
                      : 'Operate',
                },
                { value: 'observe', label: 'Observe' },
                { value: 'review', label: 'Review' },
              ]}
              size="sm"
            />
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {deskView === 'operate'
                ? 'Choose and start the next safe action.'
                : deskView === 'observe'
                  ? 'Track active work and inspect task details.'
                  : 'Review outcomes, drift, alignment, and daily signals.'}
            </span>
          </div>
        </section>

        {deskView === 'operate' && <AgentDeskNowSummary {...nowSummary} />}

        {/* ── Alerts (only when something is wrong) ── */}
        {deskView === 'operate' && runnerBlocked && (
          <OpsFeedback variant="error" title="Runner unreachable — agent tasks blocked">
            Start with <code className="font-mono-tabular">make start</code> or set{' '}
            <code className="font-mono-tabular">REMEDIATION_RUNNER_URL</code> in{' '}
            <code className="font-mono-tabular">.env</code>.
            {healthQuery.data?.error != null && healthQuery.data.error !== '' && (
              <span className="mt-1 block font-mono-tabular text-[var(--text-dense-caption)]">
                {healthQuery.data.error}
              </span>
            )}
          </OpsFeedback>
        )}
        {deskView === 'operate' && runnerWarnCursor && (
          <OpsFeedback variant="warning" title="CURSOR_API_KEY not set on runner">
            Agent runs will fail — add key to <code className="font-mono-tabular">.env</code>.
          </OpsFeedback>
        )}
        {deskView === 'operate' && !canOperate && (
          <OpsFeedback variant="warning" title="Authenticate as operator to use Agent Desk">
            Use the header auth button before starting agent tasks.
          </OpsFeedback>
        )}

        {/* ── Composer: the primary interaction ── */}
        {deskView === 'operate' && (
          <section className="agent-desk-composer-section">
            <div className="agent-desk-quick-row">
              {QUICK_PROMPTS.map(item => (
                <button
                  key={item.id}
                  type="button"
                  className={`agent-desk-quick-btn${item.scope === 'release' || item.scope === 'deliver-stg-recover' ? ' agent-desk-quick-btn--accent' : ''}`}
                  disabled={!canOperate || startMutation.isPending || runnerBlocked}
                  onClick={() => handleSend(item.prompt, item.scope)}
                >
                  {item.label}
                </button>
              ))}
            </div>
            <div className="agent-desk-composer">
              <textarea
                className="agent-desk-composer__input"
                rows={3}
                placeholder={selectedScope === 'release'
                  ? 'Describe what to release…'
                  : 'Ask the ops agent…'}
                value={composerText}
                disabled={!canOperate || startMutation.isPending || runnerBlocked}
                onChange={e => setComposerText(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault()
                    handleSend(composerText)
                  }
                }}
              />
              <div className="agent-desk-composer__footer">
                <div className="agent-desk-scope-row">
                  {(['agent-desk', 'release'] as const).map(s => (
                    <button
                      key={s}
                      type="button"
                      className={`agent-desk-scope-chip${selectedScope === s ? ' agent-desk-scope-chip--active' : ''}`}
                      onClick={() => setSelectedScope(s)}
                    >
                      {s === 'agent-desk' ? 'Ops' : 'Release'}
                    </button>
                  ))}
                </div>
                <Button
                  type="button"
                  size="sm"
                  disabled={!canOperate || startMutation.isPending || runnerBlocked || composerText.trim() === ''}
                  onClick={() => handleSend(composerText)}
                >
                  {startMutation.isPending ? 'Starting…' : 'Send'}
                </Button>
              </div>
            </div>
            {startMutation.isError && (
              <OpsFeedback variant="error" title="Task failed to start" className="mt-2">
                {(startMutation.error as Error).message}
              </OpsFeedback>
            )}
            {handoffLinkError != null && (
              <OpsFeedback variant="error" title="Task started but handoff link failed" className="mt-2">
                {handoffLinkError}
              </OpsFeedback>
            )}
            {activeUserPrompt != null && (
              <div className="agent-desk-user-bubble mt-2">
                <p className="agent-desk-user-bubble__label">Your request</p>
                <p className="agent-desk-user-bubble__body">{activeUserPrompt}</p>
              </div>
            )}
          </section>
        )}

        {deskView === 'operate' && (
          <AgentDeskSessionOpsPanels
            context={context}
            matrices={matrices}
            clusterSummary={clusterSummary}
            platformHealthy={platformHealthy}
            auditRecords={auditRecords}
            onOpenBriefing={onOpenBriefing}
            onOpenDeliveryBoard={onOpenDeliveryBoard}
            onOpenBriefingReconciliation={onOpenBriefingReconciliation}
            mode="operate"
            onOpenHandoffSource={handleOpenHandoffSource}
            onPrepareHandoffAgent={handlePrepareHandoffAgent}
            onStartHandoffAgent={handleStartHandoffAgent}
            onObserveHandoffJob={handleObserveHandoffJob}
            onNavigateRecurringSetup={onOpenAgentSystem}
            focusHandoffId={focusHandoffId}
            onFocusHandoffConsumed={onFocusHandoffConsumed}
          />
        )}

        {/* ── Recent tasks ── */}
        {deskView === 'observe' && (
          <>
            <AgentDeskNowSummary {...nowSummary} compact />
            <section className="agent-desk-tasks-section">
          <div className="flex items-center justify-between">
            <h3 className="agent-desk-section-title">Recent tasks</h3>
            <div className="flex items-center gap-3">
              <div className="agent-desk-timeline-legend">
                <span className="agent-desk-timeline-legend__item">
                  <i className="agent-desk-timeline-swatch agent-desk-timeline-swatch--done" /> ok
                </span>
                <span className="agent-desk-timeline-legend__item">
                  <i className="agent-desk-timeline-swatch agent-desk-timeline-swatch--failed" /> failed
                </span>
                <span className="agent-desk-timeline-legend__item">
                  <i className="agent-desk-timeline-swatch agent-desk-timeline-swatch--running" /> running
                </span>
                <span className="agent-desk-timeline-legend__item">
                  <i className="agent-desk-timeline-swatch agent-desk-timeline-swatch--cancelled" /> cancelled
                </span>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="text-[var(--text-dense-caption)]"
                onClick={() => void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })}
              >
                Refresh
              </Button>
            </div>
          </div>
          <div className="agent-desk-timeline">
            {jobGroups.length === 0 && (
              <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No tasks yet
              </span>
            )}
            {jobGroups.map(group => {
              const liveRunningJob = group.jobs.find(
                j => remediationTimelineCellStatus(j) === 'running',
              )
              return (
              <div key={group.scope} className="agent-desk-timeline-group">
                <div className="agent-desk-timeline-group__head">
                  <span className="agent-desk-timeline-group__label">
                    {group.label}
                    {group.scope === 'release-fix' && (
                      <span className="agent-desk-timeline-group__tier">escalation</span>
                    )}
                  </span>
                  <span className="agent-desk-timeline-group__counts">
                    {group.doneCount > 0 && (
                      <span className="agent-desk-timeline-count agent-desk-timeline-count--done">
                        {group.doneCount} ok
                      </span>
                    )}
                    {group.failedCount > 0 && (
                      <span className="agent-desk-timeline-count agent-desk-timeline-count--failed">
                        {group.failedCount} failed
                      </span>
                    )}
                    {group.runningCount > 0 && (
                      <span className="agent-desk-timeline-count agent-desk-timeline-count--running">
                        {group.runningCount} running
                      </span>
                    )}
                    {group.cancelledCount > 0 && (
                      <span className="agent-desk-timeline-count agent-desk-timeline-count--cancelled">
                        {group.cancelledCount} cancelled
                      </span>
                    )}
                    {canOperate && liveRunningJob != null && (
                      <button
                        type="button"
                        className="agent-desk-timeline-stop"
                        title={`Stop ${group.label} (${liveRunningJob.id.slice(0, 8)})`}
                        disabled={cancelMutation.isPending}
                        onClick={() =>
                          setStopConfirm({ jobId: liveRunningJob.id, label: group.label })
                        }
                      >
                        <Square size={9} /> Stop
                      </button>
                    )}
                  </span>
                </div>
                <div className="agent-desk-timeline-track">
                  <span className="agent-desk-timeline-track__now">now</span>
                  {group.jobs.map(job => (
                    <button
                      key={job.id}
                      type="button"
                      title={`${job.id.slice(0, 8)} · ${remediationJobStatusLabel(job)} · ${formatRemediationJobWhen(job.created_at)}`}
                      aria-label={`${group.label} ${remediationJobStatusLabel(job)} ${formatRemediationJobWhen(job.created_at)}`}
                      className={[
                        'agent-desk-timeline-cell',
                        `agent-desk-timeline-cell--${remediationTimelineCellStatus(job)}`,
                        job.phase === 'awaiting_approval' ? ' agent-desk-timeline-cell--attn' : '',
                        job.id === jobId ? ' agent-desk-timeline-cell--active' : '',
                      ].join(' ')}
                      onClick={() => {
                        setInitialJob(job)
                        setJobId(job.id)
                        setPanelOpen(true)
                        setDeskView('observe')
                      }}
                    />
                  ))}
                </div>
              </div>
              )
            })}
          </div>
            </section>
          </>
        )}

        {deskView === 'review' && (
          <>
            <AgentDeskSessionOpsPanels
              context={context}
              matrices={matrices}
              clusterSummary={clusterSummary}
              platformHealthy={platformHealthy}
              auditRecords={auditRecords}
              onOpenBriefing={onOpenBriefing}
              onOpenDeliveryBoard={onOpenDeliveryBoard}
              onOpenBriefingReconciliation={onOpenBriefingReconciliation}
              mode="review"
              onOpenHandoffSource={handleOpenHandoffSource}
              onObserveHandoffJob={handleObserveHandoffJob}
            />
            <BriefingFoldableSection
              kicker="Review"
              title="Flight Director · 24h digest"
              description="Job outcomes, approval events, and current trust-matrix skill signals."
              defaultExpanded={false}
            >
              <FlightDirectorBriefingPanel onOpenTrustAutonomy={onOpenTrustAutonomy} />
            </BriefingFoldableSection>
            <AgentTaskCatalogPanel
              onOpenAgentSystem={onOpenAgentSystem}
              onOpenDoctrine={tab => {
                if (tab === 'mcp-contract') onOpenMcpContract?.()
                else onOpenAgentProtocol?.()
              }}
            />
          </>
        )}

        {/* ── Infrastructure → moved to Operator Plane (L-1) ── */}
        <section className="agent-desk-infra-section">
          <button
            type="button"
            className="agent-desk-infra-toggle"
            onClick={() => onOpenOperatorPlane?.()}
            disabled={onOpenOperatorPlane == null}
          >
            <ChevronRight size={14} />
            <span>Operator Plane (L-1)</span>
            {bridge != null && (
              <span className="agent-desk-infra-summary">
                {[
                  runnerSummary(bridge),
                  bridge.git_bridge.status === 'ok'
                    ? `Git Bridge ok · ${bridge.git_bridge.repo_count ?? 0} repos`
                    : `Git Bridge ${bridge.git_bridge.status}`,
                  `${bridge.platform_mcp.implemented_count} MCP tools`,
                ].join(' · ')}
              </span>
            )}
          </button>
        </section>
      </div>

      <RemediationPanel
        variant="desk"
        open={panelOpen}
        jobId={jobId}
        initialJob={initialJob}
        initBriefFallback={activeUserPrompt}
        stopping={cancelMutation.isPending}
        onStop={id => cancelMutation.mutate(id)}
        onClose={() => setPanelOpen(false)}
        onCloseSession={
          briefingActiveSession != null
            ? () => {
                const job = trackedJob ?? initialJob
                if (job != null) setCloseSessionJob(job)
              }
            : undefined
        }
        onDismiss={() => {
          void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
        }}
        onComplete={job => {
          setInitialJob(job)
          setTrackedJob(job)
          void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
          void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
        }}
      />

      <CloseBriefingSessionDialog
        job={closeSessionJob}
        open={closeSessionJob != null}
        onDone={() => {
          setCloseSessionJob(null)
          setPanelOpen(false)
        }}
        onCancel={() => setCloseSessionJob(null)}
      />

      <ConfirmDialog
        open={stopConfirm != null}
        title="Stop running task"
        message={
          stopConfirm != null
            ? `Stop the running ${stopConfirm.label} task? The agent run will abort immediately. You can start a new task afterward.`
            : ''
        }
        confirmLabel="Stop task"
        confirming={cancelMutation.isPending}
        onConfirm={() => {
          if (stopConfirm != null) cancelMutation.mutate(stopConfirm.jobId)
        }}
        onCancel={() => setStopConfirm(null)}
      />
    </div>
  )
}

function AgentDeskNowSummary({
  activeTasks,
  pendingApprovals,
  openHandoffs,
  blockingFailures,
  compact = false,
}: {
  activeTasks: number
  pendingApprovals: number
  openHandoffs: number
  blockingFailures: number
  compact?: boolean
}) {
  const metrics = [
    {
      label: 'Active tasks',
      value: activeTasks,
      detail: 'running now',
      variant: activeTasks > 0 ? 'success' as const : 'neutral' as const,
    },
    {
      label: 'Pending approvals',
      value: pendingApprovals,
      detail: 'operator decisions',
      variant: pendingApprovals > 0 ? 'warning' as const : 'neutral' as const,
    },
    {
      label: 'Open handoffs',
      value: openHandoffs,
      detail: 'awaiting execution',
      variant: openHandoffs > 0 ? 'warning' as const : 'neutral' as const,
    },
    {
      label: 'Blocking failures',
      value: blockingFailures,
      detail: '24h failed-job proxy',
      variant: blockingFailures > 0 ? 'danger' as const : 'neutral' as const,
    },
  ]

  return (
    <section className={`agent-desk-now${compact ? ' agent-desk-now--compact' : ''}`}>
      <div className="flex items-baseline justify-between gap-2">
        <div>
          <p className="m-0 text-[var(--text-dense-caption)] font-semibold uppercase tracking-[0.08em] text-[var(--muted-foreground)]">
            {compact ? 'Active work' : 'Now'}
          </p>
          {!compact && (
            <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Current work requiring attention or execution.
            </p>
          )}
        </div>
      </div>
      <div className="agent-desk-now__grid">
        {metrics.map(metric => (
          <div key={metric.label} className="agent-desk-now__metric">
            <div className="flex min-w-0 items-center justify-between gap-2">
              <span className="truncate text-[var(--text-dense-label)] font-medium">
                {metric.label}
              </span>
              <DenseTag variant={metric.variant}>{metric.value}</DenseTag>
            </div>
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {metric.detail}
            </span>
          </div>
        ))}
      </div>
    </section>
  )
}
