import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import {
  fetchAnalyticsAtmIv,
  fetchAnalyticsIvPercentile,
  fetchAnalyticsMaxPain,
  fetchAnalyticsPcr,
  isProxyError,
  type AnalyticsListResponse,
  type MarketDataProxyError,
} from '@/api/marketDataPlugin'
import { MarketDataJsonProbeCard } from '@/components/market-data/MarketDataJsonProbeCard'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { MARKET_DATA_SCHEDULE_SLOTS } from '@/lib/market-data/scheduleSummary'

function rowsFrom(
  res: AnalyticsListResponse | MarketDataProxyError | undefined,
): {
  rows: Record<string, unknown>[]
  error: string | null
  loading: boolean
} {
  if (res == null) return { rows: [], error: null, loading: true }
  if (isProxyError(res)) return { rows: [], error: res.error, loading: false }
  const list = res as AnalyticsListResponse
  const rows = Array.isArray(list.rows)
    ? list.rows
    : Array.isArray((list as { data?: unknown }).data)
      ? ((list as { data: Record<string, unknown>[] }).data ?? [])
      : []
  return { rows, error: list.error ?? null, loading: false }
}

function SampleTable({
  title,
  rows,
  error,
  loading,
  columns,
}: {
  title: string
  rows: Record<string, unknown>[]
  error: string | null
  loading: boolean
  columns: string[]
}) {
  return (
    <div className="flex flex-col gap-1">
      <OpsSubsectionTitle>{title}</OpsSubsectionTitle>
      {loading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading…
        </p>
      ) : error != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</p>
      ) : rows.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No rows — CronJob may not have computed yet
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              {columns.map(c => (
                <DenseTableHead key={c}>{c}</DenseTableHead>
              ))}
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {rows.slice(0, 8).map((row, i) => (
              <DenseTableRow key={i}>
                {columns.map(c => (
                  <DenseTableCell key={c} className="font-mono text-xs">
                    {row[c] != null ? String(row[c]) : '—'}
                  </DenseTableCell>
                ))}
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </div>
  )
}

export function MarketDataAnalyticsTab() {
  const [symbol, setSymbol] = useState('AAPL')

  const common = { symbol: symbol.trim() || undefined, lookback_days: 5 }

  const maxPainQ = useQuery({
    queryKey: ['market-data', 'analytics', 'max-pain', common.symbol],
    queryFn: () => fetchAnalyticsMaxPain(common),
    refetchInterval: 60_000,
    retry: 1,
  })
  const atmQ = useQuery({
    queryKey: ['market-data', 'analytics', 'atm-iv', common.symbol],
    queryFn: () => fetchAnalyticsAtmIv(common),
    refetchInterval: 60_000,
    retry: 1,
  })
  const pcrQ = useQuery({
    queryKey: ['market-data', 'analytics', 'pcr', common.symbol],
    queryFn: () => fetchAnalyticsPcr(common),
    refetchInterval: 60_000,
    retry: 1,
  })
  const ivpQ = useQuery({
    queryKey: ['market-data', 'analytics', 'iv-percentile', common.symbol],
    queryFn: () => fetchAnalyticsIvPercentile(common),
    refetchInterval: 60_000,
    retry: 1,
  })

  const maxPain = rowsFrom(maxPainQ.data)
  const atm = rowsFrom(atmQ.data)
  const pcr = rowsFrom(pcrQ.data)
  const ivp = rowsFrom(ivpQ.data)

  return (
    <div className="flex flex-col gap-4">
      <OpsSection
        title="Analytics samples"
        description="Plugin /market/analytics/* — last compute samples (lookback 5d)"
        headerExtra={
          <label className="flex items-center gap-2">
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Symbol
            </span>
            <input
              className="h-7 w-24 rounded-md border border-[var(--border)] bg-[var(--background)] px-2 font-mono text-[var(--text-dense-meta)] uppercase"
              value={symbol}
              onChange={e => setSymbol(e.target.value.toUpperCase())}
            />
          </label>
        }
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        <div className="flex flex-col gap-4">
          <SampleTable
            title="Max Pain"
            loading={maxPainQ.isLoading}
            error={maxPain.error}
            rows={maxPain.rows}
            columns={['symbol', 'expiry', 'trade_date', 'max_pain_strike', 'computed_at']}
          />
          <SampleTable
            title="ATM IV"
            loading={atmQ.isLoading}
            error={atm.error}
            rows={atm.rows}
            columns={['symbol', 'trade_date', 'atm_iv', 'underlying_price', 'computed_at']}
          />
          <SampleTable
            title="PCR"
            loading={pcrQ.isLoading}
            error={pcr.error}
            rows={pcr.rows}
            columns={['symbol', 'trade_date', 'pcr_oi', 'total_put_oi', 'total_call_oi', 'computed_at']}
          />
          <SampleTable
            title="IV Percentile"
            loading={ivpQ.isLoading}
            error={ivp.error}
            rows={ivp.rows}
            columns={['symbol', 'trade_date', 'iv_percentile_1y', 'iv_current', 'lookback_days', 'computed_at']}
          />
        </div>
      </OpsSection>

      <OpsSection
        title="CronJob schedule"
        description="Static summary from plugin schedule.yaml (K8s CronJob is runtime source)"
        bodyPadding="none"
        overflow="visible"
        collapsible
        defaultCollapsed
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Slot</DenseTableHead>
              <DenseTableHead>Cron (UTC)</DenseTableHead>
              <DenseTableHead>Note</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {MARKET_DATA_SCHEDULE_SLOTS.map(s => (
              <DenseTableRow key={s.id}>
                <DenseTableCell>
                  <DenseTag variant="info">{s.id}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono text-xs">{s.cron}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {s.note}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <MarketDataJsonProbeCard
        title="JSON Probe"
        description="Probe analytics endpoints"
        defaultPath={`/market/analytics/max-pain?symbol=${encodeURIComponent(symbol || 'AAPL')}&lookback_days=5`}
      />
    </div>
  )
}
