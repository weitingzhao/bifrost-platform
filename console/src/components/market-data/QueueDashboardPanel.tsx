import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  cn,
} from '@bifrost/ui'
import {
  fetchIngestJobs,
  isProxyError,
  type IngestJob,
  type IngestQueueDashboardResponse,
  type IngestQueueKindCount,
  type IngestQueueSummaryResponse,
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
import {
  formatReadyCheckCaption,
  formatSignedDelta,
  readyCheckDelta,
  shiftReadyCheck,
  type ReadyCheckHistory,
} from '@/components/market-data/queueReadyCheck'
import {
  formatDurationSec,
  freshnessLabel,
  freshnessTagVariant,
  kindQueueCountsLabel,
  runningAgeSec,
  runningCardCaption,
  runningFreshness,
  runningJobsSummary,
  sortRunningJobs,
} from '@/components/market-data/queueRunningJobs'

const READY_CHECK_STORE_KEY = 'bifrost.market-data.queue-ready-check.v1'

function readStoredReadyCheck(): ReadyCheckHistory {
  try {
    const raw = sessionStorage.getItem(READY_CHECK_STORE_KEY)
    if (raw == null || raw === '') return { previous: null, current: null }
    const parsed = JSON.parse(raw) as ReadyCheckHistory
    if (parsed != null && typeof parsed === 'object') return parsed
  } catch {
    /* ignore */
  }
  return { previous: null, current: null }
}

function writeStoredReadyCheck(hist: ReadyCheckHistory): void {
  try {
    sessionStorage.setItem(READY_CHECK_STORE_KEY, JSON.stringify(hist))
  } catch {
    /* ignore quota */
  }
}

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

const RUNNING_JOB_LIMIT = 200

function payloadBrief(payload: Record<string, unknown> | undefined): string {
  if (payload == null) return '—'
  const parts: string[] = []
  for (const key of ['symbol', 'underlying', 'trade_date', 'from', 'to', 'option_ticker']) {
    if (payload[key] != null && String(payload[key]).trim() !== '') {
      parts.push(String(payload[key]))
    }
  }
  if (parts.length === 0) {
    try {
      return JSON.stringify(payload).slice(0, 48)
    } catch {
      return '—'
    }
  }
  return parts.join(' ')
}

function KindBars({
  kinds,
  selectedKind,
  onSelectKind,
}: {
  kinds: IngestQueueKindCount[]
  selectedKind: string
  onSelectKind: (kind: string) => void
}) {
  const max = Math.max(1, ...kinds.map(k => k.active))
  if (kinds.length === 0) {
    return (
      <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        No ready or running jobs
      </p>
    )
  }
  return (
    <div className="grid grid-cols-1 gap-x-5 gap-y-1 md:grid-cols-2">
      {kinds.map(k => {
        const selected = selectedKind === k.kind
        const tone = k.running > 0 ? 'scheduled' : k.pending > 0 ? 'ok' : 'unknown'
        const counts = kindQueueCountsLabel(k)
        return (
          <button
            key={k.kind}
            type="button"
            className={cn(
              'min-w-0 rounded-sm border-0 bg-transparent p-0 text-left',
              'cursor-pointer hover:bg-[var(--muted)]/40',
              selected &&
                'bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_14%,transparent)]',
            )}
            aria-pressed={selected}
            title={`${k.kind} · ${k.pending} waiting for a worker · ${k.running} claimed and running. Click to inspect.`}
            onClick={() => onSelectKind(k.kind)}
          >
            <CoverageBarRow
              name={shortIngestKind(k.kind)}
              nameTitle={k.kind}
              fillPct={(k.active / max) * 100}
              toneClass={toneByLevel(tone)}
              meterLabel={`${k.kind} ${counts.valueText}${counts.suffix != null ? ` ${counts.suffix}` : ''}`}
              value={counts.value}
              invert={k.pending > 0}
              valueText={counts.valueText}
              suffix={
                counts.suffix != null ? (
                  <span className="font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
                    {counts.suffix}
                  </span>
                ) : null
              }
            />
          </button>
        )
      })}
    </div>
  )
}

function RunningJobsTable({
  jobs,
  nowMs,
  loading,
  error,
}: {
  jobs: IngestJob[]
  nowMs: number
  loading: boolean
  error: string | null
}) {
  if (loading && jobs.length === 0) {
    return (
      <p className="m-0 px-1 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Loading running jobs…
      </p>
    )
  }
  if (error != null && jobs.length === 0) {
    return (
      <p className="m-0 px-1 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</p>
    )
  }
  if (jobs.length === 0) {
    return (
      <p className="m-0 px-1 py-2 text-center text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        No running jobs in this filter
      </p>
    )
  }
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Job</DenseTableHead>
          <DenseTableHead>Kind</DenseTableHead>
          <DenseTableHead>Target</DenseTableHead>
          <DenseTableHead>Running</DenseTableHead>
          <DenseTableHead>Pulse</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {jobs.map(j => {
          const age = runningAgeSec(j.started_at, nowMs)
          const tone = runningFreshness(age)
          return (
            <DenseTableRow key={String(j.job_id ?? j.id)}>
              <DenseTableCell className="font-mono text-xs">
                {j.job_id ?? j.id ?? '—'}
              </DenseTableCell>
              <DenseTableCell className="font-mono text-xs" title={j.kind}>
                {shortIngestKind(j.kind)}
              </DenseTableCell>
              <DenseTableCell className="font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                {payloadBrief(j.payload)}
              </DenseTableCell>
              <DenseTableCell className="font-mono text-xs tabular-nums">
                {formatDurationSec(age)}
              </DenseTableCell>
              <DenseTableCell>
                <DenseTag variant={freshnessTagVariant(tone)}>{freshnessLabel(tone)}</DenseTag>
              </DenseTableCell>
            </DenseTableRow>
          )
        })}
      </DenseTableBody>
    </DenseDataTable>
  )
}

