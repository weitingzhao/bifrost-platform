import type { RemediationHealthResponse, RemediationJob, RemediationJobsResponse, StartRemediationRequest } from './remediationTypes'
import { authedFetch, operatorToken, parseError } from './client'

/**
 * Start a remediation job. When platform-api answers 409 because a job with the
 * same scope is already running (TD-224), resolve to that job so the caller
 * attaches to it in the Operator Dock instead of starting a duplicate.
 */
export async function startRemediation(body: StartRemediationRequest): Promise<RemediationJob> {
  const token = operatorToken()
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch('/api/v1/remediation/start', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (r.status === 409) {
    const conflict = (await r
      .clone()
      .json()
      .catch(() => null)) as { job?: RemediationJob } | null
    if (conflict?.job?.id != null && conflict.job.id !== '') return conflict.job
  }
  if (!r.ok) throw await parseError('remediation start', r)
  return r.json() as Promise<RemediationJob>
}

export async function fetchRemediationJob(id: string): Promise<RemediationJob> {
  const r = await authedFetch('remediation job', `/api/v1/remediation/${encodeURIComponent(id)}`)
  return r.json() as Promise<RemediationJob>
}

export async function fetchRemediationJobs(opts?: unknown): Promise<RemediationJobsResponse> {
  // Accept `{ limit }` from call sites, or TanStack QueryFunctionContext when used as queryFn.
  let limit = 80
  if (opts != null && typeof opts === 'object' && 'limit' in opts) {
    const n = (opts as { limit?: unknown }).limit
    if (typeof n === 'number' && Number.isFinite(n)) limit = n
  }
  const q = new URLSearchParams({ limit: String(limit) })
  const r = await authedFetch('remediation jobs', `/api/v1/remediation/?${q.toString()}`)
  return r.json() as Promise<RemediationJobsResponse>
}

export async function cancelRemediationJob(id: string): Promise<RemediationJob> {
  const r = await authedFetch('remediation cancel', `/api/v1/remediation/${encodeURIComponent(id)}/cancel`, {
    method: 'POST',
  })
  return r.json() as Promise<RemediationJob>
}

export async function respondRemediationJob(
  id: string,
  optionId: string,
  note?: string,
  commitMessage?: string,
): Promise<void> {
  const payload: Record<string, string> = { option_id: optionId, note: note ?? '' }
  if (commitMessage != null && commitMessage.trim() !== '') {
    payload.commit_message = commitMessage.trim()
  }
  const r = await authedFetch('remediation respond', `/api/v1/remediation/${encodeURIComponent(id)}/respond`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
  if (!r.ok) throw await parseError('remediation respond', r)
}

export function remediationStreamUrl(id: string): string {
  return `/api/v1/remediation/${encodeURIComponent(id)}/stream`
}

export async function fetchRemediationHealth(): Promise<RemediationHealthResponse> {
  const r = await fetch('/api/v1/remediation/health')
  if (!r.ok) {
    const body = (await r.json().catch(() => ({}))) as RemediationHealthResponse
    return { status: 'unavailable', error: body.error ?? r.statusText }
  }
  return r.json() as Promise<RemediationHealthResponse>
}

