import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Button } from '@bifrost/ui'
import { fetchActionCatalog } from '@/api/actions'
import { createApprovalRequest } from '@/api/approvals'
import { authedFetch } from '@/api/client'
import { approvalHref } from '@/lib/shell/consoleRoutes'

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

type Notice =
  | { kind: 'idle' }
  | { kind: 'waiting'; id: string; tier: string }
  | { kind: 'done'; message: string }
  | { kind: 'error'; message: string }

/**
 * B runs `direct` immediately. C and D create an approval and link to its page;
 * approve and reject happen on `#approvals?id=` only.
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
    try {
      const catalog = catalogQuery.data ?? (await catalogQuery.refetch()).data
      const listed = catalog?.find(row => row.id === action)?.tier
      const result = await createApprovalRequest({ action, params, reason, rollback })
      if (result.kind === 'direct') {
        await runDirect()
        return
      }
      if (result.kind === 'pending') {
        setNotice({ kind: 'waiting', id: result.id, tier: result.tier !== '' ? result.tier : (listed ?? '') })
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

  return (
    <span className="inline-flex min-w-0 flex-col items-start gap-1">
      <Button type="button" size="sm" disabled={disabled || busy} onClick={() => void onClick()}>
        {busy ? 'Requesting…' : label}
      </Button>
      {notice.kind === 'waiting' ? (
        <span role="status" className="text-[var(--text-dense-caption)] text-muted-foreground">
          <span>{REQUEST_SUBMITTED_WAITING}</span>
          {notice.tier !== '' ? ` (${notice.tier})` : ''} ·{' '}
          <a href={approvalHref(notice.id)} className="underline">
            Open request
          </a>
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
    </span>
  )
}
