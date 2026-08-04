import type { RemediationHealthResponse, RemediationJob, RemediationJobsResponse, StartRemediationRequest } from './remediationTypes'
import { authedFetch, parseError } from './client'

export async function startRemediation(body: StartRemediationRequest): Promise<RemediationJob> {
  const r = await authedFetch('remediation start', '/api/v1/remediation/start', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<RemediationJob>
}

export async function fetchRemediationJob(id: string): Promise<RemediationJob> {
  const r = await authedFetch('remediation job', `/api/v1/remediation/${encodeURIComponent(id)}`)
  return r.json() as Promise<RemediationJob>
}

export async function fetchRemediationJobs(opts?: { limit?: number }): Promise<RemediationJobsResponse> {
  const limit = opts?.limit ?? 80
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

