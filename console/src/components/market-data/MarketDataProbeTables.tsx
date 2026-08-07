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
import type {
  MarketDataDeploymentInfo,
  MarketDataFreshnessInfo,
  MarketDataWorkerInfo,
} from '@/api/satelliteBusTypes'
import { OpsSection } from '@/components/layout/OpsSection'

function freshnessVerdictVariant(
  verdict: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  if (verdict === 'ok') return 'success'
  if (verdict === 'stale') return 'warning'
  if (verdict === 'fail') return 'danger'
  return 'neutral'
}

export function sortFreshness(rows: MarketDataFreshnessInfo[]): MarketDataFreshnessInfo[] {
  const rank = (v: string) => {
    if (v === 'stale') return 0
    if (v === 'fail') return 1
    if (v === 'unknown') return 2
    return 3
  }
  return [...rows].sort((a, b) => {
    const d = rank(a.verdict) - rank(b.verdict)
    if (d !== 0) return d
    return a.dimension.localeCompare(b.dimension)
  })
}

export function workerReady(w: MarketDataWorkerInfo): boolean {
  return w.status == null || w.status === '' || w.status.toLowerCase() === 'ok'
}

function formatUptime(sec: number | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  if (sec < 60) return `${Math.round(sec)}s`
  if (sec < 3600) return `${Math.round(sec / 60)}m`
  return `${(sec / 3600).toFixed(1)}h`
}

export function MarketDataFreshnessTable({
  rows,
  collapsibleWhenOk,
}: {
  rows: MarketDataFreshnessInfo[]
  collapsibleWhenOk: boolean
}) {
  const table = (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Dimension</DenseTableHead>
          <DenseTableHead>Verdict</DenseTableHead>
          <DenseTableHead>Age</DenseTableHead>
          <DenseTableHead>Last run</DenseTableHead>
          <DenseTableHead>Rows</DenseTableHead>
          <DenseTableHead>Status</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {rows.map(f => (
          <DenseTableRow key={f.dimension}>
            <DenseTableCell className="font-mono text-xs">{f.dimension}</DenseTableCell>
            <DenseTableCell>
              <DenseTag variant={freshnessVerdictVariant(f.verdict)}>{f.verdict}</DenseTag>
            </DenseTableCell>
            <DenseTableCell className="font-mono tabular-nums">
              {Number.isFinite(f.age_hours) ? `${f.age_hours.toFixed(1)}h` : '—'}
            </DenseTableCell>
            <DenseTableCell className="font-mono text-xs">
              {f.last_run_at != null && f.last_run_at !== '' ? f.last_run_at : '—'}
            </DenseTableCell>
            <DenseTableCell className="font-mono tabular-nums">{f.rows_written}</DenseTableCell>
            <DenseTableCell>{f.status ?? '—'}</DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
  if (!collapsibleWhenOk) return table
  return (
    <OpsSection
      variant="flat"
      title="All dimensions ok"
      collapsible
      defaultCollapsed
      bodyPadding="none"
      overflow="visible"
    >
      {table}
    </OpsSection>
  )
}

export function MarketDataWorkersTable({
  deployments,
  workers,
  collapsibleWhenOk,
}: {
  deployments: MarketDataDeploymentInfo[]
  workers: MarketDataWorkerInfo[]
  collapsibleWhenOk: boolean
}) {
  const table = (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Name / pool</DenseTableHead>
          <DenseTableHead>Ready / status</DenseTableHead>
          <DenseTableHead>Done / fail</DenseTableHead>
          <DenseTableHead>Uptime</DenseTableHead>
          <DenseTableHead>Last claim</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {deployments.map(d => (
          <DenseTableRow key={`deploy-${d.name}`}>
            <DenseTableCell className="font-semibold">{d.name}</DenseTableCell>
            <DenseTableCell>
              <DenseTag
                variant={
                  d.reachability === 'ok'
                    ? 'success'
                    : d.reachability === 'degraded'
                      ? 'warning'
                      : d.reachability === 'fail'
                        ? 'danger'
                        : 'neutral'
                }
              >
                {d.ready}
              </DenseTag>
            </DenseTableCell>
            <DenseTableCell className="text-[var(--muted-foreground)]">—</DenseTableCell>
            <DenseTableCell className="text-[var(--muted-foreground)]">—</DenseTableCell>
            <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {d.detail ?? '—'}
            </DenseTableCell>
          </DenseTableRow>
        ))}
        {workers.map(w => (
          <DenseTableRow key={`pool-${w.pool}`}>
            <DenseTableCell className="font-mono text-xs">pool {w.pool}</DenseTableCell>
            <DenseTableCell>
              <DenseTag variant={workerReady(w) ? 'success' : 'warning'}>
                {w.status ?? 'ok'}
              </DenseTag>
            </DenseTableCell>
            <DenseTableCell className="font-mono tabular-nums">
              {w.jobs_done} / {w.jobs_failed}
            </DenseTableCell>
            <DenseTableCell className="font-mono tabular-nums">
              {formatUptime(w.uptime_sec)}
            </DenseTableCell>
            <DenseTableCell className="font-mono text-xs">
              {w.last_claim_at != null && w.last_claim_at !== '' ? w.last_claim_at : '—'}
            </DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
  if (!collapsibleWhenOk) return table
  return (
    <OpsSection
      variant="flat"
      title="All workers ready"
      collapsible
      defaultCollapsed
      bodyPadding="none"
      overflow="visible"
    >
      {table}
    </OpsSection>
  )
}
