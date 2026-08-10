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

function formatUptime(sec: number | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  if (sec < 60) return `${Math.round(sec)}s`
  if (sec < 3600) return `${Math.round(sec / 60)}m`
  return `${(sec / 3600).toFixed(1)}h`
}

function timeAgo(iso: string | undefined | null): string {
  if (iso == null || iso === '') return '—'
  const ms = Date.now() - new Date(iso).getTime()
  if (!Number.isFinite(ms) || ms < 0) return '—'
  const sec = ms / 1000
  if (sec < 60) return `${Math.round(sec)}s ago`
  if (sec < 3600) return `${Math.round(sec / 60)}m ago`
  if (sec < 86400) return `${(sec / 3600).toFixed(1)}h ago`
  return `${(sec / 86400).toFixed(1)}d ago`
}

function timeUntil(iso: string | undefined | null): string {
  if (iso == null || iso === '') return '—'
  const ms = new Date(iso).getTime() - Date.now()
  if (!Number.isFinite(ms)) return '—'
  if (ms <= 0) return 'now'
  const sec = ms / 1000
  if (sec < 60) return `in ${Math.round(sec)}s`
  if (sec < 3600) return `in ${Math.round(sec / 60)}m`
  if (sec < 86400) return `in ${(sec / 3600).toFixed(1)}h`
  return `in ${(sec / 86400).toFixed(1)}d`
}

interface MergedWorkerRow {
  name: string
  ready?: string
  reachability?: string
  pool?: MarketDataWorkerInfo
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
  const poolByName = new Map<string, MarketDataWorkerInfo>()
  for (const w of workers) {
    poolByName.set(w.pool, w)
  }

  const rows: MergedWorkerRow[] = deployments.map(d => {
    const poolKey = d.name.replace(/^polygon-worker-/, '')
    return {
      name: d.name,
      ready: d.ready,
      reachability: d.reachability,
      pool: poolByName.get(poolKey),
    }
  })

  for (const w of workers) {
    const alreadyMerged = rows.some(r => r.pool === w)
    if (!alreadyMerged) {
      rows.push({ name: `pool-${w.pool}`, pool: w })
    }
  }

  const table = (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Worker</DenseTableHead>
          <DenseTableHead>Replicas</DenseTableHead>
          <DenseTableHead>Done / Fail</DenseTableHead>
          <DenseTableHead>Last Activity</DenseTableHead>
          <DenseTableHead>Next Run</DenseTableHead>
          <DenseTableHead>Uptime</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {rows.map(r => {
          const p = r.pool
          const reachVariant =
            r.reachability === 'ok'
              ? 'success'
              : r.reachability === 'degraded'
                ? 'warning'
                : r.reachability === 'fail'
                  ? 'danger'
                  : ('neutral' as const)

          return (
            <DenseTableRow key={r.name}>
              <DenseTableCell className="font-semibold">{r.name}</DenseTableCell>
              <DenseTableCell>
                {r.ready != null ? (
                  <DenseTag variant={reachVariant}>{r.ready}</DenseTag>
                ) : (
                  '—'
                )}
              </DenseTableCell>
              <DenseTableCell className="font-mono tabular-nums">
                {p != null ? `${p.jobs_done} / ${p.jobs_failed}` : '—'}
              </DenseTableCell>
              <DenseTableCell className="font-mono tabular-nums">
                {p != null ? timeAgo(p.last_claim_at) : '—'}
              </DenseTableCell>
              <DenseTableCell className="font-mono tabular-nums">
                {p != null ? timeUntil(p.next_run_at) : '—'}
              </DenseTableCell>
              <DenseTableCell className="font-mono tabular-nums">
                {p != null ? formatUptime(p.uptime_sec) : '—'}
              </DenseTableCell>
            </DenseTableRow>
          )
        })}
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
