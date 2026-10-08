import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Button } from '@bifrost/ui'
import { fetchActionCatalog } from '@/api/actions'
import {
  createApprovalRequest,
  postApprovalDecision,
  readApprovalToken,
  type ApprovalCreateResult,
} from '@/api/approvals'
import { authedFetch } from '@/api/client'

export const REQUEST_SUBMITTED_WAITING = 'Request submitted, waiting for approval'

export type RequestActionDirect = {
  method: 'POST' | 'PUT' | 'DELETE'
  path: string
  body?: unknown
}

export type RequestActionButtonProps = {
  /** Catalog id. The tier comes from GET /api/v1/actions and the create response. */
  action: string
  params?: Record<string, unknown>
  reason: string
  rollback?: string
  label: string
  /** Called only when the approval API classifies the action as tier B. */
  direct: RequestActionDirect
  disabled?: boolean
}

type ConfirmState = {
  id: string
  tier: string
  action: string
}

type Notice =
  | { kind: 'idle' }
  | { kind: 'waiting' }
  | { kind: 'done'; message: string }
  | { kind: 'error'; message: string }

function tierLabel(result: ApprovalCreateResult, catalogTier: string | undefined): string {
  if (result.kind === 'error') return result.tier ?? catalogTier ?? ''
  if (result.tier !== '') return result.tier
  return catalogTier ?? ''
}

/**
 * B runs `direct` immediately. C and D create an approval. When this browser
 * holds an approval token, the confirm dialog approves with channel "console".
 * Without a token, the request waits.
 */
export function RequestActionButton({
  action,
  params,
  reason,
  rollback,
  label,
  direct,
  disabled = false,
}: RequestActionButtonProps) {
  const catalogQuery = useQuery({
    queryKey: ['actions', 'catalog'],
    queryFn: fetchActionCatalog,
    staleTime: 60_000,
  })
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<Notice>({ kind: 'idle' })
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)

  async function runDirect() {
    await authedFetch(action, direct.path, {
      method: direct.method,
      body: direct.body === undefined ? undefined : JSON.stringify(direct.body),
    })
    setNotice({ kind: 'done', message: 'Done' })
  }

  async function onClick() {
    if (busy || disabled) return
    setBusy(true)
    setNotice({ kind: 'idle' })
    setConfirm(null)
    try {
      const catalog = catalogQuery.data ?? (await catalogQuery.refetch()).data
      const listed = catalog?.find(row => row.id === action)?.tier
      const result = await createApprovalRequest({ action, params, reason, rollback })
      if (result.kind === 'direct') {
        await runDirect()
        return
      }
      if (result.kind === 'pending') {
        const tier = tierLabel(result, listed)
        const token = readApprovalToken()
        if (token !== '') {
          setConfirm({ id: result.id, tier, action: result.action })
        } else {
          setNotice({ kind: 'waiting' })
        }
        return
      }
      setNotice({ kind: 'error', message: result.error })
    } catch (err) {
      setNotice({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Request failed',
      })
    } finally {
      setBusy(false)
    }
  }

  async function onApprove() {
    if (confirm == null) return
    const token = readApprovalToken()
    if (token === '') {
      setConfirm(null)
      setNotice({ kind: 'waiting' })
      return
    }
    setBusy(true)
    try {
      await postApprovalDecision(confirm.id, token, 'approve', { channel: 'console' })
      setConfirm(null)
      setNotice({ kind: 'done', message: 'Approved' })
    } catch (err) {
      setConfirm(null)
      setNotice({
        kind: 'error',
        message: err instanceof Error ? err.message : 'Approve failed',
      })
    } finally {
      setBusy(false)
    }
  }

  const confirmTier = confirm != null && confirm.tier !== '' ? ` (${confirm.tier})` : ''

  return (
    <span className="inline-flex min-w-0 flex-col items-start gap-1">
      <Button type="button" size="sm" disabled={disabled || busy} onClick={() => void onClick()}>
        {busy ? 'Requesting…' : label}
      </Button>
      {notice.kind === 'waiting' ? (
        <span role="status" className="text-[var(--text-dense-caption)] text-muted-foreground">
          {REQUEST_SUBMITTED_WAITING}
        </span>
      ) : null}
      {notice.kind === 'done' ? (
        <span role="status" className="text-[var(--text-dense-caption)] text-muted-foreground">
          {notice.message}
        </span>
      ) : null}
      {notice.kind === 'error' ? (
        <span role="alert" className="text-[var(--text-dense-caption)] text-destructive">
          {notice.message}
        </span>
      ) : null}
      {confirm != null ? (
        <div
          role="dialog"
          aria-labelledby="request-action-approve-title"
          className="flex flex-col gap-2 rounded-md border border-[var(--table-rule)] bg-card p-3"
        >
          <h2 id="request-action-approve-title" className="m-0 text-sm font-semibold">
            Approve this request?
          </h2>
          <p className="m-0 text-sm text-muted-foreground">
            {`${confirm.action}${confirmTier}. ${reason}`}
          </p>
          <span className="flex gap-2">
            <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => {
              setConfirm(null)
              setNotice({ kind: 'waiting' })
            }}>
              Cancel
            </Button>
            <Button type="button" size="sm" disabled={busy} onClick={() => void onApprove()}>
              {busy ? 'Requesting…' : 'Approve'}
            </Button>
          </span>
        </div>
      ) : null}
    </span>
  )
}
