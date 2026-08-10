import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag } from '@bifrost/ui'
import {
  fetchHermesInsights,
  fetchHermesReadiness,
  HERMES_CHAT_UI_URL,
  runHermesFirstTask,
  type HermesInsight,
} from '@/api/hermes'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip, type OpsVerdictLamp, type OpsVerdictTagVariant } from '@/components/layout/OpsVerdictStrip'
import { PageToolbar } from '@/components/layout/PageToolbar'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'

function insightVerdictVariant(verdict: string): 'success' | 'warning' | 'danger' | 'neutral' {
  const v = verdict.toLowerCase()
  if (v === 'ok' || v === 'success') return 'success'
  if (v === 'warn' || v === 'warning' || v === 'pending') return 'warning'
  if (v === 'blocked' || v === 'fail' || v === 'error') return 'danger'
  return 'neutral'
}

function formatInsightTime(iso: string): string {
  const ms = Date.parse(iso)
  if (!Number.isFinite(ms)) return iso || '—'
  return new Date(ms).toLocaleString()
}

export function AnalysisWorkspacePage() {
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()
  const readinessQ = useQuery({
    queryKey: ['hermes', 'readiness'],
    queryFn: fetchHermesReadiness,
    refetchInterval: 20_000,
  })
  const insightsQ = useQuery({
    queryKey: ['hermes', 'insights', 3],
    queryFn: () => fetchHermesInsights(3),
    refetchInterval: 30_000,
    retry: false,
  })

  const runFirst = useMutation({
    mutationFn: runHermesFirstTask,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['hermes', 'insights'] })
      void qc.invalidateQueries({ queryKey: ['hermes', 'readiness'] })
    },
  })

  const ready = readinessQ.data?.ready === true
  const lamp: OpsVerdictLamp = readinessQ.isError
    ? 'fail'
    : readinessQ.isLoading
      ? 'unknown'
      : ready
        ? 'ok'
        : 'degraded'
  const tag: OpsVerdictTagVariant =
    lamp === 'ok' ? 'success' : lamp === 'fail' ? 'danger' : lamp === 'degraded' ? 'warning' : 'neutral'
  const tagLabel =
    lamp === 'ok' ? 'READY' : lamp === 'fail' ? 'UNREACHABLE' : lamp === 'degraded' ? 'NOT READY' : 'LOADING'

  const items = insightsQ.data?.items ?? []
  const insightsError = insightsQ.error as Error | null
  const runError =
    runFirst.error instanceof Error
      ? runFirst.error.message
      : runFirst.data?.ok === false
        ? (runFirst.data.error ?? 'First Task failed')
        : null

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <OpsVerdictStrip
        title="ANALYSIS WORKSPACE"
        lamp={lamp}
        tagLabel={tagLabel}
        tagVariant={tag}
        summary="Hermes Analysis Desk V1 — status, Chat UI, and First Task. No stock-analysis engine."
        actions={
          <>
            <Button
              type="button"
              size="xs"
              disabled={!canOperate || runFirst.isPending}
              title={!canOperate ? 'Authenticate as operator to run First Task' : 'POST /api/v1/hermes/run-first-task'}
              onClick={() => runFirst.mutate()}
            >
              {runFirst.isPending ? 'Running…' : 'Run First Task'}
            </Button>
            <a
              href={HERMES_CHAT_UI_URL}
              target="_blank"
              rel="noreferrer"
              className="inline-flex h-6 items-center rounded-md border border-border px-2 text-[var(--text-dense-caption)] text-primary hover:bg-secondary"
            >
              Open Chat UI ↗
            </a>
          </>
        }
      />

      <PageToolbar align="between">
        <span className="text-[var(--text-dense-caption)] text-warning">
          Analysis is read-only. No trading actuation.
        </span>
      </PageToolbar>

      {runError != null && (
        <OpsFeedback variant="error" title="First Task failed">
          {runError}
        </OpsFeedback>
      )}

      <OpsSection
        title="Last insights"
        description="GET /api/v1/hermes/insights?limit=3 — empty until the Analysis API is live."
        bodyPadding="compact"
        overflow="visible"
      >
        {insightsQ.isLoading ? (
          <p className="text-[var(--text-dense-meta)] text-muted-foreground">Loading…</p>
        ) : insightsError != null ? (
          <p className="text-[var(--text-dense-meta)] text-muted-foreground">
            Insights API unavailable — {insightsError.message}
          </p>
        ) : items.length === 0 ? (
          <p className="text-[var(--text-dense-meta)] text-muted-foreground">No insights yet.</p>
        ) : (
          <ul className="flex flex-col gap-1.5">
            {items.map((item: HermesInsight) => (
              <li
                key={item.id}
                className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 rounded-md border border-border bg-secondary/40 px-2.5 py-1.5"
              >
                <span className="font-mono-tabular text-[var(--text-dense-caption)] text-muted-foreground">
                  {formatInsightTime(item.time)}
                </span>
                <span className="text-[var(--text-dense-label)] font-semibold">
                  {item.symbol !== '' ? item.symbol : item.type}
                </span>
                <DenseTag variant={insightVerdictVariant(item.verdict)}>{item.verdict}</DenseTag>
                {item.summary != null && item.summary !== '' && (
                  <span className="min-w-0 truncate text-[var(--text-dense-caption)] text-muted-foreground">
                    {item.summary}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </OpsSection>
    </div>
  )
}
