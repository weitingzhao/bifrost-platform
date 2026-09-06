/**
 * Self-check — the panel an operator reads when the plugin looks wrong.
 *
 * One call to GET /flex/ops/check (never IB), one verdict per kind with the
 * sentence that explains it, the time anything next happens, and the buttons
 * that apply right now. A disabled button says why. This is the difference
 * between "it's red, wait for the schedule" and "it retries at 07:32, or press
 * Run now".
 */
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import {
  fetchFlexOpsCheck,
  isProxyError,
  runFlexCheckAction,
  type FlexCheckAction,
  type FlexCheckKind,
  type FlexCheckResponse,
} from '@/api/flexQueryPlugin'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  checkVerdictVariant,
  fmtUntil,
} from '@/components/flex-query/flexQueryStatusUtils'

export const FLEX_CHECK_QUERY_KEY = ['flex-query', 'ops', 'check'] as const

function fmtStamp(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, { weekday: 'short', hour: '2-digit', minute: '2-digit' })
}

function KindRow({
  k,
  canOperate,
  busy,
  onAction,
}: {
  k: FlexCheckKind
  canOperate: boolean
  busy: boolean
  onAction: (k: FlexCheckKind, a: FlexCheckAction) => void
}) {
  return (
    <li className="rounded-md border border-[var(--border)] bg-[var(--background)] px-2.5 py-1.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs">{k.kind}</span>
        <DenseTag variant={checkVerdictVariant(k.verdict)}>{k.verdict}</DenseTag>
        {k.next_in_seconds != null ? (
          <span
            className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]"
            title={k.next_at ?? undefined}
          >
            next {fmtUntil(k.next_in_seconds)}
          </span>
        ) : null}
        {k.job?.id != null ? (
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            job #{k.job.id}
            {k.job.manual ? ' (manual)' : ''}
            {k.job.attempts != null ? ` · ${k.job.attempts}/${k.job.max_attempts ?? '?'}` : ''}
          </span>
        ) : null}
        <span className="ml-auto flex flex-wrap items-center gap-1.5">
          {k.actions.map(a => (
            <Button
              key={a.id}
              size="sm"
              variant={a.id === 'run_now' ? 'default' : 'outline'}
              disabled={!a.enabled || !canOperate || busy}
              title={
                a.reason ??
                (canOperate ? `${a.method} ${a.path}` : 'Operator auth required')
              }
              onClick={() => onAction(k, a)}
            >
              {a.label}
            </Button>
          ))}
        </span>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)]">{k.headline}</p>
      {k.detail ? (
        <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {k.detail}
        </p>
      ) : null}
      {k.actions.some(a => !a.enabled && a.reason) ? (
        <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-[var(--color-warning,#f59e0b)]">
          {k.actions
            .filter(a => !a.enabled && a.reason)
            .map(a => `${a.label}: ${a.reason}`)
            .join(' · ')}
        </p>
      ) : null}
      {k.last_success_at ? (
        <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          last success {fmtStamp(k.last_success_at)}
        </p>
      ) : null}
    </li>
  )
}

export function FlexCheckPanel() {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const q = useQuery({
    queryKey: FLEX_CHECK_QUERY_KEY,
    queryFn: fetchFlexOpsCheck,
    refetchInterval: 60_000,
    retry: 1,
  })
  const [pendingAction, setPendingAction] = useState<{ k: FlexCheckKind; a: FlexCheckAction } | null>(
    null,
  )
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  const raw = q.data
  const err = raw != null && isProxyError(raw) ? raw.error : null
  const check: FlexCheckResponse | null = raw != null && !isProxyError(raw) ? raw : null

  async function perform(k: FlexCheckKind, a: FlexCheckAction) {
    setBusy(true)
    setMsg(null)
    try {
      const res = await runFlexCheckAction(a)
      if (isProxyError(res)) {
        setFailed(true)
        setMsg(`${k.kind} · ${a.label}: ${res.error}`)
      } else {
        setFailed(false)
        const detail =
          res.message ??
          (res.deduped
            ? 'deduped — a job for today is already queued'
            : res.job_id != null
              ? `job ${res.job_id}`
              : 'done')
        setMsg(`${k.kind} · ${a.label}: ${detail}`)
        void queryClient.invalidateQueries({ queryKey: ['flex-query'] })
      }
    } catch (e) {
      setFailed(true)
      setMsg(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
      setPendingAction(null)
    }
  }

  function onAction(k: FlexCheckKind, a: FlexCheckAction) {
    // Run now only clears a wait; enqueue writes a job and gets the usual confirm.
    if (a.id === 'run_now') void perform(k, a)
    else setPendingAction({ k, a })
  }

  return (
    <OpsSection
      title="Self-check"
      description={check ? check.next_step : 'What is wrong, what happens next, what to press'}
      leading={
        check ? <DenseTag variant={checkVerdictVariant(check.verdict)}>{check.verdict}</DenseTag> : null
      }
      headerExtra={
        <span className="flex items-center gap-1.5">
          {check ? (
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {fmtStamp(check.generated_at)} · {check.timezone}
            </span>
          ) : null}
          <Button
            size="sm"
            variant="outline"
            disabled={q.isFetching}
            title="Re-read queue, freshness, worker heartbeat, plan and tokens (no IB request)"
            onClick={() => void q.refetch()}
          >
            {q.isFetching ? 'Checking…' : 'Check now'}
          </Button>
        </span>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {q.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Checking…</p>
      ) : err != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {err} — the plugin API is unreachable or predates 0.6.1.
        </p>
      ) : check == null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">—</p>
      ) : (
        <div className="flex flex-col gap-1.5">
          <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
            {check.kinds.map(k => (
              <KindRow key={k.kind} k={k} canOperate={canOperate} busy={busy} onAction={onAction} />
            ))}
          </ul>
          <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
            {check.checks.map(c => (
              <li key={c.id} className="flex items-start gap-2 text-[var(--text-dense-caption)]">
                <DenseTag variant={c.ok ? 'success' : 'warning'}>{c.id}</DenseTag>
                <span className={c.ok ? 'text-[var(--muted-foreground)]' : ''}>{c.detail}</span>
              </li>
            ))}
          </ul>
          {!canOperate ? (
            <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Authenticate as operator to press an action.
            </p>
          ) : null}
          {msg != null ? (
            <p
              className={`m-0 text-[var(--text-dense-meta)] ${
                failed ? 'text-[var(--destructive)]' : 'text-[var(--success)]'
              }`}
            >
              {msg}
            </p>
          ) : null}
        </div>
      )}
      <ConfirmDialog
        open={pendingAction != null}
        title={pendingAction ? `${pendingAction.a.label} — ${pendingAction.k.kind}` : ''}
        message={
          pendingAction
            ? `${pendingAction.a.method} ${pendingAction.a.path}${
                pendingAction.a.body ? ` ${JSON.stringify(pendingAction.a.body)}` : ''
              }. The worker picks it up asynchronously and fetches from IB.`
            : ''
        }
        confirmLabel="Confirm"
        confirming={busy}
        onConfirm={() => {
          if (pendingAction) void perform(pendingAction.k, pendingAction.a)
        }}
        onCancel={() => setPendingAction(null)}
      />
    </OpsSection>
  )
}
