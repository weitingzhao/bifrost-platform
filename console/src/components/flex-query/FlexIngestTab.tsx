import { useEffect, useState } from 'react'
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
  enqueueFlexIngestJob,
  fetchFlexIngestJobs,
  fetchFlexIngestKinds,
  fetchFlexIngestQueueDashboard,
  isProxyError,
  type FlexIngestJob,
  type FlexQueueSlot,
} from '@/api/flexQueryPlugin'
import { FlexQueueDashboardPanel } from '@/components/flex-query/FlexQueueDashboardPanel'
import {
  flexStatusVariant,
  formatFlexResult,
  slotAdherenceKind,
} from '@/components/flex-query/flexQueryStatusUtils'
import { ScoreRing } from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { describeCronSchedule } from '@/lib/patrol/cronSchedule'
import { FlexManualOpsPanel } from '@/components/flex-query/FlexManualOpsPanel'

type IngestSubTab = 'schedule' | 'jobs' | 'enqueue' | 'manual'

export type FlexIngestSubTab = IngestSubTab

export function FlexIngestTab({ initialSub }: { initialSub?: IngestSubTab }) {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [sub, setSub] = useState<IngestSubTab>(initialSub ?? 'schedule')
  const [statusFilter, setStatusFilter] = useState('all')
  const [kind, setKind] = useState('')
  const [payloadText, setPayloadText] = useState('{}')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)

  useEffect(() => {
    if (initialSub != null) setSub(initialSub)
  }, [initialSub])

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
  const kinds = kindsRaw != null && !isProxyError(kindsRaw) ? (kindsRaw.kinds ?? []) : []
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

  const onPlan = slots.filter(
    s => (s.adherence ?? (s.late ? 'late' : 'on_plan')) === 'on_plan',
  ).length
  const late = slots.filter(s => (s.adherence ?? (s.late ? 'late' : 'on_plan')) === 'late').length
  const noData = slots.filter(
    s => (s.adherence ?? (s.late ? 'late' : 'on_plan')) === 'no_data',
  ).length

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
    <div className="flex flex-col gap-2">
      <FlexQueueDashboardPanel
        counts={counts}
        loading={dashQ.isLoading && counts == null}
        error={dashErr}
      />

      <div className="flex flex-wrap items-center gap-2 border-b border-[var(--border)] pb-2">
        <SegmentControl
          size="sm"
          value={sub}
          onChange={v => setSub(v as IngestSubTab)}
          ariaLabel="Flex ingest sub-tabs"
          options={[
            { value: 'schedule', label: 'Schedule' },
            { value: 'jobs', label: 'Job queue' },
            { value: 'enqueue', label: 'Enqueue' },
            { value: 'manual', label: 'Manual' },
          ]}
        />
        {sub === 'jobs' ? (
          <div className="ml-auto flex flex-wrap items-center gap-2">
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
            <Button
              variant="outline"
              size="sm"
              disabled={jobsQ.isFetching}
              onClick={() => {
                void jobsQ.refetch()
                void dashQ.refetch()
              }}
            >
              Refresh
            </Button>
          </div>
        ) : null}
      </div>

      {sub === 'schedule' ? (
        <OpsSection
          title="Schedule plan & adherence"
          description="Cron plan vs job evidence"
          bodyPadding="compact"
          overflow="visible"
          collapsible
          defaultCollapsed={false}
        >
          {dashQ.isLoading ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Loading schedule…
            </p>
          ) : dashErr != null ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{dashErr}</p>
          ) : slots.length === 0 ? (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No slots in schedule.yaml
            </p>
          ) : (
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center gap-2">
                <ScoreRing
                  ready={onPlan}
                  thin={late}
                  blocked={0}
                  unknown={noData}
                  total={Math.max(slots.length, 1)}
                  caption="plan"
                />
                <div className="min-w-0 flex-1">
                  <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {onPlan} on plan · {late} late · {noData} no data
                  </p>
                  <div className="mt-1 flex flex-wrap gap-px">
                    {slots.map(s => {
                      const adh = s.adherence ?? (s.late ? 'late' : 'on_plan')
                      return (
                        <span
                          key={`heat-${s.slot}`}
                          title={`${s.slot} · ${adh}`}
                          className={`h-3.5 w-3.5 rounded-[2px] ${toneByLevel(slotAdherenceKind(adh))}`}
                        />
                      )
                    })}
                  </div>
                </div>
              </div>
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
                            <DenseTag variant={flexStatusVariant(adh)}>{adh}</DenseTag>
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
              </OpsSection>
            </div>
          )}
        </OpsSection>
      ) : null}

      {sub === 'jobs' ? (
        <OpsSection
          title="Job queue"
          description="Latest 40 rows"
          headerExtra={<DenseTag variant="neutral">{jobs.length} shown</DenseTag>}
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
            <OpsSection
              variant="flat"
              title="Latest jobs"
              collapsible
              defaultCollapsed={false}
              bodyPadding="none"
              overflow="visible"
            >
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
                        <DenseTag variant={flexStatusVariant(j.status)}>{j.status}</DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                        {j.created_at ?? '—'}
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                        {formatFlexResult(j.result)}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </OpsSection>
          )}
        </OpsSection>
      ) : null}

      {sub === 'enqueue' ? (
        <OpsSection
          title="Enqueue job"
          description="Writes ops_jobs.job_flex_ingest · operator auth"
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

      {sub === 'manual' ? <FlexManualOpsPanel /> : null}

      <ConfirmDialog
        open={confirmOpen}
        title="Enqueue Flex ingest job"
        message={`Enqueue kind "${selectedKind}" into ops_jobs.job_flex_ingest? The worker will pick it up asynchronously.`}
        confirmLabel="Confirm enqueue"
        confirming={acting}
        onConfirm={() => void runEnqueue()}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  )
}
