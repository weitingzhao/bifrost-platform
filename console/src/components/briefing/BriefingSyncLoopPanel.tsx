import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag } from '@bifrost/ui'
import { Check, Copy, Sparkles, BookOpen } from 'lucide-react'
import type { OpsContextResponse } from '@/api/types'
import {
  fetchAgentBridge,
  fetchAgentNightlyReport,
  fetchDriftProposals,
  fetchRemediationJobs,
  triggerNightlyDriftScan,
} from '@/api/platform'
import { StatusLamp } from '@/components/StatusLamp'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { BRIEFING_SYNC_LOOP_STEPS } from '@/lib/architecture/briefingReconciliationCatalog'
import {
  buildBriefingSyncLoopSteps,
  syncLoopStatusLabel,
  syncLoopStatusTagVariant,
} from '@/lib/briefing/briefingSyncLoop'
import { buildSyncLoopFixPack } from '@/lib/briefing/briefingFixPack'
import type { ReconcileFinding } from '@/lib/briefing/reconcileBriefing'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

type OpenAgentDeskArg = string | { prefill: string }

export function BriefingSyncLoopPanel({
  reconcileFindings,
  onOpenAgentDesk,
}: {
  context?: OpsContextResponse
  reconcileFindings: ReconcileFinding[]
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
}) {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [scanMsg, setScanMsg] = useState<string | null>(null)
  const [copied, setCopied] = useState<'fix' | 'learn' | null>(null)

  const bridgeQuery = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 60_000,
  })

  const proposalsQuery = useQuery({
    queryKey: ['agent', 'drift-proposals'],
    queryFn: fetchDriftProposals,
    refetchInterval: 60_000,
  })

  const nightlyQuery = useQuery({
    queryKey: ['agent', 'nightly-report'],
    queryFn: fetchAgentNightlyReport,
    staleTime: 60_000,
  })

  const proposals = proposalsQuery.data?.proposals ?? []
  const hasActiveFix = proposals.some(p =>
    ['pending_approval', 'approved', 'running'].includes(p.status),
  )

  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    staleTime: 30_000,
    refetchInterval: hasActiveFix ? 5_000 : 30_000,
  })

  const scanMutation = useMutation({
    mutationFn: triggerNightlyDriftScan,
    onSuccess: data => {
      setScanMsg(data.hint ?? 'Drift scan started on agent host.')
      void qc.invalidateQueries({ queryKey: ['agent', 'nightly-report'] })
      void qc.invalidateQueries({ queryKey: ['agent', 'drift-proposals'] })
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      void qc.invalidateQueries({ queryKey: ['agent', 'bridge'] })
      window.setTimeout(() => setScanMsg(null), 12_000)
    },
    onError: (err: Error) => setScanMsg(err.message),
  })

  const runnerOk = bridgeQuery.data?.remediation_runner.status === 'ok'

  const steps = useMemo(
    () =>
      buildBriefingSyncLoopSteps({
        reconcileFindings,
        nightlyReport: nightlyQuery.data,
        proposals,
        remediationJobs: jobsQuery.data?.jobs ?? [],
      }),
    [reconcileFindings, nightlyQuery.data, proposals, jobsQuery.data?.jobs],
  )

  const loopHealthy = steps.every(s => s.status === 'ok' || s.status === 'idle')

  const fixPack = useMemo(
    () =>
      buildSyncLoopFixPack({
        steps,
        reconcileFindings,
        nightlyReport: nightlyQuery.data,
      }),
    [steps, reconcileFindings, nightlyQuery.data],
  )

  function copyText(kind: 'fix' | 'learn', text: string) {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(kind)
      window.setTimeout(() => setCopied(c => (c === kind ? null : c)), 2000)
    })
  }

  function handleStepAction(stepId: string, jobId?: string) {
    if (stepId === 'owner-approval') {
      document.getElementById('drift-proposals')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      return
    }
    if (stepId === 'nightly-scan' && canOperate && runnerOk) {
      scanMutation.mutate()
      return
    }
    if (stepId === 'drift-fix' && onOpenAgentDesk != null) {
      onOpenAgentDesk(jobId)
    }
  }

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <p className="briefing-section-kicker m-0">Automation · Briefing sync loop</p>
          <h2 className="m-0 mt-1 text-sm font-semibold">Detect → propose → approve → fix</h2>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Live status of the briefing reconciliation pipeline. Runtime SYNC (Console) uses the same
            rules as nightly Layer 3 — fixes require Owner approval (no unattended spine writes).
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 shrink-0">
          <DenseTag variant={loopHealthy ? 'success' : 'warning'}>
            {loopHealthy ? 'LOOP CLEAR' : 'ATTENTION'}
          </DenseTag>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!canOperate || scanMutation.isPending || !runnerOk}
            title={!runnerOk ? 'Remediation runner must be reachable' : undefined}
            onClick={() => scanMutation.mutate()}
          >
            {scanMutation.isPending ? 'Starting scan…' : 'Run drift scan now'}
          </Button>
        </div>
      </div>

      {scanMsg != null && (
        <OpsFeedback
          variant={scanMutation.isError ? 'error' : 'success'}
          title={scanMutation.isError ? 'Drift scan failed' : 'Drift scan started'}
          className="mt-3"
        >
          {scanMsg}
        </OpsFeedback>
      )}

      {fixPack.hasIssues && (
        <div className="mt-3 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-3">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <p className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--foreground)]">
                Fix this drift
              </p>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {fixPack.summary}
              </p>
            </div>
          </div>
          <div className="mt-3 flex flex-wrap gap-2">
            <Button
              type="button"
              variant="default"
              size="sm"
              onClick={() => copyText('fix', fixPack.fixPrompt)}
            >
              {copied === 'fix' ? (
                <Check className="size-3.5 shrink-0" aria-hidden />
              ) : (
                <Copy className="size-3.5 shrink-0" aria-hidden />
              )}
              {copied === 'fix' ? 'Copied fix prompt' : 'Copy for Cursor Agent'}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!canOperate || onOpenAgentDesk == null}
              title={!canOperate ? 'Operator token required' : undefined}
              onClick={() => onOpenAgentDesk?.({ prefill: fixPack.fixPrompt })}
            >
              <Sparkles className="size-3.5 shrink-0" aria-hidden />
              Fix with built-in Agent
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => copyText('learn', fixPack.learnNote)}
            >
              {copied === 'learn' ? (
                <Check className="size-3.5 shrink-0" aria-hidden />
              ) : (
                <BookOpen className="size-3.5 shrink-0" aria-hidden />
              )}
              {copied === 'learn' ? 'Copied doctrine note' : 'Copy doctrine to learn'}
            </Button>
          </div>
          <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            <strong className="font-medium text-[var(--foreground)]">Copy for Cursor Agent</strong> — paste
            into a new Cursor chat.{' '}
            <strong className="font-medium text-[var(--foreground)]">Fix with built-in Agent</strong> — opens
            Agent Desk with the same prompt prefilled (Send to start).{' '}
            <strong className="font-medium text-[var(--foreground)]">Copy doctrine</strong> — prevention
            note for a <code className="font-mono-tabular">.cursor/rules</code> entry.
          </p>
        </div>
      )}

      <div className="mt-4 flex flex-col gap-3 lg:flex-row lg:items-stretch">
        {steps.map((step, index) => {
          const spec = BRIEFING_SYNC_LOOP_STEPS.find(s => s.id === step.id)
          const lampReach =
            step.status === 'ok'
              ? 'ok'
              : step.status === 'fail'
                ? 'fail'
                : step.status === 'warning'
                  ? 'degraded'
                  : step.status === 'active'
                    ? 'degraded'
                    : 'unknown'

          const showRunScan = step.id === 'nightly-scan' && canOperate && runnerOk

          return (
            <div key={step.id} className="flex min-w-0 flex-1 flex-col lg:flex-row lg:items-stretch">
              {index > 0 && (
                <div
                  className="hidden shrink-0 items-center justify-center px-1 text-[var(--muted-foreground)] lg:flex"
                  aria-hidden
                >
                  →
                </div>
              )}
              <div className="flex min-w-0 flex-1 flex-col rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
                <div className="flex items-center gap-2">
                  <StatusLamp value={lampReach} kind="reach" />
                  <span className="text-xs font-semibold uppercase tracking-wide">{step.label}</span>
                  <DenseTag variant={syncLoopStatusTagVariant(step.status)} className="ml-auto shrink-0">
                    {syncLoopStatusLabel(step.status)}
                  </DenseTag>
                </div>
                {step.agentTaskLabel != null && (
                  <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Agent: {step.agentTaskLabel}
                  </p>
                )}
                {spec?.scanner != null && (
                  <p className="m-0 mt-0.5 font-mono-tabular text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {spec.scanner}
                  </p>
                )}
                <p className="m-0 mt-1.5 text-[var(--text-dense-meta)]">{step.detail}</p>
                {(step.action != null || showRunScan) && (
                  <div className="mt-1 flex flex-wrap gap-2">
                    {showRunScan && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-auto self-start px-0 py-0 text-[var(--text-dense-meta)]"
                        disabled={scanMutation.isPending}
                        onClick={() => handleStepAction('nightly-scan')}
                      >
                        {scanMutation.isPending ? 'Starting…' : 'Run L1–L3 now'}
                      </Button>
                    )}
                    {step.action != null && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-auto self-start px-0 py-0 text-[var(--text-dense-meta)]"
                        onClick={() => handleStepAction(step.id, step.actionJobId)}
                      >
                        {step.action === 'scroll-proposals' ? 'Go to proposals ↓' : 'Open Agent Desk'}
                      </Button>
                    )}
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}
