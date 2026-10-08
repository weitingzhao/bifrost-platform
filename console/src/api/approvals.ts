import { authHeaders, authedFetch, parseError } from '@/api/client'

/** Browser-only admin token for approve / reject. Not the console operator token. */
export const APPROVAL_TOKEN_STORAGE_KEY = 'bifrost-ops-approval-token'

export const APPROVAL_HISTORY_LIMIT = 50

export type ApprovalItem = {
  id: string
  action: string
  tier: string
  params: unknown
  reason: string
  rollback: string
  requester: string
  status: string
  expires_at: string
  result?: unknown
  error?: string
}

export type ApprovalDecision = {
  status?: string
  result?: unknown
  error?: string
}

function approvalTokenStore(): Storage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

export function readApprovalToken(): string {
  return approvalTokenStore()?.getItem(APPROVAL_TOKEN_STORAGE_KEY)?.trim() ?? ''
}

export function writeApprovalToken(value: string): void {
  const store = approvalTokenStore()
  if (store == null) return
  const next = value.trim()
  if (next === '') store.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
  else store.setItem(APPROVAL_TOKEN_STORAGE_KEY, next)
}

/** `#approvals?id=<id>` — the phone notification target. */
export function approvalIdFromHash(hash: string): string | null {
  const raw = hash.replace(/^#/, '')
  const q = raw.indexOf('?')
  if (q < 0) return null
  if (raw.slice(0, q) !== 'approvals') return null
  const id = new URLSearchParams(raw.slice(q + 1)).get('id')?.trim() ?? ''
  return id === '' ? null : id
}

export function formatTimeRemaining(expiresAt: string, now = Date.now()): string {
  const end = Date.parse(expiresAt)
  if (!Number.isFinite(end)) return 'unknown expiry'
  const ms = end - now
  if (ms <= 0) return 'expired'
  const totalMin = Math.floor(ms / 60_000)
  const hours = Math.floor(totalMin / 60)
  const mins = totalMin % 60
  if (hours === 0) return `${mins}m left`
  if (mins === 0) return `${hours}h left`
  return `${hours}h ${mins}m left`
}

export function formatParams(params: unknown): string {
  if (params == null || params === '') return '—'
  if (typeof params === 'string') return params
  try {
    return JSON.stringify(params, null, 2)
  } catch {
    return '—'
  }
}

function isItem(value: unknown): value is ApprovalItem {
  return value != null && typeof value === 'object' && typeof (value as ApprovalItem).id === 'string'
}

/** Accepts `{ items: [...] }` or a bare array. */
export function parseApprovalList(body: unknown): ApprovalItem[] {
  const raw = Array.isArray(body)
    ? body
    : body != null && typeof body === 'object' && Array.isArray((body as { items?: unknown }).items)
      ? (body as { items: unknown[] }).items
      : null
  if (raw == null) throw new Error('approvals: unexpected list shape')
  return raw.filter(isItem)
}

/** Closed requests, newest expiry first, capped at the history window. */
export function recentClosed(items: ApprovalItem[], limit = APPROVAL_HISTORY_LIMIT): ApprovalItem[] {
  return items
    .filter(item => item.status !== 'pending')
    .sort((a, b) => Date.parse(b.expires_at) - Date.parse(a.expires_at))
    .slice(0, limit)
}

export async function fetchApprovalList(status: 'pending' | 'all'): Promise<ApprovalItem[]> {
  const r = await authedFetch('approvals', `/api/v1/approvals?status=${status}`)
  return parseApprovalList(await r.json())
}

export async function fetchApproval(id: string): Promise<ApprovalItem> {
  const r = await authedFetch('approvals', `/api/v1/approvals/${encodeURIComponent(id)}`)
  const body: unknown = await r.json()
  if (!isItem(body)) throw new Error('approvals: unexpected item')
  return body
}

export async function postApprovalDecision(
  id: string,
  token: string,
  path: 'approve' | 'reject',
  body: { channel: 'console' } | { reason: string },
): Promise<ApprovalDecision> {
  const headers = new Headers()
  headers.set('Content-Type', 'application/json')
  headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(`/api/v1/approvals/${encodeURIComponent(id)}/${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('approvals', r)
  return (await r.json()) as ApprovalDecision
}

export type ApprovalCreateBody = {
  action: string
  params?: Record<string, unknown>
  reason: string
  rollback?: string
}

/** Classified outcome of POST /api/v1/approvals. Tier B is `direct` (call the route). */
export type ApprovalCreateResult =
  | { kind: 'direct'; action: string; tier: string }
  | { kind: 'pending'; id: string; action: string; tier: string; status: string }
  | { kind: 'error'; status: number; error: string; tier?: string; action?: string }

function asRecord(value: unknown): Record<string, unknown> {
  if (value != null && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

/**
 * Create an approval. A 400 `call directly` means the classified tier is B
 * and the caller should hit the action's own endpoint. Does not throw on that
 * response — the tier lives in the body, not in a frontend table.
 */
export async function createApprovalRequest(body: ApprovalCreateBody): Promise<ApprovalCreateResult> {
  const r = await fetch('/api/v1/approvals', {
    method: 'POST',
    headers: authHeaders(true),
    body: JSON.stringify({
      action: body.action,
      params: body.params ?? {},
      reason: body.reason,
      rollback: body.rollback ?? '',
    }),
  })
  let parsed: Record<string, unknown>
  try {
    parsed = asRecord(await r.json())
  } catch {
    parsed = {}
  }
  const tier = typeof parsed.tier === 'string' ? parsed.tier : undefined
  const action = typeof parsed.action === 'string' ? parsed.action : body.action
  const error = typeof parsed.error === 'string' ? parsed.error : ''
  if (r.status === 400 && error === 'call directly') {
    return { kind: 'direct', action, tier: tier ?? '' }
  }
  if (r.status === 201 && typeof parsed.id === 'string') {
    return {
      kind: 'pending',
      id: parsed.id,
      action,
      tier: tier ?? '',
      status: typeof parsed.status === 'string' ? parsed.status : 'pending',
    }
  }
  return {
    kind: 'error',
    status: r.status,
    error: error !== '' ? error : `HTTP ${r.status}`,
    tier,
    action,
  }
}
