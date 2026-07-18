import type {
  CloseOperateQueueRequest,
  EnqueueOperateQueueRequest,
  OperateQueueResponse,
} from './operateQueueTypes'
import { getPlatformOperatorToken } from '@/lib/platformAuth'

async function parseError(prefix: string, r: Response): Promise<Error> {
  let detail = `HTTP ${r.status}`
  try {
    const body = (await r.json()) as { error?: string; message?: string }
    detail = body.error ?? body.message ?? detail
  } catch {
    // keep status detail
  }
  return new Error(`${prefix}: ${detail}`)
}

export const OPERATE_QUEUE_QUERY_KEY = ['operate', 'queue'] as const

export async function fetchOperateQueue(): Promise<OperateQueueResponse> {
  const r = await fetch('/api/v1/operate/queue')
  if (!r.ok) throw await parseError('operate queue', r)
  return r.json() as Promise<OperateQueueResponse>
}

export async function enqueueOperateQueueItem(
  body: EnqueueOperateQueueRequest,
): Promise<import('./operateQueueTypes').OperateQueueItem> {
  const token = getPlatformOperatorToken()
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch('/api/v1/operate/queue', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('operate queue enqueue', r)
  return r.json() as Promise<import('./operateQueueTypes').OperateQueueItem>
}

export async function closeOperateQueueItem(
  itemId: string,
  body: CloseOperateQueueRequest,
): Promise<import('./operateQueueTypes').OperateQueueItem> {
  const token = getPlatformOperatorToken()
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(`/api/v1/operate/queue/${encodeURIComponent(itemId)}/close`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('operate queue close', r)
  return r.json() as Promise<import('./operateQueueTypes').OperateQueueItem>
}

export async function recordOperateQueueExecution(
  itemId: string,
  executionJobId: string,
): Promise<import('./operateQueueTypes').OperateQueueItem> {
  const token = getPlatformOperatorToken()
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(`/api/v1/operate/queue/${encodeURIComponent(itemId)}/execution`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ execution_job_id: executionJobId }),
  })
  if (!r.ok) throw await parseError('operate queue execution', r)
  return r.json() as Promise<import('./operateQueueTypes').OperateQueueItem>
}
