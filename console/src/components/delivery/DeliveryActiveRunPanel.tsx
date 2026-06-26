import { Button, DenseTag } from '@bifrost/ui'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import type { DeliveryPipelineRunView } from '@/api/types'
import { fetchPipelineRunLogs, fetchPipelineRuns } from '@/api/platform'
import { DeliveryPipelineStepProgress } from '@/components/delivery/DeliveryPipelineStepProgress'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import { deliveryFocusRunQueryKey } from '@/lib/delivery/deliveryFocusRun'
import type { DeliveryTargetConfig } from '@/lib/delivery/deliveryTargets'
import {
  buildPipelineRunAskPack,
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
  platformDeliverAskContext,
} from '@/lib/delivery/pipelineRunAskPack'

function runLamp(run: { status: string; reason?: string }): 'ok' | 'fail' | 'degraded' | 'unknown' {
  if (isPipelineRunSucceeded(run as Parameters<typeof isPipelineRunSucceeded>[0])) return 'ok'
  if (isPipelineRunRunning(run as Parameters<typeof isPipelineRunRunning>[0])) return 'degraded'
  if (isPipelineRunFailed(run as Parameters<typeof isPipelineRunFailed>[0])) return 'fail'
  return 'unknown'
}

function logsNeedPoll(logs: string | undefined): boolean {
  if (logs == null || logs === '') return true
  return logs.includes('no pods yet') || logs.includes('no log lines yet')
}

interface DeliveryActiveRunPanelProps {
  target: DeliveryTargetConfig
}

