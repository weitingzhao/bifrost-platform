import { useCallback, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
  fetchMarketDataDoctor,
  healMarketData,
  type DoctorFinding,
  type HealResponse,
} from '@/api/marketDataDoctor'
import {
  autoFixableIds,
  buildDoctorAgentReport,
  describeFix,
  formatValue,
  severityVariant,
  sortFindings,
  verdictVariant,
} from '@/components/market-data/doctorModel'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

const DOCTOR_KEY = ['market-data', 'doctor'] as const
const RECHECK_AFTER_HEAL_MS = 60_000

type PendingFix = { label: string; findingIds: string[] | null }

/**
 * Check now, fix now. The doctor names what the session should hold and what
 * it does; every non-ok row carries the exact enqueue the plugin will run.
 * "Fix" executes one prescription, "Fix all" every auto-fixable one, and the
 * report copies as something an agent can act on, not just read.
 */
export function DoctorPanel() {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [pending, setPending] = useState<PendingFix | null>(null)
  const [lastHeal, setLastHeal] = useState<HealResponse | null>(null)
  const [copied, setCopied] = useState(false)

  const q = useQuery({
    queryKey: DOCTOR_KEY,
    queryFn: () => fetchMarketDataDoctor(true),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  })
  const report = q.data ?? null
  const findings = useMemo(() => sortFindings(report?.findings ?? []), [report])
  const fixable = useMemo(() => autoFixableIds(report), [report])

  const heal = useMutation({
    mutationFn: (ids: string[] | null) => healMarketData(ids == null ? { dry_run: false } : { dry_run: false, finding_ids: ids }),
    onSuccess: res => {
      setLastHeal(res)
      void qc.invalidateQueries({ queryKey: ['market-data', 'ingest'] })
    },
  })

  // Enqueued jobs take a few minutes to drain; one recheck after that instead of
  // asking the reader to remember to click.
  useEffect(() => {
    if (lastHeal == null || lastHeal.enqueued === 0) return
    const t = window.setTimeout(() => void q.refetch(), RECHECK_AFTER_HEAL_MS)
    return () => window.clearTimeout(t)
  }, [lastHeal, q])

  const copy = useCallback(async () => {
    if (report == null) return
    try {
      await navigator.clipboard.writeText(buildDoctorAgentReport(report))
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      // clipboard may be denied in some embeds — the report is still on screen
    }
  }, [report])

  const runFix = () => {
    if (pending == null) return
    heal.mutate(pending.findingIds, { onSettled: () => setPending(null) })
  }

  const verdictTag =
    report != null ? (
      <DenseTag variant={verdictVariant(report.verdict)}>{report.verdict}</DenseTag>
    ) : q.isError ? (
      <DenseTag variant="danger">doctor unreachable</DenseTag>
    ) : null

  return (
    <OpsSection
      title="Doctor"
      description={
        report != null
          ? `Session ${report.session}${report.session_is_today ? ' (today)' : ' (last completed)'} — ${report.summary}. Optionable underlyings: ${report.universe.optionable} of ${report.universe.underlyings}.`
          : 'What the last session should hold vs what it does — each gap with the exact enqueue that fills it.'
      }
      headerExtra={
        <div className="flex flex-wrap items-center gap-2">
          {verdictTag}
          {report != null ? (
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              checked {report.generated_at}
            </span>
          ) : null}
          {q.isError ? (
            <span className="text-[var(--text-dense-caption)] text-[var(--danger)]">
              {q.error instanceof Error ? q.error.message : String(q.error)}
            </span>
          ) : null}
        </div>
      }
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void q.refetch()} disabled={q.isFetching}>
            {q.isFetching ? 'Checking…' : 'Check now'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPending({ label: `every auto-fixable finding (${fixable.length})`, findingIds: null })}
            disabled={!canOperate || heal.isPending || fixable.length === 0}
            title={canOperate ? undefined : 'Operator auth required'}
          >
            {heal.isPending ? 'Fixing…' : `Fix all (${fixable.length})`}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => void copy()} disabled={report == null}>
            {copied ? 'Copied' : 'Copy doctor report for Agent'}
          </Button>
        </div>
      }
      bodyPadding="none"
      collapsible
    >
      {report == null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {q.isFetching ? 'Running the doctor…' : 'No report yet.'}
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHead>
            <DenseTableHeadRow>
              <DenseTableHeader>Sev</DenseTableHeader>
              <DenseTableHeader>Check</DenseTableHeader>
              <DenseTableHeader>Slot</DenseTableHeader>
              <DenseTableHeader>Expected</DenseTableHeader>
              <DenseTableHeader>Actual</DenseTableHeader>
              <DenseTableHeader>Detail</DenseTableHeader>
              <DenseTableHeader>Fix</DenseTableHeader>
              <DenseTableHeader>Action</DenseTableHeader>
            </DenseTableHeadRow>
          </DenseTableHead>
          <DenseTableBody>
            {findings.map(f => (
              <FindingRow
                key={f.id}
                f={f}
                canOperate={canOperate}
                busy={heal.isPending}
                onFix={() => setPending({ label: describeFix(f.fix), findingIds: [f.id] })}
              />
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}

      {lastHeal != null ? (
        <div className="flex flex-wrap items-center gap-2 border-t border-[var(--border)] px-3 py-2 text-[var(--text-dense-meta)]">
          <DenseTag variant={lastHeal.enqueued > 0 ? 'success' : 'neutral'}>
            {lastHeal.enqueued} job(s) enqueued
          </DenseTag>
          <span className="text-[var(--muted-foreground)]">
            {lastHeal.actions.map(a => `${describeFix(a)} → ${typeof a.result === 'string' ? a.result : formatValue(a.result)}`).join(' · ')}
            {lastHeal.enqueued > 0 ? ' — rechecking in a minute; the queue drains first.' : ''}
          </span>
        </div>
      ) : null}
      {heal.isError ? (
        <p className="m-0 border-t border-[var(--border)] px-3 py-2 text-[var(--text-dense-meta)] text-[var(--danger)]">
          {heal.error instanceof Error ? heal.error.message : String(heal.error)}
        </p>
      ) : null}

      <ConfirmDialog
        open={pending != null}
        title="Run doctor prescription"
        message={`${pending?.label ?? ''}. Jobs go into ops_jobs.job_ingest with the session date pinned; workers pick them up asynchronously.`}
        confirmLabel="Fix"
        confirming={heal.isPending}
        onConfirm={runFix}
        onCancel={() => setPending(null)}
      />
    </OpsSection>
  )
}

function FindingRow({
  f,
  canOperate,
  busy,
  onFix,
}: {
  f: DoctorFinding
  canOperate: boolean
  busy: boolean
  onFix: () => void
}) {
  const muted = f.severity === 'ok'
  return (
    <DenseTableRow>
      <DenseTableCell>
        <DenseTag variant={severityVariant(f.severity)}>{f.severity}</DenseTag>
      </DenseTableCell>
      <DenseTableCell className={muted ? 'text-[var(--muted-foreground)]' : undefined}>{f.title}</DenseTableCell>
      <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">{f.slot}</DenseTableCell>
      <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">{formatValue(f.expected)}</DenseTableCell>
      <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">{formatValue(f.actual)}</DenseTableCell>
      <DenseTableCell className="max-w-[28rem] whitespace-normal text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        {f.detail}
        {f.missing_sample != null && f.missing_sample.length > 0 ? (
          <span className="font-mono"> missing: {f.missing_sample.join(', ')}</span>
        ) : null}
      </DenseTableCell>
      <DenseTableCell className="text-[var(--text-dense-caption)]">
        {f.fix != null ? describeFix(f.fix) : ''}
        {f.fix != null && !f.auto_fixable ? <span className="text-[var(--muted-foreground)]"> (manual)</span> : null}
      </DenseTableCell>
      <DenseTableCell>
        {f.fix != null && f.auto_fixable ? (
          <Button
            variant="outline"
            size="sm"
            onClick={onFix}
            disabled={!canOperate || busy}
            title={canOperate ? undefined : 'Operator auth required'}
          >
            Fix
          </Button>
        ) : null}
      </DenseTableCell>
    </DenseTableRow>
  )
}
