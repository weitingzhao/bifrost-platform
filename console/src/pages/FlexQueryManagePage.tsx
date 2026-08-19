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
  fetchFlexConfigSummary,
  fetchFlexCoverageDbSummary,
  fetchFlexCoverageFreshness,
  fetchFlexCoverageRawPeek,
  fetchFlexFreshnessKpis,
  fetchFlexIngestJobs,
  fetchFlexIngestKinds,
  fetchFlexIngestQueueDashboard,
  isProxyError,
  triggerFlexFetch,
  uploadFlexXml,
  type FlexConfigSummary,
  type FlexFreshnessKpis,
  type FlexIngestJob,
  type FlexQueueSlot,
  type FlexRawPeekResponse,
  type FlexRawPeekTable,
} from '@/api/flexQueryPlugin'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { describeCronSchedule } from '@/lib/patrol/cronSchedule'

type ManageTab = 'overview' | 'ingest' | 'coverage' | 'config'
type IngestSubTab = 'schedule' | 'jobs' | 'enqueue' | 'manual'
type CoverageSubTab = 'quality' | 'db-summary' | 'raw-peek'

function statusVariant(
  status: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  const s = status.toLowerCase()
  if (s === 'done' || s === 'success' || s === 'on_plan') return 'success'
  if (s === 'running' || s === 'pending' || s === 'due') return 'info'
  if (s === 'failed' || s === 'error' || s === 'missed' || s === 'late') return 'danger'
  if (s === 'no_data') return 'neutral'
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
            { value: 'config', label: 'Config' },
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
          <FreshnessKpiPanel />
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
      {tab === 'config' ? <ConfigPanel /> : null}
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
            { value: 'manual', label: 'Manual' },
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
          title="Schedule plan & adherence"
          description="Future Cron fires + last fire vs jobs (plan vs actual)"
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
          ) : slots.length === 0 ? (
            <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No slots in schedule.yaml
            </p>
          ) : (
            <>
              {(() => {
                const onPlan = slots.filter(s => (s.adherence ?? (s.late ? 'late' : 'on_plan')) === 'on_plan').length
                const late = slots.filter(s => (s.adherence ?? (s.late ? 'late' : 'on_plan')) === 'late').length
                const noData = slots.filter(s => (s.adherence ?? (s.late ? 'late' : 'on_plan')) === 'no_data').length
                return (
                  <div className="flex flex-wrap items-center gap-2 px-3 py-2">
                    <DenseTag variant="success">on_plan {onPlan}</DenseTag>
                    {late > 0 ? <DenseTag variant="danger">late {late}</DenseTag> : null}
                    {noData > 0 ? <DenseTag variant="neutral">no_data {noData}</DenseTag> : null}
                  </div>
                )
              })()}
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
                  {slots.map(s => {
                    const adh = s.adherence ?? (s.late ? 'late' : 'on_plan')
                    return (
                    <DenseTableRow key={s.slot}>
                      <DenseTableCell>
                        <div className="flex flex-col gap-0.5">
                          <span className="font-mono text-xs">{s.slot}</span>
                          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                            {s.kind}
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
                        <DenseTag variant={statusVariant(adh)}>
                          {adh}
                        </DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                        {s.last_planned_at ?? '—'}
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                        {(s.next_fires ?? []).slice(0, 2).join(' · ') || '—'}
                      </DenseTableCell>
                      <DenseTableCell className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                        {s.last_job
                          ? `${s.last_job.status} #${s.last_job.id ?? ''}`
                          : '—'}
                      </DenseTableCell>
                    </DenseTableRow>
                    )
                  })}
                </DenseTableBody>
              </DenseDataTable>
            </>
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

      {sub === 'manual' ? <ManualOpsSubPanel /> : null}

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

function ManualOpsSubPanel() {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [triggerKind, setTriggerKind] = useState<'trades' | 'transactions'>('trades')
  const [triggerActing, setTriggerActing] = useState(false)
  const [triggerMsg, setTriggerMsg] = useState<string | null>(null)
  const [triggerFailed, setTriggerFailed] = useState(false)
  const [triggerConfirm, setTriggerConfirm] = useState(false)
  const [triggerResult, setTriggerResult] = useState<Record<string, unknown> | null>(null)

  const [uploadActing, setUploadActing] = useState(false)
  const [uploadMsg, setUploadMsg] = useState<string | null>(null)
  const [uploadFailed, setUploadFailed] = useState(false)
  const fileRef = { current: null as HTMLInputElement | null }

  async function runTrigger() {
    setTriggerActing(true)
    setTriggerMsg(null)
    setTriggerResult(null)
    try {
      const res = await triggerFlexFetch(triggerKind)
      if (isProxyError(res)) {
        setTriggerFailed(true)
        setTriggerMsg(res.error)
      } else if (res.ok === false) {
        setTriggerFailed(true)
        setTriggerMsg(res.error ?? res.message ?? 'Unknown error')
      } else {
        setTriggerFailed(false)
        setTriggerMsg(res.message ?? `Fetched ${res.count ?? 0} row(s)`)
        setTriggerResult(res as unknown as Record<string, unknown>)
        void queryClient.invalidateQueries({ queryKey: ['flex-query'] })
      }
    } catch (e) {
      setTriggerFailed(true)
      setTriggerMsg(e instanceof Error ? e.message : String(e))
    } finally {
      setTriggerActing(false)
      setTriggerConfirm(false)
    }
  }

  async function handleFileUpload(file: File) {
    setUploadActing(true)
    setUploadMsg(null)
    try {
      const xml = await file.text()
      const res = await uploadFlexXml(xml)
      if (isProxyError(res)) {
        setUploadFailed(true)
        setUploadMsg(res.error)
      } else if (res.ok === false) {
        setUploadFailed(true)
        setUploadMsg(res.error ?? res.message ?? 'Upload failed')
      } else {
        setUploadFailed(false)
        setUploadMsg(res.message ?? `Imported ${res.count ?? 0} trade(s)`)
        void queryClient.invalidateQueries({ queryKey: ['flex-query'] })
      }
    } catch (e) {
      setUploadFailed(true)
      setUploadMsg(e instanceof Error ? e.message : String(e))
    } finally {
      setUploadActing(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <OpsSection
        title="Flex refresh"
        description="Synchronous online fetch from IB Flex Web Service — same as Trade Account page"
        bodyPadding="default"
        overflow="visible"
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex min-w-[10rem] flex-col gap-1">
            <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
              Kind
            </span>
            <select
              className="h-8 rounded-md border border-[var(--border)] bg-[var(--background)] px-2 text-[var(--text-dense-meta)]"
              value={triggerKind}
              onChange={e => setTriggerKind(e.target.value as 'trades' | 'transactions')}
            >
              <option value="trades">Trades (executions)</option>
              <option value="transactions">Cash Transactions</option>
            </select>
          </label>
          <Button
            size="sm"
            disabled={!canOperate || triggerActing}
            onClick={() => setTriggerConfirm(true)}
            title={canOperate ? undefined : 'Operator auth required'}
          >
            {triggerActing ? 'Fetching…' : 'Flex Refresh'}
          </Button>
        </div>
        {!canOperate ? (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Authenticate as operator to trigger Flex fetch.
          </p>
        ) : null}
        {triggerMsg != null ? (
          <p
            className={`m-0 mt-2 text-[var(--text-dense-meta)] ${
              triggerFailed ? 'text-[var(--destructive)]' : 'text-[var(--success)]'
            }`}
          >
            {triggerMsg}
          </p>
        ) : null}
        {triggerResult != null && !triggerFailed ? (
          <div className="mt-2 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
            <div className="flex flex-wrap gap-x-6 gap-y-1 text-[var(--text-dense-caption)] font-mono">
              {triggerResult.count != null ? <span>count: {String(triggerResult.count)}</span> : null}
              {triggerResult.range_mode ? <span>mode: {String(triggerResult.range_mode)}</span> : null}
              {triggerResult.data_from ? <span>from: {String(triggerResult.data_from)}</span> : null}
              {triggerResult.data_to ? <span>to: {String(triggerResult.data_to)}</span> : null}
              {triggerResult.updated_accounts != null ? (
                <span>accounts: {String(triggerResult.updated_accounts)}</span>
              ) : null}
            </div>
            {Array.isArray(triggerResult.per_query) && triggerResult.per_query.length > 0 ? (
              <DenseDataTable wrapClassName="mt-2">
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Role</DenseTableHead>
                    <DenseTableHead>Query ID</DenseTableHead>
                    <DenseTableHead>Rows</DenseTableHead>
                    <DenseTableHead>Data span</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {(triggerResult.per_query as Array<Record<string, unknown>>).map((pq, i) => (
                    <DenseTableRow key={i}>
                      <DenseTableCell className="font-mono text-xs">{String(pq.role ?? '')}</DenseTableCell>
                      <DenseTableCell className="font-mono text-xs">{String(pq.query_id ?? '')}</DenseTableCell>
                      <DenseTableCell className="font-mono text-xs">{String(pq.rows ?? 0)}</DenseTableCell>
                      <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                        {pq.data_from ? `${String(pq.data_from)} → ${String(pq.data_to ?? '')}` : '—'}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            ) : null}
          </div>
        ) : null}
      </OpsSection>

      <OpsSection
        title="Upload Flex XML"
        description="Parse a local Flex Trades XML file and upsert into brokerage.executions_raw_flex"
        bodyPadding="default"
        overflow="visible"
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <input
            ref={el => {
              fileRef.current = el
            }}
            type="file"
            accept=".xml"
            className="hidden"
            onChange={e => {
              const f = e.target.files?.[0]
              if (f) void handleFileUpload(f)
              e.target.value = ''
            }}
          />
          <Button
            size="sm"
            variant="outline"
            disabled={!canOperate || uploadActing}
            onClick={() => fileRef.current?.click()}
            title={canOperate ? undefined : 'Operator auth required'}
          >
            {uploadActing ? 'Uploading…' : 'Choose Flex XML file'}
          </Button>
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Accepts IB Flex Trades report (Activity → Trades). Not for Cash Transactions.
          </span>
        </div>
        {!canOperate ? (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Authenticate as operator to upload XML.
          </p>
        ) : null}
        {uploadMsg != null ? (
          <p
            className={`m-0 mt-2 text-[var(--text-dense-meta)] ${
              uploadFailed ? 'text-[var(--destructive)]' : 'text-[var(--success)]'
            }`}
          >
            {uploadMsg}
          </p>
        ) : null}
      </OpsSection>

      <ConfirmDialog
        open={triggerConfirm}
        title="Trigger Flex refresh"
        message={`Synchronously fetch ${triggerKind === 'trades' ? 'Trades (executions)' : 'Cash Transactions'} from IB Flex Web Service? This uses the tokens and query IDs configured in Trade Settings.`}
        confirmLabel="Confirm fetch"
        confirming={triggerActing}
        onConfirm={() => void runTrigger()}
        onCancel={() => setTriggerConfirm(false)}
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
          { value: 'raw-peek', label: 'Raw Peek' },
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

      {sub === 'raw-peek' ? <RawPeekSubPanel /> : null}
    </div>
  )
}

function formatPeekCell(value: unknown): string {
  if (value == null || value === '') return '—'
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : '—'
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function ConfigPanel() {
  const q = useQuery({
    queryKey: ['flex-query', 'config', 'summary'],
    queryFn: fetchFlexConfigSummary,
    refetchInterval: 60_000,
    retry: 1,
  })
  const raw = q.data
  const err = raw != null && isProxyError(raw) ? raw.error : null
  let summary: FlexConfigSummary | null = null
  if (raw != null && !isProxyError(raw)) {
    summary = raw
  }

  return (
    <div className="flex flex-col gap-4">
      <OpsSection
        title="Tokens"
        description="Per-env public.settings — values masked (last 4)"
        bodyPadding="default"
        overflow="visible"
      >
        {q.isLoading ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading config…
          </p>
        ) : err != null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
        ) : (
          <div className="flex flex-col gap-2">
            {(
              [
                ['Host', summary?.tokens.host_token_set, summary?.tokens.host_token_last4],
                [
                  'Secondary',
                  summary?.tokens.secondary_token_set,
                  summary?.tokens.secondary_token_last4,
                ],
              ] as const
            ).map(([label, set, last4]) => (
              <div key={label} className="flex flex-wrap items-center gap-2">
                <span className="w-24 shrink-0 text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
                  {label}
                </span>
                <DenseTag variant={set ? 'success' : 'neutral'}>{set ? 'set' : 'not set'}</DenseTag>
                <span className="font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  {set ? `····${last4}` : '—'}
                </span>
              </div>
            ))}
          </div>
        )}
      </OpsSection>

      <OpsSection
        title="Range days"
        description="Flex fetch window from public.settings"
        bodyPadding="default"
        overflow="visible"
      >
        {summary == null && err == null && q.isLoading ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading range…
          </p>
        ) : summary == null ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">—</p>
        ) : (
          <p className="m-0 font-mono text-[var(--text-dense-meta)]">
            Default: {summary.range_days.default} days · Init: {summary.range_days.init} days
          </p>
        )}
      </OpsSection>

      <OpsSection
        title="Query rows"
        description="Golden Source brokerage.settings_flex"
        bodyPadding="none"
        overflow="visible"
      >
        {q.isLoading ? (
          <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading query rows…
          </p>
        ) : err != null ? (
          <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--destructive)]">
            {err}
          </p>
        ) : (summary?.query_rows ?? []).length === 0 ? (
          <p className="m-0 px-3 py-4 text-center text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No brokerage.settings_flex rows
          </p>
        ) : (
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Purpose</DenseTableHead>
                <DenseTableHead>Label</DenseTableHead>
                <DenseTableHead>Host query ID</DenseTableHead>
                <DenseTableHead>Secondary query ID</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {(summary?.query_rows ?? []).map((row, i) => (
                <DenseTableRow key={`${row.purpose}-${row.query_host_id}-${i}`}>
                  <DenseTableCell className="font-mono text-xs">{row.purpose}</DenseTableCell>
                  <DenseTableCell className="text-[var(--text-dense-meta)]">
                    {row.query_label ?? '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-xs">
                    {row.query_host_id || '—'}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-xs">
                    {row.query_secondary_id ?? '—'}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        )}
      </OpsSection>

      <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        Read-only. Edit in Trade → Settings → IB Connection.
      </p>
    </div>
  )
}

function kpiAgeVariant(ageSecs: number | null | undefined, warnThreshold: number, dangerThreshold: number): 'success' | 'warning' | 'danger' | 'neutral' {
  if (ageSecs == null) return 'neutral'
  if (ageSecs > dangerThreshold) return 'danger'
  if (ageSecs > warnThreshold) return 'warning'
  return 'success'
}

function FreshnessKpiPanel() {
  const q = useQuery({
    queryKey: ['flex-query', 'dashboard', 'freshness-kpis'],
    queryFn: fetchFlexFreshnessKpis,
    refetchInterval: 30_000,
    retry: 1,
  })
  const raw = q.data
  const err = raw != null && isProxyError(raw) ? raw.error : null
  let kpis: FlexFreshnessKpis | null = null
  if (raw != null && !isProxyError(raw)) {
    kpis = raw
  }

  const cards: Array<{
    label: string
    value: string
    sub?: string
    variant: 'success' | 'warning' | 'danger' | 'neutral'
  }> = []

  if (kpis) {
    const syncAge = kpis.last_successful_sync.age_seconds
    cards.push({
      label: 'Last successful sync',
      value: kpis.last_successful_sync.age_label,
      sub: kpis.last_successful_sync.at ?? undefined,
      variant: syncAge == null ? 'neutral' : kpiAgeVariant(syncAge, 86400, 172800),
    })

    cards.push({
      label: 'Latest execution',
      value: kpis.latest_execution.age_label,
      sub: kpis.latest_execution.at
        ? `${kpis.latest_execution.at} · ${kpis.latest_execution.row_count ?? 0} rows`
        : undefined,
      variant: kpiAgeVariant(kpis.latest_execution.age_seconds, 604800, 1209600),
    })

    cards.push({
      label: 'Latest transaction',
      value: kpis.latest_transaction.age_label,
      sub: kpis.latest_transaction.at
        ? `${kpis.latest_transaction.at} · ${kpis.latest_transaction.row_count ?? 0} rows`
        : undefined,
      variant: kpiAgeVariant(kpis.latest_transaction.age_seconds, 7776000, 15552000),
    })

    const lastRunLabel = kpis.last_run.age_label !== '—'
      ? `${kpis.last_run.age_label}${kpis.last_run.status ? ` · ${kpis.last_run.status}` : ''}`
      : '—'
    cards.push({
      label: 'Last run job',
      value: lastRunLabel,
      sub: kpis.last_run.kind ?? undefined,
      variant: kpis.last_run.status === 'done'
        ? 'success'
        : kpis.last_run.status === 'failed'
          ? 'danger'
          : kpis.last_run.status === 'running'
            ? 'info' as 'warning'
            : 'neutral',
    })

    cards.push({
      label: 'Next scheduled run',
      value: kpis.next_scheduled_run.until_label,
      sub: kpis.next_scheduled_run.at
        ? `${kpis.next_scheduled_run.at}${kpis.next_scheduled_run.slot ? ` · ${kpis.next_scheduled_run.slot}` : ''}`
        : undefined,
      variant: kpis.next_scheduled_run.until_seconds != null && kpis.next_scheduled_run.until_seconds <= 0
        ? 'danger'
        : 'neutral',
    })

    cards.push({
      label: 'Last planned slot',
      value: kpis.last_planned.age_label,
      sub: kpis.last_planned.at ?? undefined,
      variant: kpiAgeVariant(kpis.last_planned.age_seconds, 86400, 172800),
    })
  }

  return (
    <OpsSection
      title="Data freshness"
      description="Aggregated ingest + brokerage data age"
      bodyPadding="default"
      overflow="visible"
    >
      {q.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading freshness KPIs…
        </p>
      ) : err != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
      ) : (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
          {cards.map(c => (
            <div
              key={c.label}
              className="flex flex-col gap-1 rounded-md border border-[var(--border)] px-3 py-2"
            >
              <span className="text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)]">
                {c.label}
              </span>
              <span className="flex items-center gap-1.5">
                <DenseTag variant={c.variant}>{c.value}</DenseTag>
              </span>
              {c.sub ? (
                <span className="font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)] truncate" title={c.sub}>
                  {c.sub}
                </span>
              ) : null}
            </div>
          ))}
        </div>
      )}
    </OpsSection>
  )
}

function RawPeekSubPanel() {
  const [table, setTable] = useState<FlexRawPeekTable>('executions_raw_flex')
  const [limit, setLimit] = useState('20')
  const limitN = Number(limit) || 20
  const peekQ = useQuery({
    queryKey: ['flex-query', 'coverage', 'raw-peek', table, limitN],
    queryFn: () => fetchFlexCoverageRawPeek(table, limitN),
    refetchInterval: 60_000,
    retry: 1,
  })
  const raw = peekQ.data
  const err = raw != null && isProxyError(raw) ? raw.error : null
  let peek: FlexRawPeekResponse | null = null
  if (raw != null && !isProxyError(raw)) {
    peek = raw
  }
  const columns = peek?.columns ?? []
  const rows = peek?.rows ?? []

  return (
    <OpsSection
      title="Raw peek"
      description="Recent Golden Source rows — ingest QA only, not a Trade ledger"
      bodyPadding="none"
      overflow="visible"
    >
      <div className="flex flex-wrap items-center gap-3 px-3 py-2">
        <SegmentControl
          size="sm"
          value={table}
          onChange={v => setTable(v as FlexRawPeekTable)}
          ariaLabel="Raw peek table"
          options={[
            { value: 'executions_raw_flex', label: 'executions_raw_flex' },
            { value: 'transactions', label: 'transactions' },
          ]}
        />
        <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
          Limit:
        </span>
        <SegmentControl
          size="sm"
          value={limit}
          onChange={setLimit}
          ariaLabel="Raw peek row limit"
          options={[
            { value: '20', label: '20' },
            { value: '50', label: '50' },
            { value: '100', label: '100' },
          ]}
        />
      </div>
      {peekQ.isLoading ? (
        <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading rows…
        </p>
      ) : err != null ? (
        <p className="m-0 px-3 py-4 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
      ) : rows.length === 0 ? (
        <p className="m-0 px-3 py-4 text-center text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No rows in {table}
        </p>
      ) : (
        <>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                {columns.map(c => (
                  <DenseTableHead key={c}>{c}</DenseTableHead>
                ))}
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {rows.map((row, i) => (
                <DenseTableRow key={i}>
                  {columns.map((c, j) => (
                    <DenseTableCell
                      key={c}
                      className="font-mono text-[var(--text-dense-caption)] whitespace-nowrap"
                    >
                      {formatPeekCell(row[j])}
                    </DenseTableCell>
                  ))}
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
          <p className="m-0 px-3 py-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Showing {peek?.row_count ?? rows.length} row{rows.length === 1 ? '' : 's'}
          </p>
        </>
      )}
    </OpsSection>
  )
}
