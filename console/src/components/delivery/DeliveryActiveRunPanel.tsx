import { Button, DenseTag } from '@bifrost/ui'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import type { DeliveryPipelineRunView } from '@/api/types'
import { fetchPipelineRunLogs, fetchPipelineRuns } from '@/api/platform'
import { DeliveryPipelineStepProgress } from '@/components/delivery/DeliveryPipelineStepProgress'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliveryPageTabs'
import { DELIVERY_FOCUS_RUN_QUERY_KEY } from '@/lib/delivery/deliveryFocusRun'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
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

export function DeliveryActiveRunPanel() {
  const qc = useQueryClient()
  const { data: pinnedName = null } = useQuery<string | null>({
    queryKey: DELIVERY_FOCUS_RUN_QUERY_KEY,
    queryFn: () => null,
    initialData: null,
    staleTime: Infinity,
  })

  const runsQuery = useQuery({
    queryKey: ['delivery', 'runs', DELIVER_STG_PIPELINE],
    queryFn: () => fetchPipelineRuns(DELIVER_STG_PIPELINE),
    staleTime: 0,
    refetchInterval: () => {
      const pin = qc.getQueryData<string | null>(DELIVERY_FOCUS_RUN_QUERY_KEY)
      const runs = qc.getQueryData<{ runs: DeliveryPipelineRunView[] }>(['delivery', 'runs', DELIVER_STG_PIPELINE])
        ?.runs ?? []
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
      // Run just started — API list may lag behind pin.
      return {
        name: pinnedName,
        namespace: ns,
        pipeline: DELIVER_STG_PIPELINE,
        status: 'Unknown',
        reason: 'Running',
      }
    }
    return runs.find(r => isPipelineRunRunning(r)) ?? runs[0]
  }, [runs, pinnedName, ns])

  useEffect(() => {
    if (focusRun?.name == null) return
    void qc.invalidateQueries({ queryKey: ['delivery', 'steps', focusRun.name] })
  }, [focusRun?.name, qc])

  useEffect(() => {
    if (focusRun == null) return
    if (isPipelineRunSucceeded(focusRun) || isPipelineRunFailed(focusRun)) {
      qc.setQueryData(DELIVERY_FOCUS_RUN_QUERY_KEY, null)
    }
  }, [focusRun, qc])

  const running = focusRun != null && isPipelineRunRunning(focusRun)
  const terminal = focusRun != null && (isPipelineRunSucceeded(focusRun) || isPipelineRunFailed(focusRun))
  const pollSteps = pinnedName != null || running || (focusRun != null && !terminal)

  const logsQuery = useQuery({
    queryKey: ['delivery', 'logs', DELIVER_STG_PIPELINE, focusRun?.name, 'active'],
    queryFn: () => fetchPipelineRunLogs(focusRun!.name, runsQuery.data?.namespace ?? focusRun!.namespace),
    enabled: focusRun != null,
    refetchInterval: query => {
      if (focusRun == null) return false
      if (isPipelineRunRunning(focusRun)) return 5_000
      if (logsNeedPoll(query.state.data?.logs)) return 5_000
      return false
    },
  })

  const gatewayUrl = 'http://192.168.10.73:30880/'

  return (
    <OpsSection
      title={running ? 'Active deliver run' : 'Latest deliver run'}
      description={
        running
          ? 'Live Tekton logs — bifrost-deliver-stg pipeline in progress (refreshes every 5s).'
          : 'Most recent bifrost-deliver-stg run. Start a new deliver from Supply chain above.'
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
          No deliver runs yet — use Supply chain → Run deliver-stg.
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
          />
          <pre className="llm-content-pre m-0 mt-3 max-h-56 overflow-auto font-mono-tabular text-[var(--text-dense-meta)]">
            {logsQuery.isLoading && logsQuery.data == null
              ? 'Loading logs…'
              : logsQuery.data?.logs ?? '(empty)'}
          </pre>
          {terminal && isPipelineRunSucceeded(focusRun) && (
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Deliver succeeded — verify STG below.
              </span>
              <Button variant="outline" size="sm" asChild>
                <a href={gatewayUrl} target="_blank" rel="noreferrer">
                  Open STG gateway
                </a>
              </Button>
            </div>
          )}
          {terminal && isPipelineRunFailed(focusRun) && (
            <p className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
              Deliver failed — open Observe → Pipeline runs for full logs and Ask AI triage.
            </p>
          )}
        </>
      )}
    </OpsSection>
  )
}
