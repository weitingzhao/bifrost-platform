import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, SegmentControl, cn } from '@bifrost/ui'
import {
  fetchIngestHistory,
  isProxyError,
  type IngestHistoryResponse,
} from '@/api/marketDataPlugin'
import { shortIngestKind } from '@/components/market-data/ingestKindLabel'
import {
  buildDailyVolumeViews,
  topKindChips,
} from '@/components/market-data/ingestDailyVolumeModel'
import { StackedBar } from '@/components/market-data/overviewDash'
import { fmtCount } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'
import { dagsterSchedulesUrl } from '@/lib/architecture/opsToolRackCatalog'

type DaysWindow = '7' | '14' | '30'

export function IngestDailyVolume({
  onSelectKind,
}: {
  onSelectKind?: (kind: string) => void
}) {
  const [daysWindow, setDaysWindow] = useState<DaysWindow>('14')
  const [kindFilter, setKindFilter] = useState('')
  const days = Number(daysWindow) as 7 | 14 | 30

  const histQ = useQuery({
    queryKey: ['market-data', 'ingest', 'history', days],
    queryFn: () => fetchIngestHistory({ days }),
    refetchInterval: 60_000,
    retry: 1,
  })

  const raw = histQ.data
  const err = raw != null && isProxyError(raw) ? raw.error : null
  const hist: IngestHistoryResponse | null =
    raw != null && !isProxyError(raw) ? raw : null

  const { rows, maxTotal } = useMemo(
    () => buildDailyVolumeViews(hist?.days_series ?? [], kindFilter),
    [hist?.days_series, kindFilter],
  )
  const chips = useMemo(() => topKindChips(hist?.kind_totals ?? []), [hist?.kind_totals])
  const windowTotal = rows.reduce((s, r) => s + r.total, 0)
  const windowFailed = rows.reduce((s, r) => s + r.failed, 0)

  return (
    <OpsSection
      title="Daily volume"
      description="UTC calendar days from job_ingest · trim ~7d may empty older bars · Dagster = ignition, not job counts"
      headerExtra={
        hist != null ? (
          <DenseTag variant="neutral">
            {fmtCount(windowTotal)} jobs · {fmtCount(windowFailed)} failed
          </DenseTag>
        ) : null
      }
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <SegmentControl
            size="sm"
            ariaLabel="Daily volume window"
            value={daysWindow}
            onChange={v => setDaysWindow(v as DaysWindow)}
            options={[
              { value: '7', label: '7d' },
              { value: '14', label: '14d' },
              { value: '30', label: '30d' },
            ]}
          />
          <Button variant="outline" size="sm" asChild>
            <a href={dagsterSchedulesUrl()} target="_blank" rel="noreferrer">
              Dagster
            </a>
          </Button>
        </div>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {histQ.isLoading && hist == null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading daily volume…
        </p>
      ) : err != null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          Daily volume needs Plugin ≥0.9.7 ({err})
        </p>
      ) : hist == null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No history
        </p>
      ) : (
        <div className="flex flex-col gap-2 px-3 py-2">
          {chips.length > 0 ? (
            <div className="flex flex-wrap items-center gap-1">
              <button
                type="button"
                className={cn(
                  'rounded-sm border px-1.5 py-0.5 font-mono text-[var(--text-dense-caption)]',
                  kindFilter === ''
                    ? 'border-[var(--color-info)] bg-[var(--color-info)]/10'
                    : 'border-[var(--border)] text-[var(--muted-foreground)]',
                )}
                onClick={() => setKindFilter('')}
              >
                all
              </button>
              {chips.map(k => (
                <button
                  key={k.kind}
                  type="button"
                  title={`${k.kind} · ${fmtCount(k.total ?? 0)}`}
                  className={cn(
                    'rounded-sm border px-1.5 py-0.5 font-mono text-[var(--text-dense-caption)]',
                    kindFilter === k.kind
                      ? 'border-[var(--color-info)] bg-[var(--color-info)]/10'
                      : 'border-[var(--border)] text-[var(--muted-foreground)]',
                  )}
                  onClick={() => {
                    const next = kindFilter === k.kind ? '' : k.kind
                    setKindFilter(next)
                    if (next !== '' && onSelectKind != null) onSelectKind(next)
                  }}
                >
                  {shortIngestKind(k.kind)} {fmtCount(k.total ?? 0)}
                </button>
              ))}
            </div>
          ) : null}

          <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {hist.retention_note ?? 'job_ingest retention applies'}
            {kindFilter !== '' ? ` · scoped to ${kindFilter}` : ''}
          </p>

          <div className="flex flex-col gap-1">
            {[...rows].reverse().map(r => {
              const widthPct = Math.max(2, (r.total / maxTotal) * 100)
              const tip = [
                r.day,
                `done ${r.done}`,
                `failed ${r.failed}`,
                `active ${r.active}`,
                r.topKinds.length > 0
                  ? `top ${r.topKinds.map(t => `${shortIngestKind(t.kind)}=${t.total}`).join(' · ')}`
                  : null,
              ]
                .filter(Boolean)
                .join(' · ')
              return (
                <div
                  key={r.day}
                  className="grid grid-cols-[3.25rem_minmax(0,1fr)_max-content] items-center gap-x-2"
                  title={tip}
                >
                  <span className="font-mono text-[var(--text-dense-caption)] tabular-nums text-[var(--muted-foreground)]">
                    {r.label}
                  </span>
                  <div className="min-w-0" style={{ width: `${widthPct}%` }}>
                    {r.total === 0 ? (
                      <div className="h-1.5 rounded-sm bg-[var(--muted)]/40" />
                    ) : (
                      <StackedBar
                        readyPct={r.donePct}
                        thinPct={r.activePct}
                        blockedPct={r.failedPct}
                      />
                    )}
                  </div>
                  <span className="font-mono text-[var(--text-dense-caption)] tabular-nums">
                    {fmtCount(r.total)}
                    {r.failed > 0 ? (
                      <span className="ml-1 text-[var(--destructive)]">{fmtCount(r.failed)}</span>
                    ) : null}
                  </span>
                </div>
              )
            })}
          </div>

          <div className="flex flex-wrap gap-3 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            <span className="inline-flex items-center gap-1">
              <span className="inline-block h-1.5 w-2.5 rounded-sm bg-[var(--color-success)]" />
              done
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="inline-block h-1.5 w-2.5 rounded-sm bg-[var(--color-warning)]" />
              pending/running
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="inline-block h-1.5 w-2.5 rounded-sm bg-[var(--color-danger,var(--destructive))]" />
              failed
            </span>
          </div>
        </div>
      )}
    </OpsSection>
  )
}
