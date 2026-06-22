import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, StatusLamp } from '@bifrost/ui'
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { OpsContextResponse, RemediationJob } from '@/api/types'
import {
  cancelRemediationJob,
  fetchAgentNightlyReport,
  fetchRemediationHealth,
  fetchRemediationJobs,
  startRemediation,
} from '@/api/platform'
import { RemediationPanel } from '@/components/cluster/RemediationPanel'
import { AgentMcpPanel } from '@/components/agent/AgentMcpPanel'
import { AgentHostDeployPanel } from '@/components/agent/AgentHostDeployPanel'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

interface AgentDeskPageProps {
  context: OpsContextResponse | undefined
  initialJobId?: string | null
  onInitialJobConsumed?: () => void
  onOpenBriefing?: () => void
  onOpenCluster?: () => void
  onOpenMcpContract?: () => void
}

interface QuickPrompt {
  id: string
  label: string
  prompt: string
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
]

function runnerReachability(status: string | undefined): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (status === 'ok') return 'ok'
  if (status === 'unavailable') return 'fail'
  return 'unknown'
}

export function AgentDeskPage({
  context,
  initialJobId,
  onInitialJobConsumed,
  onOpenBriefing,
  onOpenCluster,
  onOpenMcpContract,
}: AgentDeskPageProps) {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [composerText, setComposerText] = useState('')
  const [jobId, setJobId] = useState<string | null>(null)
  const [initialJob, setInitialJob] = useState<RemediationJob | null>(null)
  const [panelOpen, setPanelOpen] = useState(false)
  const [userPrompts, setUserPrompts] = useState<Record<string, string>>({})

  useEffect(() => {
    if (initialJobId == null) return
    setJobId(initialJobId)
    setPanelOpen(true)
    onInitialJobConsumed?.()
  }, [initialJobId, onInitialJobConsumed])

  const healthQuery = useQuery({
    queryKey: ['remediation', 'health'],
    queryFn: fetchRemediationHealth,
    refetchInterval: 60_000,
  })

  const nightlyQuery = useQuery({
    queryKey: ['agent', 'nightly-report'],
    queryFn: fetchAgentNightlyReport,
    staleTime: 120_000,
  })

  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: panelOpen ? 15_000 : 60_000,
  })

  const recentJobs = useMemo(() => {
    const jobs = jobsQuery.data?.jobs ?? []
    return [...jobs].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at)).slice(0, 12)
  }, [jobsQuery.data?.jobs])

  const startMutation = useMutation({
    mutationFn: async (prompt: string) => {
      const spineNote =
        context?.focus?.headline != null ? `Spine focus: ${context.focus.headline}\n\n` : ''
      return startRemediation({
        scope: 'agent-desk',
        prompt: `${spineNote}${prompt.trim()}`,
      })
    },
    onSuccess: (job, prompt) => {
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
    onSuccess: job => setInitialJob(job),
  })

  const runnerStatus = healthQuery.data?.status
  const runnerHealthy = runnerStatus === 'ok'
  const runnerHasCursorKey = healthQuery.data?.cursor_api_key === true
  const runnerBlocked = !runnerHealthy
  const runnerWarnCursor = runnerHealthy && !runnerHasCursorKey

  const handleSend = useCallback(
    (text: string) => {
      const trimmed = text.trim()
      if (trimmed === '' || !canOperate || runnerBlocked) return
      startMutation.mutate(trimmed)
    },
    [canOperate, runnerBlocked, startMutation],
  )

  const activeUserPrompt = jobId != null ? userPrompts[jobId] : undefined

  const runnerStatusClass =
    runnerStatus === 'ok'
      ? 'text-semantic-success'
      : runnerStatus === 'unavailable'
        ? 'text-semantic-error'
        : 'text-semantic-warning'

  return (
    <div
      className={`agent-desk-shell${panelOpen ? ' agent-desk-shell--panel-open' : ''}`}
    >
      <div className="agent-desk-main flex min-w-0 flex-col gap-3">
        <section className="panel-elevated px-4 py-3">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="briefing-section-kicker m-0">Owner control plane</p>
              <h2 className="m-0 mt-1 text-sm font-semibold">Agent Desk</h2>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Chat-style requests routed through platform-api → remediation runner on the agent
                host. Task stream, approvals, and history share the same remediation pipeline as
                Cluster auto-remediate.
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <div className="flex items-center gap-2 text-[var(--text-dense-meta)]">
                <StatusLamp value={runnerReachability(runnerStatus)} kind="reach" />
                <span className={runnerStatusClass}>
                  Runner {runnerStatus ?? (healthQuery.isLoading ? '…' : 'unknown')}
                </span>
              </div>
              {onOpenBriefing != null && (
                <Button type="button" variant="outline" size="sm" onClick={onOpenBriefing}>
                  Briefing packs
                </Button>
              )}
              {onOpenCluster != null && (
                <Button type="button" variant="outline" size="sm" onClick={onOpenCluster}>
                  Cluster
                </Button>
              )}
            </div>
          </div>
          {context?.focus?.headline != null && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              <span className="text-[var(--foreground)]">Now:</span> {context.focus.headline}
            </p>
          )}
          {nightlyQuery.data?.available && nightlyQuery.data.generated_at != null && (
            <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Nightly report {new Date(nightlyQuery.data.generated_at).toLocaleString()}
            </p>
          )}
        </section>

        <AgentHostDeployPanel />

        <AgentMcpPanel onOpenMcpContract={onOpenMcpContract} onOpenBriefing={onOpenBriefing} />

        {runnerBlocked && (
          <OpsFeedback variant="error" title="Remediation runner unavailable">
            Agent tasks cannot start until platform-api can reach the remediation runner.
            <ul className="m-0 mt-2 list-disc pl-4">
              <li>
                Local dev: run <code className="font-mono-tabular">make start</code> (auto-starts
                runner) or <code className="font-mono-tabular">make dev-agent</code> in another
                terminal.
              </li>
              <li>
                Mac Mini agent: set <code className="font-mono-tabular">REMEDIATION_RUNNER_URL</code>{' '}
                in <code className="font-mono-tabular">.env</code> (e.g.{' '}
                <code className="font-mono-tabular">http://192.168.10.50:8781</code>) and restart
                platform-api.
              </li>
            </ul>
            {healthQuery.data?.error != null && healthQuery.data.error !== '' && (
              <p className="m-0 mt-2 font-mono-tabular text-[var(--text-dense-caption)]">
                {healthQuery.data.error}
              </p>
            )}
          </OpsFeedback>
        )}

        {runnerWarnCursor && (
          <OpsFeedback variant="warning" title="CURSOR_API_KEY not configured on runner">
            The runner is up but agent runs will fail until{' '}
            <code className="font-mono-tabular">CURSOR_API_KEY</code> is set in bifrost-platform{' '}
            <code className="font-mono-tabular">.env</code> and the runner is restarted.
          </OpsFeedback>
        )}

        <section className="panel-elevated flex min-h-0 flex-1 flex-col px-4 py-3">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[var(--text-dense-label)] font-medium text-[var(--muted-foreground)]">
              Quick prompts
            </span>
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              AI brief only — deterministic scan: MCP Bridge → Run drift scan now · report: Agent Briefing
            </span>
            {QUICK_PROMPTS.map(item => (
              <Button
                key={item.id}
                type="button"
                variant="outline"
                size="sm"
                disabled={!canOperate || startMutation.isPending || runnerBlocked}
                onClick={() => handleSend(item.prompt)}
              >
                {item.label}
              </Button>
            ))}
          </div>

          {activeUserPrompt != null && (
            <div className="agent-desk-user-bubble mt-3">
              <p className="agent-desk-user-bubble__label">Your request</p>
              <p className="agent-desk-user-bubble__body">{activeUserPrompt}</p>
            </div>
          )}

          {!canOperate && (
            <OpsFeedback variant="warning" title="Operator authentication required" className="mt-3">
              Authenticate as operator in the header before starting agent tasks.
            </OpsFeedback>
          )}
          {startMutation.isError && (
            <OpsFeedback variant="error" title="Task failed to start" className="mt-3">
              {(startMutation.error as Error).message}
            </OpsFeedback>
          )}

          <div className="agent-desk-composer mt-3">
            <textarea
              className="agent-desk-composer__input"
              rows={3}
              placeholder="Ask the ops agent…"
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
            <div className="agent-desk-composer__actions">
              <Button
                type="button"
                size="sm"
                disabled={
                  !canOperate ||
                  startMutation.isPending ||
                  runnerBlocked ||
                  composerText.trim() === ''
                }
                onClick={() => handleSend(composerText)}
              >
                {startMutation.isPending ? 'Starting…' : 'Send'}
              </Button>
            </div>
          </div>
        </section>

        <section className="panel-elevated px-4 py-3">
          <div className="flex items-center justify-between gap-2">
            <h3 className="m-0 text-sm font-semibold">Recent tasks</h3>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })}
            >
              Refresh
            </Button>
          </div>
          <div className="agent-desk-history mt-2 dense-scroll-x">
            {recentJobs.length === 0 && (
              <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No agent tasks yet.
              </span>
            )}
            {recentJobs.map(job => (
              <button
                key={job.id}
                type="button"
                className={`agent-desk-history-chip${job.id === jobId ? ' agent-desk-history-chip--active' : ''}`}
                onClick={() => {
                  setJobId(job.id)
                  setPanelOpen(true)
                }}
              >
                <span className="font-mono-tabular">{job.id.slice(0, 8)}</span>
                <span className={`agent-desk-history-chip__status agent-desk-history-chip__status--${job.status}`}>
                  {job.status}
                </span>
                {job.scope != null && job.scope !== '' && (
                  <span className="agent-desk-history-chip__scope">{job.scope}</span>
                )}
              </button>
            ))}
          </div>
          {recentJobs.length > 0 && !panelOpen && (
            <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Closed the task drawer? Click a chip above to reopen the report — use Copy report in the
              drawer before closing if you need the text elsewhere.
            </p>
          )}
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
        onComplete={job => {
          setInitialJob(job)
          void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
          void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
        }}
      />
    </div>
  )
}
