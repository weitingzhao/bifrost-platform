import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronRight, Square } from 'lucide-react'
import type { AgentBridgeResponse, OpsContextResponse, RemediationJob } from '@/api/types'
import {
  cancelRemediationJob,
  fetchAgentBridge,
  fetchRemediationHealth,
  fetchRemediationJobs,
  startRemediation,
} from '@/api/platform'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { RemediationPanel } from '@/components/cluster/RemediationPanel'
import { AgentTaskCatalogPanel } from '@/components/agent/AgentTaskCatalogPanel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  formatRemediationJobWhen,
  groupRemediationJobsByScope,
  remediationJobStatusLabel,
  remediationTimelineCellStatus,
} from '@/lib/remediation/remediationJobDisplay'

interface AgentDeskPageProps {
  context: OpsContextResponse | undefined
  initialJobId?: string | null
  prefillPrompt?: string | null
  onInitialJobConsumed?: () => void
  onPrefillConsumed?: () => void
  onOpenBriefing?: () => void
  onOpenCluster?: () => void
  onOpenMcpContract?: () => void
  onOpenAgentProtocol?: () => void
  onOpenAgentSystem?: () => void
  onOpenOperatorPlane?: () => void
}

type AgentScope = 'agent-desk' | 'release'

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
      'Deploy latest changes to prod. Scan all repos for uncommitted changes, commit and push, then run the full STG → Prod pipeline.',
    scope: 'release',
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
  initialJobId,
  prefillPrompt,
  onInitialJobConsumed,
  onPrefillConsumed,
  onOpenBriefing,
  onOpenCluster,
  onOpenMcpContract,
  onOpenAgentProtocol,
  onOpenAgentSystem,
  onOpenOperatorPlane,
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

  useEffect(() => {
    if (initialJobId == null) return
    setJobId(initialJobId)
    setPanelOpen(true)
    onInitialJobConsumed?.()
  }, [initialJobId, onInitialJobConsumed])

  useEffect(() => {
    if (prefillPrompt == null || prefillPrompt === '') return
    setComposerText(prefillPrompt)
    onPrefillConsumed?.()
  }, [prefillPrompt, onPrefillConsumed])

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
    mutationFn: async ({ prompt, scope }: { prompt: string; scope: AgentScope }) => {
      const spineNote =
        context?.focus?.headline != null ? `Spine focus: ${context.focus.headline}\n\n` : ''
      return startRemediation({
        scope,
        prompt: `${spineNote}${prompt.trim()}`,
      })
    },
    onSuccess: (job, { prompt }) => {
      setUserPrompts(prev => ({ ...prev, [job.id]: prompt }))
      setInitialJob(job)
      setJobId(job.id)
      setPanelOpen(true)
      setComposerText('')
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
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

  const handleSend = useCallback(
    (text: string, scopeOverride?: AgentScope) => {
      const trimmed = text.trim()
      if (trimmed === '' || !canOperate || runnerBlocked) return
      startMutation.mutate({ prompt: trimmed, scope: scopeOverride ?? selectedScope })
    },
    [canOperate, runnerBlocked, startMutation, selectedScope],
  )

  const activeUserPrompt = jobId != null ? userPrompts[jobId] : undefined

  return (
    <div className={`agent-desk-shell${panelOpen ? ' agent-desk-shell--panel-open' : ''}`}>
      <div className="agent-desk-main flex min-w-0 flex-col gap-3">

        {/* ── Hero: Title + Status bar ── */}
        <section className="agent-desk-hero">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <h2 className="m-0 text-base font-semibold">Agent Desk</h2>
              <div className="agent-desk-status-bar">
                <StatusLamp value={runnerReachability(runnerStatus)} kind="reach" />
                <DenseTag variant={statusVariant(runnerStatus)}>
                  Runner
                </DenseTag>
                <DenseTag variant={statusVariant(gitBridgeStatus)}>
                  Git Bridge
                  {gitBridgeStatus === 'ok' && bridge?.git_bridge?.dirty_repos != null && bridge.git_bridge.dirty_repos > 0
                    ? ` · ${bridge.git_bridge.dirty_repos}`
                    : ''}
                </DenseTag>
              </div>
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
          </div>
          {context?.focus?.headline != null && (
            <p className="agent-desk-spine-hint">
              {context.focus.headline}
            </p>
          )}
        </section>

        {/* ── Alerts (only when something is wrong) ── */}
        {runnerBlocked && (
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
        {runnerWarnCursor && (
          <OpsFeedback variant="warning" title="CURSOR_API_KEY not set on runner">
            Agent runs will fail — add key to <code className="font-mono-tabular">.env</code>.
          </OpsFeedback>
        )}
        {!canOperate && (
          <OpsFeedback variant="warning" title="Authenticate as operator to use Agent Desk">
            Use the header auth button before starting agent tasks.
          </OpsFeedback>
        )}

        {/* ── Composer: the primary interaction ── */}
        <section className="agent-desk-composer-section">
          <div className="agent-desk-quick-row">
            {QUICK_PROMPTS.map(item => (
              <button
                key={item.id}
                type="button"
                className={`agent-desk-quick-btn${item.scope === 'release' ? ' agent-desk-quick-btn--accent' : ''}`}
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
          {activeUserPrompt != null && (
            <div className="agent-desk-user-bubble mt-2">
              <p className="agent-desk-user-bubble__label">Your request</p>
              <p className="agent-desk-user-bubble__body">{activeUserPrompt}</p>
            </div>
          )}
        </section>

        <AgentTaskCatalogPanel
          onOpenAgentSystem={onOpenAgentSystem}
          onOpenDoctrine={tab => {
            if (tab === 'mcp-contract') onOpenMcpContract?.()
            else onOpenAgentProtocol?.()
          }}
        />

        {/* ── Recent tasks ── */}
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
                      }}
                    />
                  ))}
                </div>
              </div>
              )
            })}
          </div>
        </section>

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
        onDismiss={() => {
          void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
        }}
        onComplete={job => {
          setInitialJob(job)
          void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
          void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
        }}
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
