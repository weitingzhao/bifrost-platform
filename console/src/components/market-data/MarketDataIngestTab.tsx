import { useCallback, useEffect, useMemo, useState } from 'react'
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
  cn,
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
import { JobQueuePressure } from '@/components/market-data/JobQueuePressure'
import { QueueDashboardPanel } from '@/components/market-data/QueueDashboardPanel'
import { ScheduleSwimlane } from '@/components/market-data/ScheduleSwimlane'
import { ScoreRing } from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import {
  filterScheduleSlots,
  scheduleLaneId,
  scheduleRowId,
  toggleSlotSelection,
  type ScheduleAdherenceFilter,
} from '@/components/market-data/scheduleSwimlaneModel'
import {
  formatDurationSec,
  freshnessLabel,
  freshnessTagVariant,
  runningAgeSec,
  runningFreshness,
} from '@/components/market-data/queueRunningJobs'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { describeCronSchedule } from '@/lib/patrol/cronSchedule'

type IngestDetailTab = 'schedule' | 'jobs' | 'enqueue'

const JOB_PAGE_LIMIT = 40

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

function jobPageShowingLabel(args: {
  loading: boolean
  shown: number
  statusFilter: string
  readyNow: number
  running: number
  done15m?: number
  failed15m?: number
}): string {
  if (args.loading) return 'showing …'
  const { shown, statusFilter, readyNow, running } = args
  if (statusFilter === 'pending') return `showing ${shown} of ${readyNow}`
  if (statusFilter === 'running') return `showing ${shown} of ${running}`
  if (statusFilter === 'done' && args.done15m != null) {
    return `showing latest ${shown} · done 15m ${args.done15m}`
  }
  if (statusFilter === 'failed' && args.failed15m != null) {
    return `showing latest ${shown} · failed 15m ${args.failed15m}`
  }
  return `showing latest ${shown}`
}

function slotAdherenceKind(
  adherence: string | undefined,
): 'ok' | 'scheduled' | 'missing' | 'unknown' {
  const a = (adherence ?? '').toLowerCase()
  if (a === 'on_plan') return 'ok'
  if (a === 'due' || a === 'draining') return 'scheduled'
  if (a === 'missed') return 'missing'
  return 'unknown'
}

