import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Button } from '@bifrost/ui'
import {
  approvalIdFromHash,
  fetchApproval,
  fetchApprovalList,
  formatParams,
  formatTimeRemaining,
  postApprovalDecision,
  readApprovalToken,
  recentClosed,
  writeApprovalToken,
  type ApprovalItem,
} from '@/api/approvals'

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

function TokenField({ token, onSaved }: { token: string; onSaved: (next: string) => void }) {
  const [draft, setDraft] = useState(token)
  return (
    <form
      className="flex w-full min-w-0 flex-col gap-2 sm:flex-row sm:items-end"
      onSubmit={event => {
        event.preventDefault()
        writeApprovalToken(draft)
        onSaved(readApprovalToken())
      }}
    >
      <label className="flex min-w-0 flex-1 flex-col gap-1 text-sm" htmlFor="approval-token">
        Set approval token
        <input
          id="approval-token"
          name="approval-token"
          type="password"
          autoComplete="off"
          spellCheck={false}
          className="w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
          value={draft}
          onChange={event => setDraft(event.target.value)}
        />
      </label>
      <Button type="submit" variant="outline" className="w-full sm:w-auto">
        Save token
      </Button>
    </form>
  )
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="break-words">{value || '—'}</div>
    </div>
  )
}

function ApprovalCard({
  item,
  hasToken,
  open,
  busy,
  onApprove,
  onReject,
}: {
  item: ApprovalItem
  hasToken: boolean
  open: boolean
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
      data-open={open ? 'true' : 'false'}
      aria-current={open ? 'true' : undefined}
      className="flex w-full min-w-0 flex-col gap-3 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-3"
    >
      <div className="grid w-full min-w-0 grid-cols-1 gap-2 sm:grid-cols-2">
        <Field label="Requester" value={item.requester} />
        <Field label="Action" value={item.action} />
        <Field label="Tier" value={item.tier} />
        <Field label="Status" value={item.status} />
        {pending ? (
          <Field label="Time remaining" value={formatTimeRemaining(item.expires_at)} />
        ) : null}
        <Field label="Rollback" value={item.rollback} />
      </div>
      <Field label="Reason" value={item.reason} />
      <details open={open ? true : undefined} className="min-w-0">
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
              className="w-full sm:w-auto"
              disabled={!canDecide}
              aria-label={`Approve ${item.action}`}
              onClick={() => onApprove(item.id)}
            >
              Approve
            </Button>
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto"
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

export function ApprovalsPage() {
  const qc = useQueryClient()
  const deepLinkId = useDeepLinkId()
  const [token, setToken] = useState(() => readApprovalToken())
  const [busyId, setBusyId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const hasToken = token !== ''

  const pendingQ = useQuery({
    queryKey: ['approvals', 'pending'],
    queryFn: () => fetchApprovalList('pending'),
    retry: false,
  })
  const historyQ = useQuery({
    queryKey: ['approvals', 'all'],
    queryFn: () => fetchApprovalList('all'),
    retry: false,
  })
  const itemQ = useQuery({
    queryKey: ['approvals', 'item', deepLinkId],
    queryFn: () => fetchApproval(deepLinkId ?? ''),
    enabled: deepLinkId != null,
    retry: false,
  })

  const pending = (pendingQ.data ?? []).filter(item => item.id !== deepLinkId)
  const closed = recentClosed(historyQ.data ?? []).filter(item => item.id !== deepLinkId)
  const opened = deepLinkId == null ? null : (itemQ.data ?? null)

  async function decide(id: string, path: 'approve' | 'reject', reason?: string) {
    if (!hasToken) return
    setBusyId(id)
    setActionError(null)
    try {
      await postApprovalDecision(
        id,
        token,
        path,
        path === 'approve' ? { channel: 'console' } : { reason: reason ?? '' },
      )
      await qc.invalidateQueries({ queryKey: ['approvals'] })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Approval action failed')
    } finally {
      setBusyId(null)
    }
  }

  const listError = pendingQ.error ?? historyQ.error

  return (
    <div data-testid="approvals-page" className="flex w-full min-w-0 flex-col gap-4">
      <TokenField token={token} onSaved={setToken} />
      {hasToken ? (
        <p className="text-sm text-muted-foreground">Approval token saved in this browser.</p>
      ) : (
        <p role="status" className="text-sm">
          {MISSING_TOKEN}
        </p>
      )}
      {actionError != null ? <p className="text-sm text-destructive">{actionError}</p> : null}
      {listError != null ? (
        <p className="text-sm text-destructive">
          {listError instanceof Error ? listError.message : 'Could not load approvals'}
        </p>
      ) : null}

      {deepLinkId != null ? (
        <section aria-label={`Approval ${deepLinkId}`} className="flex w-full min-w-0 flex-col gap-2">
          <h2 className="text-sm font-medium">Open request</h2>
          {itemQ.isLoading ? <p className="text-sm">Opening {deepLinkId}…</p> : null}
          {itemQ.error != null ? (
            <p className="text-sm text-destructive">
              {itemQ.error instanceof Error ? itemQ.error.message : 'Could not open this request'}
            </p>
          ) : null}
          {opened != null ? (
            <ApprovalCard
              item={opened}
              hasToken={hasToken}
              open
              busy={busyId === opened.id}
              onApprove={id => void decide(id, 'approve')}
              onReject={(id, reason) => void decide(id, 'reject', reason)}
            />
          ) : null}
        </section>
      ) : null}

      <section aria-label="Pending requests" className="flex w-full min-w-0 flex-col gap-2">
        <h2 className="text-sm font-medium">Pending</h2>
        {pendingQ.isLoading ? <p className="text-sm">Loading pending requests…</p> : null}
        {!pendingQ.isLoading && pending.length === 0 ? (
          <p className="text-sm text-muted-foreground">No pending requests.</p>
        ) : null}
        {pending.map(item => (
          <ApprovalCard
            key={item.id}
            item={item}
            hasToken={hasToken}
            open={false}
            busy={busyId === item.id}
            onApprove={id => void decide(id, 'approve')}
            onReject={(id, reason) => void decide(id, 'reject', reason)}
          />
        ))}
      </section>

      <section aria-label="Closed requests" className="flex w-full min-w-0 flex-col gap-2">
        <h2 className="text-sm font-medium">Closed</h2>
        {closed.length === 0 && !historyQ.isLoading ? (
          <p className="text-sm text-muted-foreground">No closed requests.</p>
        ) : null}
        {closed.map(item => (
          <ApprovalCard
            key={item.id}
            item={item}
            hasToken={hasToken}
            open={false}
            busy={false}
            onApprove={() => undefined}
            onReject={() => undefined}
          />
        ))}
      </section>
    </div>
  )
}
