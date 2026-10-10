import { authHeaders, authedFetch, operatorToken, parseError } from '@/api/client'
import {
  GO_ZERO_DECIDED_AT,
  type PlatformApprovalRecord,
} from '@/api/approvalsServerContract'

/** Browser-only admin token for approve / reject. Not the console operator token. */
export const APPROVAL_TOKEN_STORAGE_KEY = 'bifrost-ops-approval-token'

export const APPROVAL_HISTORY_LIMIT = 50

export type ApprovalItem = PlatformApprovalRecord

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
  const page = raw.slice(0, q)
  // #maintenance?id= is the old link; the shell rewrites it to #approvals?id=.
  if (page !== 'approvals' && page !== 'maintenance') return null
  const id = new URLSearchParams(raw.slice(q + 1)).get('id')?.trim() ?? ''
  return id === '' ? null : id
}

export function isUnsetDecidedAt(value: string | undefined): boolean {
  if (value == null || value === '') return true
  if (value === GO_ZERO_DECIDED_AT) return true
  const ms = Date.parse(value)
  return !Number.isFinite(ms) || ms <= 0
}

/** Human label for decided_at; pending / Go zero time must not show year 0001. */
export function formatDecidedAt(item: Pick<ApprovalItem, 'status' | 'decided_at'>): string {
  if (item.status === 'pending' || isUnsetDecidedAt(item.decided_at)) return 'Pending'
  const ms = Date.parse(item.decided_at!)
  if (!Number.isFinite(ms)) return '—'
  return new Date(ms).toLocaleString()
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

/** How long a request has waited since it was created: `3d 4h`, `5h 12m`, `12m`. */
export function formatWaited(createdAt: string, now = Date.now()): string {
  const start = Date.parse(createdAt)
  if (!Number.isFinite(start)) return 'unknown'
  const totalMin = Math.max(0, Math.floor((now - start) / 60_000))
  const days = Math.floor(totalMin / 1440)
  const hours = Math.floor((totalMin % 1440) / 60)
  const mins = totalMin % 60
  if (days > 0) return hours === 0 ? `${days}d` : `${days}d ${hours}h`
  if (hours > 0) return mins === 0 ? `${hours}h` : `${hours}h ${mins}m`
  return `${mins}m`
}

/** Environment named in the request params, if any. */
export function approvalEnv(item: Pick<ApprovalItem, 'params'>): string {
  for (const key of ['env', 'environment']) {
    const value = item.params?.[key]
    if (typeof value === 'string' && value.trim() !== '') return value.trim()
  }
  return ''
}

/** Still waiting for a decision: pending and not past its expiry. */
export function isAwaitingDecision(item: ApprovalItem, now = Date.now()): boolean {
  if (item.status !== 'pending') return false
  const end = Date.parse(item.expires_at)
  return !Number.isFinite(end) || end > now
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

/** Accepts `{ approvals: [...] }` (platform-api) or a bare array. */
export function parseApprovalList(body: unknown): ApprovalItem[] {
  const raw = Array.isArray(body)
    ? body
    : body != null &&
        typeof body === 'object' &&
        Array.isArray((body as { approvals?: unknown }).approvals)
      ? (body as { approvals: unknown[] }).approvals
      : null
  if (raw == null) throw new Error('approvals: unexpected list shape')
  return raw.filter(isItem)
}

function closedSortKey(item: ApprovalItem): number {
  if (!isUnsetDecidedAt(item.decided_at)) return Date.parse(item.decided_at!)
  return Date.parse(item.expires_at)
}

/** Closed requests, newest decision/expiry first, capped at the history window. */
export function recentClosed(rows: ApprovalItem[], limit = APPROVAL_HISTORY_LIMIT): ApprovalItem[] {
  return rows
    .filter(item => item.status !== 'pending')
    .sort((a, b) => closedSortKey(b) - closedSortKey(a))
    .slice(0, limit)
}

/**
 * Reads need a viewer token. The console operator token wins; a device that only
 * saved the approval token (the phone home-screen app) reads with that one, so the
 * token is entered once.
 */
export async function viewerRead(prefix: string, path: string): Promise<Response> {
  if (operatorToken() !== '') return authedFetch(prefix, path)
  const headers = new Headers()
  const token = readApprovalToken()
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(path, { headers })
  if (!r.ok) throw await parseError(prefix, r)
  return r
}

function approvalsRead(path: string): Promise<Response> {
  return viewerRead('approvals', path)
}

export async function fetchApprovalList(status: 'pending' | 'all'): Promise<ApprovalItem[]> {
  const r = await approvalsRead(`/api/v1/approvals?status=${status}`)
  return parseApprovalList(await r.json())
}

export async function fetchApproval(id: string): Promise<ApprovalItem> {
  const r = await approvalsRead(`/api/v1/approvals/${encodeURIComponent(id)}`)
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