export function QueueDashboardPanel({
  dash,
  summary,
  loading,
  error,
  checkedAtMs,
  nowMs,
  onOpenJobQueue,
}: {
  dash: IngestQueueDashboardResponse | null
  summary: IngestQueueSummaryResponse | null
  loading: boolean
  error: string | null
  /** React Query dataUpdatedAt — used when API generated_at is missing. */
  checkedAtMs?: number
  nowMs?: number
  onOpenJobQueue?: (args: { kind: string; status: 'running' }) => void
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
  const checkAtMs = (() => {
    const iso = dash?.generated_at ?? summary?.generated_at
    if (iso != null && iso !== '') {
      const parsed = Date.parse(iso)
      if (Number.isFinite(parsed)) return parsed
    }
    if (checkedAtMs != null && Number.isFinite(checkedAtMs) && checkedAtMs > 0) {
      return checkedAtMs
    }
    return null
  })()
  const [readyHist, setReadyHist] = useState<ReadyCheckHistory>(readStoredReadyCheck)
  useEffect(() => {
    if (loading || error != null) return
    const atMs = checkAtMs ?? Date.now()
    setReadyHist(prev => {
      const next = shiftReadyCheck(prev, { count: readyNow, atMs })
      if (next !== prev) writeStoredReadyCheck(next)
      return next
    })
  }, [checkAtMs, error, loading, readyNow])
  const readyDelta = readyCheckDelta(readyHist)
  const readyDeltaTag =
    readyDelta == null
      ? null
      : formatSignedDelta(readyDelta)
  const readyDeltaVariant =
    readyDelta == null ? 'neutral' : readyDelta < 0 ? 'success' : readyDelta > 0 ? 'warning' : 'neutral'
  const oldestLabel = oldest != null ? `oldest ${formatAge(oldest)}` : null
  const readyCaption = formatReadyCheckCaption({
    hist: readyHist,
    nowMs: Date.now(),
    oldestLabel,
  })
  const readyCaptionTitle =
    readyHist.previous != null
      ? `Previous check ${new Date(readyHist.previous.atMs).toLocaleString()} · ${readyCaption}`
      : readyCaption
  const clockMs = nowMs != null && Number.isFinite(nowMs) ? nowMs : Date.now()
  const [inspectKind, setInspectKind] = useState('')
  const [inspectOpen, setInspectOpen] = useState(false)
  const runningJobsQ = useQuery({
    queryKey: ['market-data', 'ingest', 'jobs', 'running'],
    queryFn: () =>
      fetchIngestJobs({
        limit: RUNNING_JOB_LIMIT,
        status: 'running',
      }),
    refetchInterval: 15_000,
    retry: 1,
    enabled: !loading && error == null && (running > 0 || inspectOpen),
  })
  const runningJobsRaw = runningJobsQ.data
  const runningJobsErr =
    runningJobsRaw != null && isProxyError(runningJobsRaw) ? runningJobsRaw.error : null
  const runningJobs: IngestJob[] = useMemo(() => {
    const rows =
      runningJobsRaw != null && !isProxyError(runningJobsRaw) ? (runningJobsRaw.jobs ?? []) : []
    return sortRunningJobs(rows, clockMs)
  }, [clockMs, runningJobsRaw])
  const inspectJobs =
    inspectKind !== '' ? runningJobs.filter(j => j.kind === inspectKind) : runningJobs
  const runSummary = runningJobsSummary(runningJobs, clockMs)
  const inspectSummary = runningJobsSummary(inspectJobs, clockMs)
  const inFlight = readyNow === 0 && running > 0
  const etaValue =
    eta != null ? `${eta}m` : idle ? '—' : inFlight ? 'in flight' : 'stalled'
  const etaCaption = idle
    ? 'queue empty'
    : inFlight
      ? 'ready empty · workers busy'
      : 'at last-15m rate'
  const toggleInspect = (kind: string) => {
    if (inspectOpen && inspectKind === kind) {
      setInspectOpen(false)
      setInspectKind('')
      return
    }
    setInspectKind(kind)
    setInspectOpen(true)
  }

  return (
    <OpsSection
      title="Queue dashboard"
      description="Ready = waiting for a worker. Running = claimed and in flight. Click a kind or Running to inspect jobs."
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
                tag={readyDeltaTag ?? undefined}
                tagVariant={readyDeltaVariant}
                value={fmtCount(readyNow)}
                rawValue={readyNow}
                invertFlash={readyNow > 0}
                unit="jobs"
                caption={readyCaption}
                captionTitle={readyCaptionTitle}
              >
                <Meter
                  fillPct={(readyNow / pressureCap) * 100}
                  toneClass={toneByLevel(readyNow > 0 ? 'scheduled' : 'ok')}
                />
              </DashCard>
              <DashCard
                title="Running"
                tag={
                  runSummary.stale > 0
                    ? `${runSummary.stale} stuck?`
                    : runSummary.long > 0
                      ? `${runSummary.long} long`
                      : running > 0
                        ? 'in flight'
                        : undefined
                }
                tagVariant={
                  runSummary.stale > 0
                    ? 'danger'
                    : runSummary.long > 0
                      ? 'warning'
                      : running > 0
                        ? 'success'
                        : 'neutral'
                }
                value={fmtCount(running)}
                rawValue={running}
                unit="jobs"
                caption={
                  running > 0
                    ? runningJobsQ.isLoading && runSummary.count === 0
                      ? 'checking how long they have been running…'
                      : runningCardCaption(runSummary)
                    : `${fmtCount(scheduledFuture)} not yet enqueued`
                }
                onClick={running > 0 ? () => toggleInspect('') : undefined}
              >
                <Meter
                  fillPct={(running / pressureCap) * 100}
                  toneClass={toneByLevel(
                    runSummary.stale > 0 ? 'missing' : running > 0 ? 'ok' : 'unknown',
                  )}
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
                value={etaValue}
                rawValue={eta}
                invertFlash={eta != null && eta >= 15}
                caption={etaCaption}
              >
                <Meter
                  fillPct={eta != null ? Math.min(100, (eta / 60) * 100) : idle || inFlight ? 0 : 100}
                  toneClass={toneByLevel(
                    idle || inFlight
                      ? 'ok'
                      : eta == null
                        ? 'missing'
                        : eta >= 15
                          ? 'scheduled'
                          : 'ok',
                  )}
                />
              </DashCard>
            </div>
          </div>
          <KindBars
            kinds={kinds}
            selectedKind={inspectKind}
            onSelectKind={toggleInspect}
          />
          {inspectOpen ? (
            <div className="border-t border-[var(--border)] pt-1.5">
              <div className="mb-1 flex flex-wrap items-center gap-2">
                <span className="text-[var(--text-dense-caption)] font-medium">
                  {inspectKind !== '' ? shortIngestKind(inspectKind) : 'All kinds'}
                </span>
                <DenseTag variant="info">{inspectSummary.count} running</DenseTag>
                {inspectSummary.oldestSec != null ? (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    oldest {formatDurationSec(inspectSummary.oldestSec)}
                  </span>
                ) : null}
                {inspectSummary.stale > 0 ? (
                  <DenseTag variant="danger">{inspectSummary.stale} stuck?</DenseTag>
                ) : null}
                <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
                  Pulse uses started_at only — workers do not heartbeat while a job runs.
                </span>
                <div className="ml-auto flex items-center gap-1">
                  {onOpenJobQueue != null ? (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => onOpenJobQueue({ kind: inspectKind, status: 'running' })}
                    >
                      Job queue
                    </Button>
                  ) : null}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setInspectOpen(false)
                      setInspectKind('')
                    }}
                  >
                    Close
                  </Button>
                </div>
              </div>
              <RunningJobsTable
                jobs={inspectJobs}
                nowMs={clockMs}
                loading={runningJobsQ.isLoading}
                error={runningJobsErr}
              />
            </div>
          ) : null}
        </div>
      )}
    </OpsSection>
  )
}