export function MarketDataIngestTab() {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [kind, setKind] = useState('')
  const [detailTab, setDetailTab] = useState<IngestDetailTab>('schedule')
  const [statusFilter, setStatusFilter] = useState('all')
  const [adherenceFilter, setAdherenceFilter] = useState<ScheduleAdherenceFilter>('all')
  const [jobKindFilter, setJobKindFilter] = useState('')
  const [selectedSlot, setSelectedSlot] = useState<string | null>(
    () => new URLSearchParams(window.location.search).get('slot'),
  )
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
    queryKey: ['market-data', 'ingest', 'jobs', statusFilter, jobKindFilter],
    queryFn: () =>
      fetchIngestJobs({
        limit: JOB_PAGE_LIMIT,
        status: statusFilter === 'all' ? undefined : statusFilter,
        kind: jobKindFilter !== '' ? jobKindFilter : undefined,
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

  const selectedKind = useMemo(() => {
    if (kind !== '') return kind
    return kinds[0] ?? ''
  }, [kind, kinds])

  const scheduleSlots: IngestScheduleSlot[] = useMemo(
    () => dash?.schedule?.slots ?? [],
    [dash?.schedule?.slots],
  )
  const visibleScheduleSlots = useMemo(
    () => filterScheduleSlots(scheduleSlots, adherenceFilter),
    [scheduleSlots, adherenceFilter],
  )
  const done15m = dash?.throughput?.done_last_15m
  const failed15m = dash?.throughput?.failed_last_15m
  const jobsShowing = jobPageShowingLabel({
    loading: jobsQ.isLoading,
    shown: jobs.length,
    statusFilter,
    readyNow,
    running,
    done15m,
    failed15m,
  })
  const selectedSlotRow = useMemo(
    () => visibleScheduleSlots.find(s => s.slot === selectedSlot) ?? null,
    [visibleScheduleSlots, selectedSlot],
  )

  useEffect(() => {
    const url = new URL(window.location.href)
    if (selectedSlot) url.searchParams.set('slot', selectedSlot)
    else url.searchParams.delete('slot')
    window.history.replaceState({}, '', url.toString())
  }, [selectedSlot])

  useEffect(() => {
    if (selectedSlot == null || scheduleSlots.length === 0) return
    if (!visibleScheduleSlots.some(s => s.slot === selectedSlot)) setSelectedSlot(null)
  }, [scheduleSlots.length, visibleScheduleSlots, selectedSlot])

  const selectScheduleSlot = useCallback((slot: string, from: 'lane' | 'table') => {
    setSelectedSlot(prev => {
      const next = toggleSlotSelection(prev, slot)
      if (next != null) {
        const targetId = from === 'lane' ? scheduleRowId(next) : scheduleLaneId(next)
        queueMicrotask(() => {
          document.getElementById(targetId)?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
        })
      }
      return next
    })
  }, [])

  const openSlotJobs = useCallback((slot: IngestScheduleSlot) => {
    const kindsForSlot = slot.evidence_kinds ?? []
    setJobKindFilter(kindsForSlot[0] ?? '')
    setDetailTab('jobs')
  }, [])

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
    <div className="flex flex-col gap-2">
      <QueueDashboardPanel
        dash={dash}
        summary={summary}
        loading={dashQ.isLoading && dash == null && summary == null}
        error={dashErr != null && summary == null ? dashErr : null}
        checkedAtMs={dashQ.dataUpdatedAt || summaryQ.dataUpdatedAt}
        nowMs={nowMs}
        onOpenJobQueue={({ kind, status }) => {
          setJobKindFilter(kind)
          setStatusFilter(status)
          setDetailTab('jobs')
        }}
      />

      {/* ── Detail secondary tabs ── */}
      <div className="flex flex-wrap items-center gap-2 border-b border-[var(--border)] pb-2">
        <SegmentControl
          size="sm"
          ariaLabel="Ingest detail panel"
          value={detailTab}
          onChange={v => setDetailTab(v as IngestDetailTab)}
          options={[
            { value: 'schedule', label: 'Schedule' },
            { value: 'jobs', label: 'Job queue' },
            { value: 'enqueue', label: 'Enqueue' },
          ]}
        />
        {detailTab === 'schedule' && dash?.schedule != null ? (
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant={statusVariant(dash.schedule.verdict ?? '')}>
              {dash.schedule.verdict ?? '—'}
            </DenseTag>
            <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
              Adherence
            </span>
            <SegmentControl
              size="sm"
              ariaLabel="Schedule adherence filter"
              value={adherenceFilter}
              onChange={v => setAdherenceFilter(v as ScheduleAdherenceFilter)}
              options={[
                { value: 'all', label: `All ${scheduleSlots.length}` },
                { value: 'on_plan', label: `On plan ${dash.schedule.on_plan ?? 0}` },
                { value: 'due', label: `Due ${dash.schedule.due ?? 0}` },
                { value: 'missed', label: `Missed ${dash.schedule.missed ?? 0}` },
              ]}
            />
          </div>
        ) : null}
        {detailTab === 'jobs' ? (
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {jobKindFilter !== '' ? (
              <DenseTag variant="info">kind {jobKindFilter}</DenseTag>
            ) : null}
            {jobKindFilter !== '' ? (
              <Button variant="ghost" size="sm" onClick={() => setJobKindFilter('')}>
                Clear kind
              </Button>
            ) : null}
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
                { value: 'pending', label: `Pending ${readyNow}` },
                { value: 'running', label: `Running ${running}` },
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
        ) : null}
      </div>

      {detailTab === 'schedule' ? (
        <OpsSection
          title="Schedule plan & adherence"
          description="Diamond = Cron fire · bar = drain. Click a lane to link the table."
          bodyPadding="compact"
          overflow="visible"
          collapsible
          defaultCollapsed={false}
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
            <>
              <div className="mb-2 flex items-center gap-2">
                <ScoreRing
                  ready={dash.schedule?.on_plan ?? 0}
                  thin={dash.schedule?.due ?? 0}
                  blocked={dash.schedule?.missed ?? 0}
                  total={Math.max(scheduleSlots.length, 1)}
                  caption="plan"
                />
                <div className="min-w-0 flex-1">
                  <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {dash.schedule?.verdict ?? '—'} · {dash.schedule?.on_plan ?? 0} on plan ·{' '}
                    {dash.schedule?.due ?? 0} due · {dash.schedule?.missed ?? 0} missed
                    {adherenceFilter !== 'all'
                      ? ` · showing ${visibleScheduleSlots.length}`
                      : ''}
                  </p>
                  <div className="mt-1 flex flex-wrap gap-px">
                    {visibleScheduleSlots.map(s => (
                      <button
                        key={`heat-${s.slot}`}
                        type="button"
                        title={`${s.slot} · ${s.adherence ?? '—'}`}
                        className={`h-3.5 w-3.5 rounded-[2px] border-0 p-0 ${toneByLevel(slotAdherenceKind(s.adherence))}`}
                        aria-pressed={selectedSlot === s.slot}
                        onClick={() => selectScheduleSlot(s.slot, 'lane')}
                      />
                    ))}
                  </div>
                </div>
              </div>
              {visibleScheduleSlots.length === 0 ? (
                <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  No slots match this adherence filter
                </p>
              ) : (
              <ScheduleSwimlane
                slots={visibleScheduleSlots}
                kindCounts={kindRollup}
                horizon={dash.schedule?.horizon}
                nowMs={nowMs}
                selectedSlot={selectedSlot}
                onSelectSlot={slot => selectScheduleSlot(slot, 'lane')}
              />
              )}
              {selectedSlotRow != null ? (
                <div className="flex flex-wrap items-center gap-2 border-t border-[var(--border)] px-3 py-2">
                  <DenseTag variant={statusVariant(selectedSlotRow.adherence ?? '')}>
                    {selectedSlotRow.slot}
                  </DenseTag>
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {selectedSlotRow.detail ?? selectedSlotRow.note ?? 'Selected slot'}
                  </span>
                  {(selectedSlotRow.evidence_kinds ?? []).length > 0 ? (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => openSlotJobs(selectedSlotRow)}
                    >
                      View jobs
                    </Button>
                  ) : null}
                  <Button variant="ghost" size="sm" onClick={() => setSelectedSlot(null)}>
                    Clear
                  </Button>
                </div>
              ) : null}
              <OpsSection
                variant="flat"
                title="Plan table"
                collapsible
                defaultCollapsed
                bodyPadding="none"
                overflow="visible"
              >
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Slot</DenseTableHead>
                    <DenseTableHead>Schedule</DenseTableHead>
                    <DenseTableHead>Adherence</DenseTableHead>
                    <DenseTableHead>Last fire</DenseTableHead>
                    <DenseTableHead>Next fires</DenseTableHead>
                    <DenseTableHead>Evidence</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {visibleScheduleSlots.length === 0 ? (
                    <DenseTableRow>
                      <DenseTableCell colSpan={6} className="text-center text-[var(--muted-foreground)]">
                        No slots match this adherence filter
                      </DenseTableCell>
                    </DenseTableRow>
                  ) : (
                  visibleScheduleSlots.map(s => (
                    <DenseTableRow
                      key={s.slot}
                      id={scheduleRowId(s.slot)}
                      className={cn(
                        'cursor-pointer',
                        selectedSlot === s.slot &&
                          'bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_14%,transparent)]',
                      )}
                      aria-selected={selectedSlot === s.slot}
                      tabIndex={0}
                      onClick={() => selectScheduleSlot(s.slot, 'table')}
                      onKeyDown={e => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          selectScheduleSlot(s.slot, 'table')
                        }
                      }}
                    >
                      <DenseTableCell>
                        <div className="flex flex-col gap-0.5">
                          <span className="font-mono text-xs">{s.slot}</span>
                          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                            {s.note ?? ''}
                            {s.inline ? ' · inline' : ''}
                          </span>
                        </div>
                      </DenseTableCell>
                      <DenseTableCell
                        className="text-[var(--text-dense-meta)]"
                        title={s.cron ? `cron: ${s.cron}` : undefined}
                      >
                        {s.cron ? describeCronSchedule(s.cron) : '—'}
                      </DenseTableCell>
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
                  ))
                  )}
                </DenseTableBody>
              </DenseDataTable>
              </OpsSection>
            </>
          )}
        </OpsSection>
      ) : null}

      {detailTab === 'jobs' ? (
        <OpsSection
          title="Job queue"
          description={`Click a kind to filter · table is latest ${JOB_PAGE_LIMIT} · Running = time since claim`}
          headerExtra={<DenseTag variant="neutral">{jobsShowing}</DenseTag>}
          bodyPadding="none"
          overflow="visible"
          collapsible
          defaultCollapsed={false}
        >
          <JobQueuePressure
            dash={dash}
            kinds={kindRollup}
            selectedKind={jobKindFilter}
            nowMs={nowMs}
            onSelectKind={k => setJobKindFilter(prev => (prev === k ? '' : k))}
          />
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
            <>
            <OpsSection
              variant="flat"
              title="Latest jobs"
              collapsible
              defaultCollapsed={statusFilter === 'all' && jobKindFilter === ''}
              bodyPadding="none"
              overflow="visible"
            >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>ID</DenseTableHead>
                  <DenseTableHead>Kind</DenseTableHead>
                  <DenseTableHead>Status</DenseTableHead>
                  <DenseTableHead>Waited</DenseTableHead>
                  <DenseTableHead>Running</DenseTableHead>
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
                      {j.status === 'pending'
                        ? waitedLabel(j.created_at, nowMs)
                        : j.started_at && j.created_at
                          ? waitedLabel(j.created_at, Date.parse(j.started_at))
                          : '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-xs">
                      {j.started_at == null ? (
                        '—'
                      ) : j.status === 'running' ? (
                        <span className="inline-flex items-center gap-1">
                          {formatDurationSec(runningAgeSec(j.started_at, nowMs))}
                          <DenseTag
                            variant={freshnessTagVariant(
                              runningFreshness(runningAgeSec(j.started_at, nowMs)),
                            )}
                          >
                            {freshnessLabel(runningFreshness(runningAgeSec(j.started_at, nowMs)))}
                          </DenseTag>
                        </span>
                      ) : j.finished_at != null ? (
                        waitedLabel(j.started_at, Date.parse(j.finished_at))
                      ) : (
                        '—'
                      )}
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
            <p className="m-0 border-t border-[var(--border)] px-3 py-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {jobsShowing}
              {statusFilter === 'pending' || statusFilter === 'running'
                ? ` · page size ${JOB_PAGE_LIMIT}`
                : ` · page size ${JOB_PAGE_LIMIT} (not a full-table total)`}
            </p>
            </OpsSection>
            </>
          )}
        </OpsSection>
      ) : null}

      {detailTab === 'enqueue' ? (
        <OpsSection
          title="Enqueue job"
          description="Writes ops_jobs.job_ingest · operator auth"
          bodyPadding="compact"
          overflow="visible"
          collapsible
          defaultCollapsed={false}
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
      ) : null}

      <OpsSection
        title="JSON probe"
        description="Inspect kinds / queue-dashboard / a single job"
        bodyPadding="compact"
        overflow="visible"
        collapsible
        defaultCollapsed
      >
        <MarketDataJsonProbeCard
          title="JSON Probe"
          description="Inspect ingest kinds / queue-dashboard / a single job"
          defaultPath="/market/ingest/queue-dashboard"
        />
      </OpsSection>

      <ConfirmDialog
        open={confirmOpen}
        title="Enqueue ingest job"
        message={`Enqueue kind "${selectedKind}" into ops_jobs.job_ingest? Workers will pick it up asynchronously.`}
        confirmLabel="Confirm enqueue"
        confirming={acting}
        onConfirm={() => void runEnqueue()}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  )
}
