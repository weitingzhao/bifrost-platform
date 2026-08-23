import { cn } from '@bifrost/ui'
import type { IngestQueueDashboardResponse, IngestQueueKindCount } from '@/api/marketDataPlugin'
import { shortIngestKind } from '@/components/market-data/ingestKindLabel'
import {
  buildQueuePressure,
  utcClock,
  type QueuePressureLevel,
} from '@/components/market-data/jobQueuePressureModel'
import {
  CoverageBarRow,
  DashCard,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import { formatDurationParts } from '@/lib/patrol/cronSchedule'
import { kindQueueCountsLabel } from '@/components/market-data/queueRunningJobs'

function levelLabel(level: QueuePressureLevel): string {
  if (level === 'idle') return 'idle'
  if (level === 'low') return 'light'
  if (level === 'elevated') return 'elevated'
  if (level === 'high') return 'high'
  return 'stalled'
}

function levelTone(
  level: QueuePressureLevel,
): 'ok' | 'scheduled' | 'missing' | 'unknown' {
  if (level === 'idle' || level === 'low') return 'ok'
  if (level === 'elevated') return 'scheduled'
  if (level === 'high' || level === 'stalled') return 'missing'
  return 'unknown'
}

export function JobQueuePressure({
  dash,
  kinds,
  selectedKind,
  nowMs,
  onSelectKind,
}: {
  dash: IngestQueueDashboardResponse | null
  kinds: IngestQueueKindCount[]
  selectedKind: string
  nowMs: number
  onSelectKind?: (kind: string) => void
}) {
  const view = buildQueuePressure({
    pending: dash?.queue?.ready_now ?? dash?.queue?.pending ?? 0,
    running: dash?.queue?.running ?? 0,
    ratePerMin: dash?.throughput?.jobs_per_min_15m,
    etaMinutes: dash?.throughput?.eta_minutes_at_current_rate,
    oldestPendingAgeSec: dash?.queue?.oldest_pending_age_sec,
    doneLast5m: dash?.throughput?.done_last_5m,
    doneLast15m: dash?.throughput?.done_last_15m,
    doneLast60m: dash?.throughput?.done_last_60m,
    kinds,
    selectedKind,
    nowMs,
  })
  const maxKind = Math.max(1, ...view.kinds.map(k => k.active))
  const maxDone = Math.max(1, view.doneLast5m, view.doneLast15m, view.doneLast60m)
  const etaLabel =
    view.etaMinutes != null
      ? formatDurationParts(view.etaMinutes * 60_000)
      : view.level === 'stalled'
        ? 'no rate'
        : '—'
  const emptyLabel = view.emptyAtMs != null ? utcClock(view.emptyAtMs) : null
  const waitedLabel =
    view.waitedSec != null ? formatDurationParts(view.waitedSec * 1000) : null
  const scoped = selectedKind !== ''
  const idle = view.pending + view.running === 0

  return (
    <div className="flex flex-col gap-1.5 border-b border-[var(--border)] px-3 py-2">
      <div className="flex items-stretch gap-2">
        <ScoreRing
          ready={idle ? 1 : view.running}
          thin={idle ? 0 : view.pending}
          blocked={view.level === 'stalled' ? 1 : 0}
          total={Math.max(idle ? 1 : view.running + view.pending + (view.level === 'stalled' ? 1 : 0), 1)}
          caption={idle ? 'idle' : 'run'}
        />
        <div className="grid min-w-0 flex-1 grid-cols-2 gap-1.5 xl:grid-cols-3">
          <DashCard
            title="Pressure"
            tag={levelLabel(view.level)}
            tagVariant={
              view.level === 'stalled' || view.level === 'high'
                ? 'danger'
                : view.level === 'elevated'
                  ? 'warning'
                  : view.level === 'idle'
                    ? 'success'
                    : 'neutral'
            }
            value={fmtCount(view.pending)}
            rawValue={view.pending}
            invertFlash={view.pending > 0}
            unit="ready"
            caption={scoped ? selectedKind : `${fmtCount(view.running)} running`}
          >
            <Meter
              fillPct={view.fillPct}
              toneClass={toneByLevel(levelTone(view.level))}
              label={`pending ${view.pending}`}
            />
          </DashCard>
          <DashCard
            title="Drain"
            value={etaLabel}
            rawValue={view.etaMinutes}
            invertFlash={view.etaMinutes != null && view.etaMinutes >= 15}
            caption={
              emptyLabel != null
                ? `empty ~${emptyLabel}`
                : waitedLabel != null
                  ? `waited ${waitedLabel}`
                  : 'at last-15m rate'
            }
          >
            <div className="relative h-1.5 overflow-hidden rounded-sm bg-[var(--muted)]">
              {view.progress01 != null ? (
                <>
                  <div
                    className="absolute inset-y-0 left-0 bg-[var(--color-success)]"
                    style={{ width: `${view.progress01 * 100}%` }}
                    title={waitedLabel != null ? `waited ${waitedLabel}` : 'waited'}
                  />
                  <div
                    className="absolute inset-y-0 bg-[var(--color-warning)]"
                    style={{
                      left: `${view.progress01 * 100}%`,
                      width: `${(1 - view.progress01) * 100}%`,
                    }}
                    title={`remaining ${etaLabel}`}
                  />
                </>
              ) : (
                <div
                  className={`h-full ${toneByLevel(view.level === 'stalled' ? 'missing' : 'unknown')}`}
                  style={{ width: view.pending > 0 ? '100%' : '0%' }}
                />
              )}
            </div>
          </DashCard>
          <DashCard
            title="Throughput"
            value={`${view.ratePerMin}`}
            rawValue={view.ratePerMin}
            unit="/min"
            caption={`5m ${view.doneLast5m} · 15m ${view.doneLast15m} · 60m ${view.doneLast60m}`}
          >
            <Meter
              fillPct={(view.doneLast15m / maxDone) * 100}
              toneClass={toneByLevel(view.doneLast15m > 0 ? 'ok' : 'unknown')}
              label={`done 15m ${view.doneLast15m}`}
            />
          </DashCard>
        </div>
      </div>

      {view.kinds.length > 0 ? (
        <div className="grid grid-cols-1 gap-x-5 gap-y-1 md:grid-cols-2">
          {view.kinds.map(k => {
            const selected = selectedKind === k.kind
            const kindTone =
              k.running > 0 ? 'scheduled' : k.pending > 0 ? 'ok' : 'unknown'
            const counts = kindQueueCountsLabel(k)
            const kindEta =
              k.etaMinutes != null ? formatDurationParts(k.etaMinutes * 60_000) : '—'
            return (
              <button
                key={k.kind}
                type="button"
                className={cn(
                  'min-w-0 rounded-sm border-0 bg-transparent p-0 text-left',
                  onSelectKind && 'cursor-pointer hover:bg-[var(--muted)]/40',
                  selected &&
                    'bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_14%,transparent)]',
                )}
                aria-pressed={selected}
                title={`${k.kind} · ${k.pending} waiting · ${k.running} running`}
                onClick={() => onSelectKind?.(k.kind)}
              >
                <CoverageBarRow
                  name={shortIngestKind(k.kind)}
                  nameTitle={k.kind}
                  fillPct={(k.active / maxKind) * 100}
                  toneClass={toneByLevel(kindTone)}
                  meterLabel={`${k.kind} ${counts.valueText}${counts.suffix != null ? ` ${counts.suffix}` : ''}`}
                  value={counts.value}
                  invert={k.pending > 0}
                  valueText={counts.valueText}
                  suffix={
                    <span className="font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
                      {counts.suffix ?? ''}
                      {k.pending > 0 ? ` · ${kindEta}` : ''}
                    </span>
                  }
                />
              </button>
            )
          })}
        </div>
      ) : (
        <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          No pending or running jobs
        </p>
      )}
    </div>
  )
}
