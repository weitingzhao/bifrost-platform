import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchSepaStats, isProxyError } from '@/api/marketDataPlugin'
import { CoverageBarRow } from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'

const TABLE_ALIAS: Record<string, string> = {
  option_open_interest: 'opt_oi',
  option_snapshot: 'opt_snap',
  option_contract: 'opt_ctr',
  stock_adjustment: 'stk_adj',
}

function shortTable(name: string | undefined): string {
  if (name == null || name === '') return '—'
  const bare = name.replace(/^market\./, '').replace(/^analytics\./, 'a.')
  return TABLE_ALIAS[bare] ?? bare
}

function shortDay(iso: string | null | undefined): string {
  if (iso == null || iso === '') return '—'
  return iso.slice(0, 10).slice(5)
}

function latestKind(iso: string | null | undefined): 'ok' | 'scheduled' | 'missing' {
  if (iso == null || iso === '') return 'missing'
  const day = iso.slice(0, 10)
  const today = new Date().toISOString().slice(0, 10)
  if (day === today) return 'ok'
  const t = Date.parse(`${day}T00:00:00Z`)
  if (!Number.isFinite(t)) return 'missing'
  const age = (Date.now() - t) / 86_400_000
  if (age <= 3) return 'scheduled'
  return 'missing'
}

export function SepaStatsSection() {
  const q = useQuery({
    queryKey: ['market-data', 'coverage', 'sepa-stats'],
    queryFn: fetchSepaStats,
    refetchInterval: 60_000,
    retry: 1,
  })

  const proxyErr = q.data != null && isProxyError(q.data) ? q.data.error : null
  const tables =
    q.data != null && !isProxyError(q.data) ? (q.data.tables ?? []) : []
  const maxRows = Math.max(1, ...tables.map(t => t.row_count ?? 0))
  const fresh = tables.filter(t => latestKind(t.latest) === 'ok').length

  return (
    <OpsSection
      title="SEPA table stats"
      description="Bar = rows vs max table. Color = latest-day freshness."
      headerExtra={
        tables.length > 0 ? (
          <DenseTag variant={fresh === tables.length ? 'success' : fresh > 0 ? 'warning' : 'danger'}>
            {fresh}/{tables.length} today
          </DenseTag>
        ) : null
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {q.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading SEPA stats…
        </p>
      ) : proxyErr != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{proxyErr}</p>
      ) : tables.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No SEPA table stats
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-x-5 gap-y-1 md:grid-cols-2">
          {tables.map(t => {
            const rows = t.row_count ?? 0
            const kind = latestKind(t.latest)
            return (
              <CoverageBarRow
                key={t.table ?? 'unknown'}
                name={shortTable(t.table)}
                nameTitle={t.table ?? undefined}
                fillPct={(rows / maxRows) * 100}
                toneClass={toneByLevel(kind)}
                meterLabel={`${t.table} ${fmtCount(rows)} · latest ${t.latest ?? '—'}`}
                value={rows}
                valueText={fmtCount(t.row_count)}
                suffix={
                  <span className="font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
                    {shortDay(t.latest)}
                  </span>
                }
              />
            )
          })}
        </div>
      )}
    </OpsSection>
  )
}
