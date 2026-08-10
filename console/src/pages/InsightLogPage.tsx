import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeader,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTag,
  denseTableNumCell,
} from '@bifrost/ui'
import { fetchHermesInsights, type HermesInsight } from '@/api/hermes'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip, type OpsVerdictLamp, type OpsVerdictTagVariant } from '@/components/layout/OpsVerdictStrip'

function verdictVariant(verdict: string): 'success' | 'warning' | 'danger' | 'neutral' {
  const v = verdict.toLowerCase()
  if (v === 'ok' || v === 'success') return 'success'
  if (v === 'warn' || v === 'warning' || v === 'pending') return 'warning'
  if (v === 'blocked' || v === 'fail' || v === 'error') return 'danger'
  return 'neutral'
}

function formatTime(iso: string): string {
  const ms = Date.parse(iso)
  if (!Number.isFinite(ms)) return iso || '—'
  return new Date(ms).toLocaleString()
}

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '—'
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60_000).toFixed(1)}m`
}

export function InsightLogPage() {
  const insightsQ = useQuery({
    queryKey: ['hermes', 'insights', 50],
    queryFn: () => fetchHermesInsights(50),
    refetchInterval: 30_000,
    retry: false,
  })

  const items = insightsQ.data?.items ?? []
  const total = insightsQ.data?.total ?? items.length
  const err = insightsQ.error as Error | null
  const lamp: OpsVerdictLamp = insightsQ.isError ? 'degraded' : insightsQ.isLoading ? 'unknown' : items.length > 0 ? 'ok' : 'unknown'
  const tag: OpsVerdictTagVariant = insightsQ.isError ? 'warning' : items.length > 0 ? 'neutral' : 'neutral'
  const tagLabel = insightsQ.isError ? 'API DOWN' : insightsQ.isLoading ? 'LOADING' : `${total} INSIGHTS`

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <OpsVerdictStrip
        title="INSIGHT LOG"
        lamp={lamp}
        tagLabel={tagLabel}
        tagVariant={tag}
        summary="Hermes analysis history — read-only. D10: no trading actuation."
      />

      <OpsSection title="Insights" overflow="visible">
        {insightsQ.isLoading ? (
          <p className="py-4 text-center text-[var(--text-dense-meta)] text-muted-foreground">Loading…</p>
        ) : err != null ? (
          <p className="py-4 text-center text-[var(--text-dense-meta)] text-muted-foreground">
            Insights API unavailable — {err.message}
          </p>
        ) : items.length === 0 ? (
          <p className="py-4 text-center text-[var(--text-dense-meta)] text-muted-foreground">No insights yet.</p>
        ) : (
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Time</DenseTableHead>
                <DenseTableHead>Symbol</DenseTableHead>
                <DenseTableHead>Type</DenseTableHead>
                <DenseTableHead>Verdict</DenseTableHead>
                <DenseTableHead className={denseTableNumCell}>Duration</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {items.map((row: HermesInsight) => (
                <DenseTableRow key={row.id}>
                  <DenseTableCell className="font-mono-tabular">{formatTime(row.time)}</DenseTableCell>
                  <DenseTableCell className="font-semibold">
                    {row.symbol !== '' ? row.symbol : '—'}
                  </DenseTableCell>
                  <DenseTableCell>{row.type}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={verdictVariant(row.verdict)}>{row.verdict}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className={denseTableNumCell}>
                    {formatDuration(row.duration_ms)}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        )}
      </OpsSection>
    </div>
  )
}
