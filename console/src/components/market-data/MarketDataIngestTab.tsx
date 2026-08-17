import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  SegmentControl,
} from '@bifrost/ui'
import {
  enqueueIngestJob,
  fetchIngestJobs,
  fetchIngestKinds,
  fetchIngestQueueDashboard,
  fetchIngestQueueSummary,
  isProxyError,
  type IngestJob,
  type IngestQueueDashboardResponse,
  type IngestQueueKindCount,
  type IngestQueueSummaryResponse,
  type IngestScheduleSlot,
} from '@/api/marketDataPlugin'
import { MarketDataJsonProbeCard } from '@/components/market-data/MarketDataJsonProbeCard'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

function statusVariant(
  status: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  const s = status.toLowerCase()
  if (s === 'done' || s === 'success' || s === 'on_plan') return 'success'
  if (s === 'running' || s === 'pending' || s === 'due' || s === 'draining') return 'info'
  if (s === 'failed' || s === 'error' || s === 'missed') return 'danger'
  return 'neutral'
}

function formatResult(result: unknown): string {
  if (result == null) return '—'
  if (typeof result === 'string') return result.slice(0, 80)
  try {
    return JSON.stringify(result).slice(0, 80)
  } catch {
    return String(result)
  }
}

function formatAge(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  if (sec < 60) return `${Math.floor(sec)}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${Math.floor(sec % 60)}s`
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return `${h}h ${m}m`
}

function waitedLabel(createdAt: string | undefined, nowMs: number): string {
  if (!createdAt) return '—'
  const t = Date.parse(createdAt)
  if (!Number.isFinite(t)) return '—'
  return formatAge((nowMs - t) / 1000)
}

