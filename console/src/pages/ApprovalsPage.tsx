import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Button } from '@bifrost/ui'
import { authedFetch } from '@/api/client'
import {
  approvalEnv,
  approvalIdFromHash,
  fetchApproval,
  formatDecidedAt,
  formatParams,
  formatTimeRemaining,
  formatWaited,
  postApprovalDecision,
  readApprovalToken,
  type ApprovalItem,
} from '@/api/approvals'
import { ApprovalTokenField } from '@/pages/shell/needs-you/ApprovalTokenField'
import { NEEDS_YOU_REFRESH_MS } from '@/pages/shell/needs-you/useNeedsYou'

const MISSING_TOKEN =
  'Approve and Reject stay disabled until an approval token is saved in this browser. The console operator token is not used for these actions.'

function useDeepLinkId(): string | null {
  const [id, setId] = useState(() => approvalIdFromHash(window.location.hash))
  useEffect(() => {
    const sync = () => setId(approvalIdFromHash(window.location.hash))
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  return id
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="break-words">{value || '—'}</div>
    </div>
  )
}

function ActuationDetail({ item }: { item: ApprovalItem }) {
  const planId =
    item.action === 'apply_manifest' && typeof item.params.plan_id === 'string' ? item.params.plan_id : ''
  const command =
    item.action === 'owner_run_command' && typeof item.params.command === 'string' ? item.params.command : ''
  const plan = useQuery({
    queryKey: ['actuation-plan', planId],
    enabled: planId !== '',
    queryFn: async () => {
      const response = await authedFetch(
        'Plan',
        `/api/v1/actuation/manifests/plans/${encodeURIComponent(planId)}`,
      )
      return (await response.json()) as {
        objects?: unknown
        policy?: string
        ready?: boolean
        logs?: string
      }
    },
  })
  if (command !== '') return <Field label="Command" value={command} />
  const pipelineParams =
    item.action === 'start_pipeline_run' && item.params.params != null && typeof item.params.params === 'object'
      ? (item.params.params as Record<string, unknown>)
      : null
  if (pipelineParams != null) {
    return (
      <>
        {Object.entries(pipelineParams).map(([key, value]) => (
          <Field key={key} label={key} value={value == null ? '' : String(value)} />
        ))}
      </>
    )
  }
  if (planId === '') return null
  const summary = plan.data
  const status =
    summary == null
      ? planId
      : `${planId} · ${summary.policy ?? 'pending'} · ${summary.ready ? 'ready' : 'not ready'}`
  return (
    <>
      <Field label="Plan" value={status} />
      {summary?.logs ? <Field label="Plan logs" value={summary.logs} /> : null}
      {summary?.objects != null ? (
        <pre className="overflow-x-auto text-xs">{JSON.stringify(summary.objects, null, 2)}</pre>
      ) : null}
    </>
  )
}

function ApprovalCard({
  item,
  hasToken,
  busy,
  onApprove,
  onReject,
}: {
  item: ApprovalItem
  hasToken: boolean
  busy: boolean
  onApprove: (id: string) => void
  onReject: (id: string, reason: string) => void
}) {
  const [reason, setReason] = useState('')
  const pending = item.status === 'pending'
  const canDecide = pending && hasToken && !busy
  const trimmed = reason.trim()
  return (
    <article
      id={`approval-${item.id}`}
      data-open="true"
      aria-current="true"
      className="flex w-full min-w-0 flex-col gap-3 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-3"
    >
      <h2 className="m-0 break-all text-sm font-medium" data-approval-ref>
        Request {item.id}
      </h2>
      <div className="grid w-full min-w-0 grid-cols-1 gap-2 sm:grid-cols-2">
        <Field label="Requester" value={item.requester} />
        <Field label="Action" value={item.action} />
        <Field label="Tier" value={item.tier} />
        <Field label="Environment" value={approvalEnv(item)} />
        <Field label="Status" value={item.status} />
        {pending ? (
          <Field
            label="Waited · time remaining"
            value={`${formatWaited(item.created_at)} · ${formatTimeRemaining(item.expires_at)}`}
          />
        ) : (
          <Field label="Decided at" value={formatDecidedAt(item)} />
        )}
        <Field label="Rollback" value={item.rollback ?? ''} />
      </div>
      <Field label="Reason" value={item.reason} />
      <ActuationDetail item={item} />
      <details open className="min-w-0">
        <summary className="cursor-pointer text-sm">Params</summary>
        <pre className="mt-1 max-w-full whitespace-pre-wrap break-all text-xs">
          {formatParams(item.params)}
        </pre>
      </details>
      {!pending ? (
        <Field
          label={item.error ? 'Error' : 'Result'}
          value={item.error ? item.error : formatParams(item.result)}
        />
      ) : (
        <div className="flex w-full min-w-0 flex-col gap-2">
          <label className="flex min-w-0 flex-col gap-1 text-sm" htmlFor={`reject-${item.id}`}>
            Reject reason
            <input
              id={`reject-${item.id}`}
              className="w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
              value={reason}
              onChange={event => setReason(event.target.value)}
            />
          </label>
          <div className="flex w-full min-w-0 flex-col gap-2 sm:flex-row">
            <Button
              type="button"
              className="h-11 w-full sm:h-auto sm:w-auto"
              disabled={!canDecide}
              aria-label={`Approve ${item.action}`}
              onClick={() => onApprove(item.id)}
            >
              Approve
            </Button>
            <Button
              type="button"
              variant="outline"
              className="h-11 w-full sm:h-auto sm:w-auto"
              disabled={!canDecide || trimmed === ''}
              aria-label={`Reject ${item.action}`}
              onClick={() => onReject(item.id, trimmed)}
            >
              Reject
            </Button>
          </div>
        </div>
      )}
    </article>
  )
}

/** The request page, `#approvals?id=<id>`: the only place to approve or reject. */
export function ApprovalsPage() {
  const qc = useQueryClient()
  const id = useDeepLinkId()
  const [token, setToken] = useState(() => readApprovalToken())
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const hasToken = token !== ''

  const itemQ = useQuery({
    queryKey: ['approvals', 'item', id],
    queryFn: () => fetchApproval(id ?? ''),
    enabled: id != null,
    retry: false,
    refetchInterval: query => (query.state.data?.status === 'pending' ? NEEDS_YOU_REFRESH_MS : false),
  })
  const opened = id == null ? null : (itemQ.data ?? null)

  async function decide(requestId: string, path: 'approve' | 'reject', reason?: string) {
    if (!hasToken) return
    setBusy(true)
    setActionError(null)
    try {
      await postApprovalDecision(
        requestId,
        token,
        path,
        path === 'approve'
          ? {
              channel: 'console',
              approval_line: opened?.approval_line ?? '',
              params_hash: opened?.params_hash ?? '',
            }
          : { reason: reason ?? '' },
      )
      await qc.invalidateQueries({ queryKey: ['approvals'] })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Approval action failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div data-testid="approvals-page" className="flex w-full min-w-0 max-w-3xl flex-col gap-4">
      <a href="#needs-you" className="text-sm text-muted-foreground no-underline hover:text-foreground">
        ← Needs you
      </a>
      {hasToken ? (
        <details className="min-w-0 text-sm text-muted-foreground">
          <summary className="cursor-pointer">Approval token saved in this browser.</summary>
          <div className="mt-2">
            <ApprovalTokenField token={token} onSaved={setToken} />
          </div>
        </details>
      ) : (
        <div className="flex w-full min-w-0 flex-col gap-2">
          <p role="status" className="m-0 text-sm">
            {MISSING_TOKEN}
          </p>
          <ApprovalTokenField token={token} onSaved={setToken} />
        </div>
      )}
      {actionError != null ? <p className="m-0 text-sm text-destructive">{actionError}</p> : null}

      {id == null ? (
        <p className="m-0 text-sm text-muted-foreground">No request named in the link.</p>
      ) : (
        <section aria-label={`Approval ${id}`} className="flex w-full min-w-0 flex-col gap-2">
          {itemQ.isLoading ? <p className="m-0 text-sm">Opening {id}…</p> : null}
          {itemQ.error != null ? (
            <p className="m-0 text-sm text-destructive">
              {itemQ.error instanceof Error ? itemQ.error.message : 'Could not open this request'}
            </p>
          ) : null}
          {opened != null ? (
            <ApprovalCard
              key={opened.id}
              item={opened}
              hasToken={hasToken}
              busy={busy}
              onApprove={requestId => void decide(requestId, 'approve')}
              onReject={(requestId, reason) => void decide(requestId, 'reject', reason)}
            />
          ) : null}
        </section>
      )}
    </div>
  )
}
