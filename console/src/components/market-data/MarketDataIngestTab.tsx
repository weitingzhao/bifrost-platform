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
} from '@bifrost/ui'
import {
  enqueueIngestJob,
  fetchIngestJobs,
  fetchIngestKinds,
  isProxyError,
  type IngestJob,
} from '@/api/marketDataPlugin'
import { MarketDataJsonProbeCard } from '@/components/market-data/MarketDataJsonProbeCard'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

function statusVariant(
  status: string,
): 'success' | 'warning' | 'danger' | 'neutral' | 'info' {
  const s = status.toLowerCase()
  if (s === 'done' || s === 'success') return 'success'
  if (s === 'running' || s === 'pending') return 'info'
  if (s === 'failed' || s === 'error') return 'danger'
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

export function MarketDataIngestTab() {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [kind, setKind] = useState('')
  const [payloadText, setPayloadText] = useState('{}')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)

  const kindsQ = useQuery({
    queryKey: ['market-data', 'ingest', 'kinds'],
    queryFn: fetchIngestKinds,
    refetchInterval: 120_000,
    retry: 1,
  })
  const jobsQ = useQuery({
    queryKey: ['market-data', 'ingest', 'jobs'],
    queryFn: () => fetchIngestJobs({ limit: 40 }),
    refetchInterval: 15_000,
    retry: 1,
  })

  const kindsRaw = kindsQ.data
  const kindsErr = kindsRaw != null && isProxyError(kindsRaw) ? kindsRaw.error : null
  const kinds =
    kindsRaw != null && !isProxyError(kindsRaw) ? (kindsRaw.kinds ?? []) : []

  const jobsRaw = jobsQ.data
  const jobsErr = jobsRaw != null && isProxyError(jobsRaw) ? jobsRaw.error : null
  const jobs: IngestJob[] =
    jobsRaw != null && !isProxyError(jobsRaw) ? (jobsRaw.jobs ?? []) : []

  const selectedKind = useMemo(() => {
    if (kind !== '') return kind
    return kinds[0] ?? ''
  }, [kind, kinds])

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
        setActionMsg(e instanceof Error ? e.message : 'Invalid payload JSON')
        return
      }
      if (!selectedKind) {
        setActionFailed(true)
        setActionMsg('Select a kind')
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
          ? `Deduplicated · job ${res.job_id ?? '—'}`
          : `Enqueued · job ${res.job_id ?? '—'}`,
      )
      void queryClient.invalidateQueries({ queryKey: ['market-data', 'ingest', 'jobs'] })
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
      <OpsSection
        title="Job queue"
        description="Plugin GET /market/ingest/jobs"
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={jobsQ.isFetching}
            onClick={() => void jobsQ.refetch()}
          >
            Refresh
          </Button>
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

      <MarketDataJsonProbeCard
        title="JSON Probe"
        description="Inspect ingest kinds / a single job"
        defaultPath="/market/ingest/kinds"
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
