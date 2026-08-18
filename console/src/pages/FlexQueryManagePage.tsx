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
  StatusLamp,
} from '@bifrost/ui'
import {
  enqueueFlexIngestJob,
  fetchFlexCoverageDbSummary,
  fetchFlexCoverageFreshness,
  fetchFlexIngestJobs,
  fetchFlexIngestKinds,
  fetchFlexIngestQueueDashboard,
  isProxyError,
  type FlexIngestJob,
  type FlexQueueSlot,
} from '@/api/flexQueryPlugin'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { describeCronSchedule } from '@/lib/patrol/cronSchedule'

type ManageTab = 'overview' | 'ingest' | 'coverage'
type IngestSubTab = 'schedule' | 'jobs' | 'enqueue'
type CoverageSubTab = 'quality' | 'db-summary'

function statusVariant(
  status: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  const s = status.toLowerCase()
  if (s === 'done' || s === 'success' || s === 'on_plan') return 'success'
  if (s === 'running' || s === 'pending' || s === 'due') return 'info'
  if (s === 'failed' || s === 'error' || s === 'missed' || s === 'late') return 'danger'
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

function reachToVerdict(reach: 'ok' | 'degraded' | 'fail' | 'unknown'): {
  lamp: 'ok' | 'degraded' | 'fail' | 'unknown'
  tagLabel: string
  tagVariant: 'success' | 'warning' | 'danger' | 'neutral'
} {
  switch (reach) {
    case 'ok':
      return { lamp: 'ok', tagLabel: 'OK', tagVariant: 'success' }
    case 'degraded':
      return { lamp: 'degraded', tagLabel: 'DEGRADED', tagVariant: 'warning' }
    case 'fail':
      return { lamp: 'fail', tagLabel: 'FAIL', tagVariant: 'danger' }
    default:
      return { lamp: 'unknown', tagLabel: 'UNKNOWN', tagVariant: 'neutral' }
  }
}

export function FlexQueryManagePage() {
  const [tab, setTab] = useState<ManageTab>('overview')
  const probe = useFlexQueryLiveProbe()
  const mdReach = probe.isLoading ? 'unknown' : probe.probeReach
  const verdict = reachToVerdict(mdReach)

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <SegmentControl
          size="sm"
          value={tab}
          onChange={v => setTab(v as ManageTab)}
          ariaLabel="Flex Query manage tabs"
          options={[
            { value: 'overview', label: 'Overview' },
            { value: 'ingest', label: 'Ingest' },
            { value: 'coverage', label: 'Coverage' },
          ]}
        />
      </div>

      {tab === 'overview' ? (
        <div className="flex flex-col gap-4">
          <OpsVerdictStrip
            ariaLabel="IB Flex Query plugin verdict"
            title="IB FLEX QUERY"
            lamp={verdict.lamp}
            tagLabel={verdict.tagLabel}
            tagVariant={verdict.tagVariant}
            summary={probe.summary}
            actions={
              <Button
                variant="outline"
                size="sm"
                disabled={probe.isLoading}
                onClick={() => probe.refetch()}
              >
                Refresh
              </Button>
            }
          />
          <OpsSection
            title="Deployments"
            description="L0 observe via platform-api GET /api/v1/plugins/flex-query/status"
            leading={<StatusLamp value={mdReach} kind="reach" />}
            headerExtra={<DenseTag variant={verdict.tagVariant}>{verdict.tagLabel}</DenseTag>}
            bodyPadding="default"
            overflow="visible"
          >
            {(probe.status?.deployments ?? []).length === 0 ? (
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No deployment snapshot yet — apply k8s/base or check platform-api probe.
              </p>
            ) : (
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Name</DenseTableHead>
                    <DenseTableHead>Ready</DenseTableHead>
                    <DenseTableHead>Reach</DenseTableHead>
                    <DenseTableHead>Detail</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {(probe.status?.deployments ?? []).map(d => (
                    <DenseTableRow key={d.name}>
                      <DenseTableCell className="font-mono text-xs">{d.name}</DenseTableCell>
                      <DenseTableCell className="font-mono text-xs">{d.ready}</DenseTableCell>
                      <DenseTableCell>
                        <DenseTag variant={statusVariant(d.reachability ?? '')}>
                          {d.reachability ?? '—'}
                        </DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                        {d.detail ?? '—'}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            )}
          </OpsSection>
        </div>
      ) : null}

      {tab === 'ingest' ? <IngestPanel /> : null}
      {tab === 'coverage' ? <CoveragePanel /> : null}
    </div>
  )
}

function IngestPanel() {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [sub, setSub] = useState<IngestSubTab>('schedule')
  const [statusFilter, setStatusFilter] = useState('all')
  const [kind, setKind] = useState('')
  const [payloadText, setPayloadText] = useState('{}')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)

  const kindsQ = useQuery({
    queryKey: ['flex-query', 'ingest', 'kinds'],
    queryFn: fetchFlexIngestKinds,
    refetchInterval: 120_000,
    retry: 1,
  })
  const dashQ = useQuery({
    queryKey: ['flex-query', 'ingest', 'queue-dashboard'],
    queryFn: fetchFlexIngestQueueDashboard,
    refetchInterval: 15_000,
    retry: 1,
  })
  const jobsQ = useQuery({
    queryKey: ['flex-query', 'ingest', 'jobs', statusFilter],
    queryFn: () =>
      fetchFlexIngestJobs({
        limit: 40,
        status: statusFilter === 'all' ? undefined : statusFilter,
      }),
    refetchInterval: 15_000,
    retry: 1,
  })

  const kindsRaw = kindsQ.data
  const kinds =
    kindsRaw != null && !isProxyError(kindsRaw) ? (kindsRaw.kinds ?? []) : []
  const kindsErr = kindsRaw != null && isProxyError(kindsRaw) ? kindsRaw.error : null
  const selectedKind = kind || kinds[0] || ''

  const dashRaw = dashQ.data
  const dashErr = dashRaw != null && isProxyError(dashRaw) ? dashRaw.error : null
  const slots: FlexQueueSlot[] =
    dashRaw != null && !isProxyError(dashRaw) ? (dashRaw.slots ?? []) : []
  const counts = dashRaw != null && !isProxyError(dashRaw) ? dashRaw.counts : null

  const jobsRaw = jobsQ.data
  const jobsErr = jobsRaw != null && isProxyError(jobsRaw) ? jobsRaw.error : null
  const jobs: FlexIngestJob[] =
    jobsRaw != null && !isProxyError(jobsRaw) ? (jobsRaw.jobs ?? []) : []

  async function runEnqueue() {
    setActing(true)
    setActionMsg(null)
    try {
      let payload: Record<string, unknown> = {}
      if (payloadText.trim() !== '') {
        payload = JSON.parse(payloadText) as Record<string, unknown>
      }
      const res = await enqueueFlexIngestJob(selectedKind, payload)
      if (isProxyError(res)) {
        setActionFailed(true)
        setActionMsg(res.error)
      } else {
        setActionFailed(false)
        setActionMsg(
          res.deduped
            ? `Deduped — existing ${res.kind} job still pending/running`
            : `Enqueued ${res.kind} job ${res.job_id ?? ''}`,
        )
        void queryClient.invalidateQueries({ queryKey: ['flex-query', 'ingest'] })
      }
    } catch (e) {
      setActionFailed(true)
      setActionMsg(e instanceof Error ? e.message : String(e))
    } finally {
      setActing(false)
      setConfirmOpen(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <SegmentControl
          size="sm"
          value={sub}
          onChange={v => setSub(v as IngestSubTab)}
          ariaLabel="Flex ingest sub-tabs"
          options={[
            { value: 'schedule', label: 'Schedule' },
            { value: 'jobs', label: 'Job Queue' },
            { value: 'enqueue', label: 'Enqueue' },
          ]}
        />
        {counts != null ? (
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            pending {counts.pending ?? 0} · running {counts.running ?? 0} · done {counts.done ?? 0} ·
            failed {counts.failed ?? 0}
          </span>
        ) : null}
      </div>

      {sub === 'schedule' ? (
        <OpsSection
          title="Schedule"
          description="Plugin GET /flex/ingest/queue-dashboard — plan vs last job"
          bodyPadding="none"
          overflow="visible"
        >
          {dashQ.isLoading ? (
            <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Loading schedule…
            </p>
          ) : dashErr != null ? (
            <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--destructive)]">
              {dashErr}
            </p>
          ) : (
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Slot</DenseTableHead>
                  <DenseTableHead>Cron</DenseTableHead>
                  <DenseTableHead>Last planned</DenseTableHead>
                  <DenseTableHead>Next</DenseTableHead>
                  <DenseTableHead>Last job</DenseTableHead>
                  <DenseTableHead>Adherence</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {slots.map(s => (
                  <DenseTableRow key={s.slot}>
                    <DenseTableCell className="font-mono text-xs">{s.slot}</DenseTableCell>
                    <DenseTableCell className="text-[var(--text-dense-meta)]">
                      {s.cron ? describeCronSchedule(s.cron) : '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                      {s.last_planned_at ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                      {(s.next_fires ?? []).slice(0, 2).join(' · ') || '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                      {s.last_job
                        ? `${s.last_job.status} #${s.last_job.id ?? ''}`
                        : '—'}
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={s.late ? 'danger' : 'success'}>
                        {s.late ? 'late' : 'on plan'}
                      </DenseTag>
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          )}
        </OpsSection>
      ) : null}

      {sub === 'jobs' ? (
        <OpsSection
          title="Job queue"
          description="Plugin GET /flex/ingest/jobs"
          bodyPadding="none"
          overflow="visible"
        >
          <div className="flex items-center gap-2 px-3 py-2">
            <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
              Status:
            </span>
            <SegmentControl
              size="sm"
              value={statusFilter}
              onChange={setStatusFilter}
              ariaLabel="Job status filter"
              options={[
                { value: 'all', label: 'All' },
                { value: 'pending', label: 'Pending' },
                { value: 'running', label: 'Running' },
                { value: 'done', label: 'Done' },
                { value: 'failed', label: 'Failed' },
              ]}
            />
          </div>
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
                  <DenseTableHead>Created</DenseTableHead>
                  <DenseTableHead>Result</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {jobs.map(j => (
                  <DenseTableRow key={String(j.id)}>
                    <DenseTableCell className="font-mono text-xs">{j.id ?? '—'}</DenseTableCell>
                    <DenseTableCell className="font-mono text-xs">{j.kind}</DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={statusVariant(j.status)}>{j.status}</DenseTag>
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
      ) : null}

      {sub === 'enqueue' ? (
        <OpsSection
          title="Enqueue job"
          description="POST /flex/ingest/enqueue — writes flex_ops.job_flex_ingest (operator auth)"
          bodyPadding="default"
          overflow="visible"
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

      <ConfirmDialog
        open={confirmOpen}
        title="Enqueue Flex ingest job"
        message={`Enqueue kind "${selectedKind}" into flex_ops.job_flex_ingest? The worker will pick it up asynchronously.`}
        confirmLabel="Confirm enqueue"
        confirming={acting}
        onConfirm={() => void runEnqueue()}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  )
}

function CoveragePanel() {
  const [sub, setSub] = useState<CoverageSubTab>('quality')
  const freshQ = useQuery({
    queryKey: ['flex-query', 'coverage', 'freshness'],
    queryFn: fetchFlexCoverageFreshness,
    refetchInterval: 60_000,
    retry: 1,
  })
  const summaryQ = useQuery({
    queryKey: ['flex-query', 'coverage', 'db-summary'],
    queryFn: fetchFlexCoverageDbSummary,
    refetchInterval: 60_000,
    retry: 1,
  })

  const freshRaw = freshQ.data
  const freshErr = freshRaw != null && isProxyError(freshRaw) ? freshRaw.error : null
  const dims = useMemo(
    () => (freshRaw != null && !isProxyError(freshRaw) ? (freshRaw.dimensions ?? []) : []),
    [freshRaw],
  )
  const summaryRaw = summaryQ.data
  const summaryErr = summaryRaw != null && isProxyError(summaryRaw) ? summaryRaw.error : null
  const tables =
    summaryRaw != null && !isProxyError(summaryRaw) ? (summaryRaw.tables ?? []) : []

  return (
    <div className="flex flex-col gap-4">
      <SegmentControl
        size="sm"
        value={sub}
        onChange={v => setSub(v as CoverageSubTab)}
        ariaLabel="Flex coverage sub-tabs"
        options={[
          { value: 'quality', label: 'Quality' },
          { value: 'db-summary', label: 'DB Summary' },
        ]}
      />

      {sub === 'quality' ? (
        <OpsSection
          title="Freshness"
          description="flex_ops.ingest_freshness — last worker write per kind"
          bodyPadding="none"
          overflow="visible"
        >
          {freshQ.isLoading ? (
            <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Loading freshness…
            </p>
          ) : freshErr != null ? (
            <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--destructive)]">
              {freshErr}
            </p>
          ) : dims.length === 0 ? (
            <p className="m-0 px-3 py-4 text-center text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No ingest_freshness rows yet — run CronJobs or enqueue, then refresh.
            </p>
          ) : (
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Dimension</DenseTableHead>
                  <DenseTableHead>Latest</DenseTableHead>
                  <DenseTableHead>Rows</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {dims.map(d => (
                  <DenseTableRow key={d.dimension}>
                    <DenseTableCell className="font-mono text-xs">{d.dimension}</DenseTableCell>
                    <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                      {d.latest_ts ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-xs">{d.row_count ?? '—'}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          )}
        </OpsSection>
      ) : null}

      {sub === 'db-summary' ? (
        <OpsSection
          title="Brokerage tables"
          description="Golden Source brokerage.* row counts"
          bodyPadding="none"
          overflow="visible"
        >
          {summaryQ.isLoading ? (
            <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Loading summary…
            </p>
          ) : summaryErr != null ? (
            <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--destructive)]">
              {summaryErr}
            </p>
          ) : (
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Table</DenseTableHead>
                  <DenseTableHead>Rows</DenseTableHead>
                  <DenseTableHead>Latest</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {tables.map(t => (
                  <DenseTableRow key={t.name}>
                    <DenseTableCell className="font-mono text-xs">{t.relation ?? t.name}</DenseTableCell>
                    <DenseTableCell className="font-mono text-xs">
                      {t.row_count ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                      {t.latest_ts ?? '—'}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          )}
        </OpsSection>
      ) : null}
    </div>
  )
}