export function DeliveryActiveRunPanel({ target }: DeliveryActiveRunPanelProps) {
  const qc = useQueryClient()
  const focusKey = deliveryFocusRunQueryKey(target.pipeline)
  const pipeline = target.pipeline

  const { data: pinnedName = null } = useQuery<string | null>({
    queryKey: focusKey,
    queryFn: () => null,
    initialData: null,
    staleTime: Infinity,
  })

  const runsQuery = useQuery({
    queryKey: ['delivery', 'runs', pipeline],
    queryFn: () => fetchPipelineRuns(pipeline),
    staleTime: 0,
    refetchInterval: () => {
      const pin = qc.getQueryData<string | null>(focusKey)
      const runs =
        qc.getQueryData<{ runs: DeliveryPipelineRunView[] }>(['delivery', 'runs', pipeline])?.runs ?? []
      if (pin != null) {
        const pinned = runs.find(r => r.name === pin)
        if (pinned == null || isPipelineRunRunning(pinned)) return 3_000
        if (isPipelineRunSucceeded(pinned) || isPipelineRunFailed(pinned)) return 15_000
      }
      if (runs.some(r => isPipelineRunRunning(r))) return 3_000
      return 15_000
    },
  })

  const runs = runsQuery.data?.runs ?? []
  const ns = runsQuery.data?.namespace ?? 'cicd'

  const focusRun = useMemo((): DeliveryPipelineRunView | undefined => {
    if (pinnedName) {
      const pinned = runs.find(r => r.name === pinnedName)
      if (pinned != null) return pinned
      return {
        name: pinnedName,
        namespace: ns,
        pipeline,
        status: 'Unknown',
        reason: 'Running',
      }
    }
    return runs.find(r => isPipelineRunRunning(r)) ?? runs[0]
  }, [runs, pinnedName, ns, pipeline])

  useEffect(() => {
    if (focusRun?.name == null) return
    void qc.invalidateQueries({ queryKey: ['delivery', 'steps', focusRun.name] })
  }, [focusRun?.name, qc])

  useEffect(() => {
    if (focusRun == null) return
    if (isPipelineRunSucceeded(focusRun) || isPipelineRunFailed(focusRun)) {
      qc.setQueryData(focusKey, null)
    }
  }, [focusRun, focusKey, qc])

  const running = focusRun != null && isPipelineRunRunning(focusRun)
  const terminal = focusRun != null && (isPipelineRunSucceeded(focusRun) || isPipelineRunFailed(focusRun))
  const pollSteps = pinnedName != null || running || (focusRun != null && !terminal)

  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'error'>('idle')

  const isPlatformTarget = target.id === 'platform-stg' || target.id === 'platform-prod'

  const buildAskPack = (logsText: string): string =>
    buildPipelineRunAskPack({
      pipeline,
      run: focusRun!,
      logs: logsText,
      context: isPlatformTarget
        ? platformDeliverAskContext({ shortLabel: target.shortLabel, namespace: target.namespace })
        : undefined,
    })

  const logsQuery = useQuery({
    queryKey: ['delivery', 'logs', pipeline, focusRun?.name, 'active'],
    queryFn: () => fetchPipelineRunLogs(focusRun!.name, runsQuery.data?.namespace ?? focusRun!.namespace),
    enabled: focusRun != null,
    refetchInterval: query => {
      if (focusRun == null) return false
      if (isPipelineRunRunning(focusRun)) return 5_000
      if (logsNeedPoll(query.state.data?.logs)) return 5_000
      return false
    },
  })

  const handleAskAi = async () => {
    if (focusRun == null) return
    // Pull the freshest logs so the bundle is complete even mid-failure.
    let logsText = logsQuery.data?.logs ?? ''
    try {
      const res = await fetchPipelineRunLogs(focusRun.name, ns)
      logsText = res.logs
    } catch {
      /* fall back to cached logs */
    }
    try {
      await navigator.clipboard.writeText(buildAskPack(logsText))
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2500)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 2500)
    }
  }

  const handleDownload = () => {
    if (focusRun == null) return
    const blob = new Blob([buildAskPack(logsQuery.data?.logs ?? '')], { type: 'text/markdown' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `deliver-debug-${focusRun.name}.md`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <OpsSection
      title={running ? `Active run — ${target.shortLabel}` : `Latest run — ${target.shortLabel}`}
      description={
        running
          ? `Live Tekton steps for ${pipeline} (auto-refresh).`
          : `Most recent ${pipeline} run — start from actuate panel above.`
      }
      leading={focusRun != null ? <StatusLamp value={runLamp(focusRun)} kind="reach" /> : undefined}
      actions={
        focusRun != null ? (
          <DenseTag variant={running ? 'warning' : isPipelineRunSucceeded(focusRun) ? 'success' : 'danger'}>
            {formatPipelineRunStatus(focusRun)}
          </DenseTag>
        ) : undefined
      }
      headerExtra={
        runsQuery.error instanceof Error ? (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
            {runsQuery.error.message}
          </p>
        ) : running ? (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Pipeline running — status and logs update automatically.
          </p>
        ) : null
      }
      bodyPadding="default"
      overflow="visible"
    >
      {runsQuery.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading pipeline runs…</p>
      ) : focusRun == null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No runs yet for {pipeline}.
        </p>
      ) : (
        <>
          <p className="m-0 font-mono-tabular text-[var(--text-dense-label)]">{focusRun.name}</p>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {focusRun.start_time != null && focusRun.start_time !== ''
              ? `Started ${new Date(focusRun.start_time).toLocaleString()}`
              : 'Start time pending'}
            {focusRun.completion_time != null && focusRun.completion_time !== ''
              ? ` · Completed ${new Date(focusRun.completion_time).toLocaleString()}`
              : running
                ? ' · Running'
                : ''}
          </p>
          <DeliveryPipelineStepProgress
            runName={focusRun.name}
            namespace={ns}
            pollUntilTerminal={pollSteps}
            runTerminal={
              terminal
                ? isPipelineRunSucceeded(focusRun)
                  ? 'succeeded'
                  : 'failed'
                : undefined
            }
          />
          <pre className="llm-content-pre m-0 mt-3 max-h-80 overflow-auto font-mono-tabular text-[var(--text-dense-meta)]">
            {logsQuery.isLoading && logsQuery.data == null
              ? 'Loading logs…'
              : logsQuery.data?.logs ?? '(empty)'}
          </pre>
          {terminal && isPipelineRunSucceeded(focusRun) && (
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Deliver succeeded — verify smoke on Observe.
              </span>
              <Button variant="outline" size="sm" asChild>
                <a href={target.successLink.href} target="_blank" rel="noreferrer">
                  {target.successLink.label}
                </a>
              </Button>
            </div>
          )}
          {terminal && isPipelineRunFailed(focusRun) && (
            <div className="mt-3 flex flex-col gap-2">
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
                Deliver failed{focusRun.reason != null && focusRun.reason !== '' ? `: ${focusRun.reason}` : ''} — export the full context below for AI triage.
              </p>
              <div className="flex flex-wrap items-center gap-2">
                <Button size="sm" onClick={() => void handleAskAi()}>
                  {copyState === 'copied'
                    ? 'Copied — paste into AI'
                    : copyState === 'error'
                      ? 'Copy failed'
                      : 'Ask AI for Help'}
                </Button>
                <Button size="sm" variant="outline" onClick={handleDownload}>
                  Download log
                </Button>
              </div>
              {copyState === 'copied' && (
                <p className="m-0 text-[var(--text-dense-meta)] text-[var(--success)]">
                  Debug bundle (run status, reason, pipeline tasks, log tail) copied — paste it into your AI assistant.
                </p>
              )}
            </div>
          )}
        </>
      )}
    </OpsSection>
  )
}