function payloadBrief(payload: Record<string, unknown> | undefined): string {
  if (payload == null) return '—'
  const parts: string[] = []
  for (const key of ['symbol', 'underlying', 'trade_date', 'from', 'to', 'option_ticker']) {
    if (payload[key] != null && String(payload[key]).trim() !== '') {
      parts.push(`${key}=${String(payload[key])}`)
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

function KindStackBars({ kinds }: { kinds: IngestQueueKindCount[] }) {
  const max = Math.max(1, ...kinds.map(k => k.active))
  if (kinds.length === 0) {
    return (
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        No pending or running jobs
      </p>
    )
  }
  return (
    <div className="flex flex-col gap-1.5">
      {kinds.map(k => {
        const pendingPct = (k.pending / max) * 100
        const runningPct = (k.running / max) * 100
        return (
          <div key={k.kind} className="flex items-center gap-2">
            <span className="w-40 shrink-0 truncate font-mono text-[var(--text-dense-caption)]">
              {k.kind}
            </span>
            <div className="flex h-2 min-w-0 flex-1 overflow-hidden rounded-sm bg-[var(--muted)]">
              <div
                className="h-full bg-[var(--color-info,theme(colors.sky.500))]"
                style={{ width: `${pendingPct}%` }}
                title={`pending ${k.pending}`}
              />
              <div
                className="h-full bg-[var(--color-warning,theme(colors.amber.500))]"
                style={{ width: `${runningPct}%` }}
                title={`running ${k.running}`}
              />
            </div>
            <span className="w-20 shrink-0 text-right font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              p{k.pending} r{k.running}
            </span>
          </div>
        )
      })}
      <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        Bar = relative depth · blue pending (ready) · amber running
      </p>
    </div>
  )
}

export function MarketDataIngestTab() {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [kind, setKind] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [payloadText, setPayloadText] = useState('{}')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)
  const nowMs = Date.now()

  const kindsQ = useQuery({
    queryKey: ['market-data', 'ingest', 'kinds'],
    queryFn: fetchIngestKinds,
    refetchInterval: 120_000,
    retry: 1,
  })
  const dashQ = useQuery({
    queryKey: ['market-data', 'ingest', 'queue-dashboard'],
    queryFn: () => fetchIngestQueueDashboard({ grace_minutes: 45 }),
    refetchInterval: 15_000,
    retry: 1,
  })
  const summaryQ = useQuery({
    queryKey: ['market-data', 'ingest', 'queue-summary'],
    queryFn: fetchIngestQueueSummary,
    refetchInterval: 15_000,
    retry: 1,
    // Fallback when dashboard API not yet deployed
    enabled: dashQ.isError || (dashQ.data != null && isProxyError(dashQ.data)),
  })
  const jobsQ = useQuery({
    queryKey: ['market-data', 'ingest', 'jobs', statusFilter],
    queryFn: () =>
      fetchIngestJobs({
        limit: 40,
        status: statusFilter === 'all' ? undefined : statusFilter,
      }),
    refetchInterval: 15_000,
    retry: 1,
  })

  const kindsRaw = kindsQ.data
  const kindsErr = kindsRaw != null && isProxyError(kindsRaw) ? kindsRaw.error : null
  const kinds = useMemo(
    () => (kindsRaw != null && !isProxyError(kindsRaw) ? (kindsRaw.kinds ?? []) : []),
    [kindsRaw],
  )

  const jobsRaw = jobsQ.data
  const jobsErr = jobsRaw != null && isProxyError(jobsRaw) ? jobsRaw.error : null
  const jobs: IngestJob[] =
    jobsRaw != null && !isProxyError(jobsRaw) ? (jobsRaw.jobs ?? []) : []

  const dashRaw = dashQ.data
  const dashErr = dashRaw != null && isProxyError(dashRaw) ? dashRaw.error : null
  const dash: IngestQueueDashboardResponse | null =
    dashRaw != null && !isProxyError(dashRaw) ? dashRaw : null

  const summaryRaw = summaryQ.data
  const summary: IngestQueueSummaryResponse | null =
    summaryRaw != null && !isProxyError(summaryRaw) ? summaryRaw : null

  const kindRollup: IngestQueueKindCount[] =
    dash?.queue?.kinds ?? summary?.kinds ?? []
  const readyNow = dash?.queue?.ready_now ?? summary?.pending ?? 0
  const running = dash?.queue?.running ?? summary?.running ?? 0
  const scheduledFuture = dash?.queue?.scheduled_future ?? 0
  const queueVerdict = dash?.queue?.verdict ?? (readyNow + running > 0 ? 'draining' : 'idle')

  const selectedKind = useMemo(() => {
    if (kind !== '') return kind
    return kinds[0] ?? ''
  }, [kind, kinds])

  const scheduleSlots: IngestScheduleSlot[] = dash?.schedule?.slots ?? []

  const runEnqueue = async () => {
    setActing(true)
    setActionMsg(null)
    setActionFailed(false)
    try {
      let payload: Record<string, unknown> = {}
      try {
        const parsed = JSON.parse(payloadText || '{}') as unknown
        if (parsed != null && typeof parsed === 'object' && !Array.isArray(parsed)) {
          payload = parsed as Record<string, unknown>
        } else {
          throw new Error('Payload must be a JSON object')
        }
      } catch (e) {
        setActionFailed(true)
        setActionMsg(e instanceof Error ? e.message : 'Invalid JSON payload')
        return
      }
      const res = await enqueueIngestJob({ kind: selectedKind, payload })
      if (isProxyError(res) || res.ok === false) {
        setActionFailed(true)
        setActionMsg(isProxyError(res) ? res.error : (res.error ?? 'Enqueue failed'))
        return
      }
      setActionMsg(
        res.deduplicated
          ? `Deduped · existing job ${res.job_id ?? '—'}`
          : `Enqueued · job ${res.job_id ?? '—'}`,
      )
      void queryClient.invalidateQueries({ queryKey: ['market-data', 'ingest'] })
    } catch (e) {
      setActionFailed(true)
      setActionMsg(e instanceof Error ? e.message : 'Enqueue failed')
    } finally {
      setActing(false)
      setConfirmOpen(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <OpsSection
        title="Queue dashboard"
        description="Ready queue vs Cron plan — GET /market/ingest/queue-dashboard"
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        {dashQ.isLoading && dash == null && summary == null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading queue…
          </p>
        ) : dashErr != null && summary == null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{dashErr}</p>
        ) : (
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center gap-2">
              <DenseTag variant={statusVariant(queueVerdict)}>{queueVerdict}</DenseTag>
              <DenseTag variant="info">ready {readyNow}</DenseTag>
              <DenseTag variant="warning">running {running}</DenseTag>
              <DenseTag variant="neutral">scheduled(future) {scheduledFuture}</DenseTag>
              {dash?.throughput != null ? (
                <>
                  <DenseTag variant="success">
                    ~{dash.throughput.jobs_per_min_15m ?? 0}/min (15m)
                  </DenseTag>
                  <DenseTag variant="neutral">
                    done 15m {dash.throughput.done_last_15m ?? 0}
                  </DenseTag>
                  {dash.throughput.eta_minutes_at_current_rate != null ? (
                    <DenseTag variant="info">
                      ETA ~{dash.throughput.eta_minutes_at_current_rate}m
                    </DenseTag>
                  ) : null}
                </>
              ) : null}
              {dash?.queue?.oldest_pending_age_sec != null ? (
                <DenseTag variant="neutral">
                  oldest wait {formatAge(dash.queue.oldest_pending_age_sec)}
                </DenseTag>
              ) : null}
            </div>
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {dash?.model?.scheduled_future_jobs ??
                'CronJobs enqueue at fire time; job_ingest has no future-dated rows. Ready = pending waiting for workers.'}
            </p>
            <KindStackBars kinds={kindRollup} />
          </div>
        )}
      </OpsSection>

      <OpsSection
        title="Schedule plan & adherence"
        description="Future Cron fires + last fire vs jobs/freshness (plan vs actual)"
        bodyPadding="none"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
        actions={
          dash?.schedule != null ? (
            <div className="flex flex-wrap items-center gap-2">
              <DenseTag variant={statusVariant(dash.schedule.verdict ?? '')}>
                {dash.schedule.verdict ?? '—'}
              </DenseTag>
              <DenseTag variant="success">on_plan {dash.schedule.on_plan ?? 0}</DenseTag>
              <DenseTag variant="info">due {dash.schedule.due ?? 0}</DenseTag>
              <DenseTag variant="danger">missed {dash.schedule.missed ?? 0}</DenseTag>
              <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                grace {dash.schedule.grace_minutes ?? 45}m
              </span>
            </div>
          ) : null
        }
      >
        {dash == null ? (
          <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {dashErr != null
              ? `Schedule dashboard needs Plugin ≥0.4.3 (${dashErr})`
              : 'Loading schedule…'}
          </p>
        ) : scheduleSlots.length === 0 ? (
          <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No slots in schedule.yaml
          </p>
        ) : (
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Slot</DenseTableHead>
                <DenseTableHead>Cron (UTC)</DenseTableHead>
                <DenseTableHead>Adherence</DenseTableHead>
                <DenseTableHead>Last fire</DenseTableHead>
                <DenseTableHead>Next fires</DenseTableHead>
                <DenseTableHead>Evidence</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {scheduleSlots.map(s => (
                <DenseTableRow key={s.slot}>
                  <DenseTableCell>
                    <div className="flex flex-col gap-0.5">
                      <span className="font-mono text-xs">{s.slot}</span>
                      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                        {s.note ?? ''}
                        {s.inline ? ' · inline' : ''}
                      </span>
                    </div>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-xs">{s.cron}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={statusVariant(s.adherence ?? '')}>
                      {s.adherence ?? '—'}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                    {s.last_fire ?? '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                    {(s.next_fires ?? []).slice(0, 2).join(' · ') || '—'}
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {s.detail ?? '—'}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        )}
      </OpsSection>

      <OpsSection
        title="Job queue"
        description="Plugin GET /market/ingest/jobs — ready/running history rows"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
              Status
            </span>
            <SegmentControl
              size="sm"
              ariaLabel="Ingest job status filter"
              value={statusFilter}
              onChange={setStatusFilter}
              options={[
                { value: 'all', label: 'All' },
                { value: 'pending', label: 'Pending' },
                { value: 'running', label: 'Running' },
                { value: 'done', label: 'Done' },
                { value: 'failed', label: 'Failed' },
              ]}
            />
            <Button
              variant="outline"
              size="sm"
              disabled={jobsQ.isFetching}
              onClick={() => {
                void jobsQ.refetch()
                void dashQ.refetch()
                void summaryQ.refetch()
              }}
            >
              Refresh
            </Button>
          </div>
        }
        bodyPadding="none"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        {jobsQ.isLoading ? (
          <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading jobs…
          </p>
        ) : jobsErr != null ? (
          <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--destructive)]">
            {jobsErr}
          </p>
        ) : jobs.length === 0 ? (
          <p className="m-0 px-3 py-4 text-center text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No ingest jobs
          </p>
        ) : (
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>ID</DenseTableHead>
                <DenseTableHead>Kind</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Waited</DenseTableHead>
                <DenseTableHead>Payload</DenseTableHead>
                <DenseTableHead>Created</DenseTableHead>
                <DenseTableHead>Result</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {jobs.map(j => (
                <DenseTableRow key={String(j.job_id ?? j.id)}>
                  <DenseTableCell className="font-mono text-xs">
                    {j.job_id ?? j.id ?? '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-xs">{j.kind}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={statusVariant(j.status)}>{j.status}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-xs">
                    {j.status === 'pending' || j.status === 'running'
                      ? waitedLabel(j.created_at, nowMs)
                      : j.started_at && j.created_at
                        ? waitedLabel(j.created_at, Date.parse(j.started_at))
                        : '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {payloadBrief(j.payload)}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                    {j.created_at ?? '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {formatResult(j.result)}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        )}
      </OpsSection>

      <OpsSection
        title="Enqueue job"
        description="POST /market/ingest/enqueue — writes data_ops.job_ingest (operator auth)"
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed
      >
        {kindsErr != null ? (
          <p className="m-0 mb-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
            {kindsErr}
          </p>
        ) : null}
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex min-w-[12rem] flex-col gap-1">
            <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
              Kind
            </span>
            <select
              className="h-8 rounded-md border border-[var(--border)] bg-[var(--background)] px-2 text-[var(--text-dense-meta)]"
              value={selectedKind}
              onChange={e => setKind(e.target.value)}
              disabled={kinds.length === 0}
            >
              {kinds.length === 0 ? <option value="">No kinds</option> : null}
              {kinds.map(k => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
          </label>
          <label className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
              Payload (JSON)
            </span>
            <textarea
              className="min-h-[4.5rem] rounded-md border border-[var(--border)] bg-[var(--background)] px-2 py-1 font-mono text-[var(--text-dense-caption)]"
              value={payloadText}
              onChange={e => setPayloadText(e.target.value)}
            />
          </label>
          <Button
            size="sm"
            disabled={!canOperate || acting || !selectedKind}
            onClick={() => setConfirmOpen(true)}
            title={canOperate ? undefined : 'Operator auth required'}
          >
            Enqueue
          </Button>
        </div>
        {!canOperate ? (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Authenticate as operator to enqueue jobs.
          </p>
        ) : null}
        {actionMsg != null ? (
          <p
            className={`m-0 mt-2 text-[var(--text-dense-meta)] ${
              actionFailed ? 'text-[var(--destructive)]' : 'text-[var(--success)]'
            }`}
          >
            {actionMsg}
          </p>
        ) : null}
      </OpsSection>

      <MarketDataJsonProbeCard
        title="JSON Probe"
        description="Inspect ingest kinds / queue-dashboard / a single job"
        defaultPath="/market/ingest/queue-dashboard"
      />

      <ConfirmDialog
        open={confirmOpen}
        title="Enqueue ingest job"
        message={`Enqueue kind "${selectedKind}" into data_ops.job_ingest? Workers will pick it up asynchronously.`}
        confirmLabel="Confirm enqueue"
        confirming={acting}
        onConfirm={() => void runEnqueue()}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  )
}
