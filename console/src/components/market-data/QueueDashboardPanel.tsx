import { DenseTag } from '@bifrost/ui'
import type {
  IngestQueueDashboardResponse,
  IngestQueueKindCount,
  IngestQueueSummaryResponse,
} from '@/api/marketDataPlugin'
import {
  CoverageBarRow,
  DashCard,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { fmtCount, toneByLevel } from '@/components/market-data/overviewDashModel'
import { shortIngestKind } from '@/components/market-data/ingestKindLabel'
import { OpsSection } from '@/components/layout/OpsSection'

function formatAge(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  if (sec < 60) return `${Math.floor(sec)}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m`
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

function verdictTone(
  verdict: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  const s = verdict.toLowerCase()
  if (s === 'idle' || s === 'on_plan') return 'success'
  if (s === 'draining' || s === 'due' || s === 'running') return 'info'
  if (s === 'missed' || s === 'stalled' || s === 'failed') return 'danger'
  return 'neutral'
}

function KindBars({ kinds }: { kinds: IngestQueueKindCount[] }) {
  const max = Math.max(1, ...kinds.map(k => k.active))
  if (kinds.length === 0) {
    return (
      <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        No pending or running jobs
      </p>
    )
  }
  return (
    <div className="grid grid-cols-1 gap-x-5 gap-y-1 md:grid-cols-2 xl:grid-cols-3">
      {kinds.map(k => {
        const kind =
          k.running > 0 ? 'scheduled' : k.pending > 0 ? 'ok' : 'unknown'
        return (
          <CoverageBarRow
            key={k.kind}
            name={shortIngestKind(k.kind)}
            nameTitle={`${k.kind} · pending ${k.pending} · running ${k.running}`}
            fillPct={(k.active / max) * 100}
            toneClass={toneByLevel(kind)}
            meterLabel={`${k.kind} pending ${k.pending} running ${k.running}`}
            value={k.pending}
            invert={k.pending > 0}
            valueText={fmtCount(k.pending)}
            suffix={
              <span className="font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
                r{k.running}
              </span>
            }
          />
        )
      })}
    </div>
  )
}

export function QueueDashboardPanel({
  dash,
  summary,
  loading,
  error,
}: {
  dash: IngestQueueDashboardResponse | null
  summary: IngestQueueSummaryResponse | null
  loading: boolean
  error: string | null
}) {
  const kinds = dash?.queue?.kinds ?? summary?.kinds ?? []
  const readyNow = dash?.queue?.ready_now ?? summary?.pending ?? 0
  const running = dash?.queue?.running ?? summary?.running ?? 0
  const scheduledFuture = dash?.queue?.scheduled_future ?? 0
  const failed15m = dash?.throughput?.failed_last_15m ?? 0
  const done15m = dash?.throughput?.done_last_15m ?? 0
  const rate = dash?.throughput?.jobs_per_min_15m ?? 0
  const eta = dash?.throughput?.eta_minutes_at_current_rate
  const oldest = dash?.queue?.oldest_pending_age_sec
  const verdict = dash?.queue?.verdict ?? (readyNow + running > 0 ? 'draining' : 'idle')
  const idle = readyNow + running + failed15m === 0
  const ringTotal = idle ? 1 : running + readyNow + failed15m
  const pressureCap = Math.max(readyNow + running, 1)

  return (
    <OpsSection
      title="Queue dashboard"
      description="Ready vs workers. Bar = kind depth vs the busiest kind."
      headerExtra={<DenseTag variant={verdictTone(verdict)}>{verdict}</DenseTag>}
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {loading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading queue…
        </p>
      ) : error != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</p>
      ) : (
        <div className="flex flex-col gap-1.5">
          <div className="flex items-stretch gap-2">
            <ScoreRing
              ready={idle ? 1 : running}
              thin={idle ? 0 : readyNow}
              blocked={failed15m}
              total={Math.max(ringTotal, 1)}
              caption={idle ? 'idle' : 'run'}
            />
            <div className="grid min-w-0 flex-1 grid-cols-2 gap-1.5 xl:grid-cols-4">
              <DashCard
                title="Ready"
                value={fmtCount(readyNow)}
                rawValue={readyNow}
                invertFlash={readyNow > 0}
                unit="jobs"
                caption={oldest != null ? `oldest ${formatAge(oldest)}` : 'waiting workers'}
              >
                <Meter
                  fillPct={(readyNow / pressureCap) * 100}
                  toneClass={toneByLevel(readyNow > 0 ? 'scheduled' : 'ok')}
                />
              </DashCard>
              <DashCard
                title="Running"
                value={fmtCount(running)}
                rawValue={running}
                unit="jobs"
                caption={`${fmtCount(scheduledFuture)} scheduled`}
              >
                <Meter
                  fillPct={(running / pressureCap) * 100}
                  toneClass={toneByLevel(running > 0 ? 'ok' : 'unknown')}
                />
              </DashCard>
              <DashCard
                title="Rate"
                value={`${rate}`}
                rawValue={rate}
                unit="/min"
                caption={`${fmtCount(done15m)} done · ${fmtCount(failed15m)} fail · 15m`}
              >
                <Meter
                  fillPct={Math.min(100, rate * 10)}
                  toneClass={toneByLevel(failed15m > 0 ? 'missing' : rate > 0 ? 'ok' : 'unknown')}
                />
              </DashCard>
              <DashCard
                title="ETA"
                value={eta != null ? `${eta}m` : idle ? '—' : 'stalled'}
                rawValue={eta}
                invertFlash={eta != null && eta >= 15}
                caption={idle ? 'queue empty' : 'at last-15m rate'}
              >
                <Meter
                  fillPct={eta != null ? Math.min(100, (eta / 60) * 100) : idle ? 0 : 100}
                  toneClass={toneByLevel(
                    idle ? 'ok' : eta == null ? 'missing' : eta >= 15 ? 'scheduled' : 'ok',
                  )}
                />
              </DashCard>
            </div>
          </div>
          <KindBars kinds={kinds} />
        </div>
      )}
    </OpsSection>
  )
}
