import type {
  CloseOperateQueueRequest,
  EnqueueOperateQueueRequest,
  OperateQueueResponse,
} from './operateQueueTypes'
import { authHeaders, parseError } from './client'

export const OPERATE_QUEUE_QUERY_KEY = ['operate', 'queue'] as const

export async function fetchOperateQueue(): Promise<OperateQueueResponse> {
  const r = await fetch('/api/v1/operate/queue')
  if (!r.ok) throw await parseError('operate queue', r)
  return r.json() as Promise<OperateQueueResponse>
}

export async function enqueueOperateQueueItem(
  body: EnqueueOperateQueueRequest,
): Promise<import('./operateQueueTypes').OperateQueueItem> {
  const headers = authHeaders(true)
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
  const headers = authHeaders(true)
  const r = await fetch(`/api/v1/operate/queue/${encodeURIComponent(itemId)}/close`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('operate queue close', r)
  return r.json() as Promise<import('./operateQueueTypes').OperateQueueItem>
}

export async function dismissOperateQueueItem(
  itemId: string,
  body: import('./operateQueueTypes').DismissOperateQueueRequest,
): Promise<import('./operateQueueTypes').OperateQueueItem> {
  const headers = authHeaders(true)
  const r = await fetch(`/api/v1/operate/queue/${encodeURIComponent(itemId)}/dismiss`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('operate queue dismiss', r)
  return r.json() as Promise<import('./operateQueueTypes').OperateQueueItem>
}

export async function recordOperateQueueExecution(
  itemId: string,
  executionJobId: string,
): Promise<import('./operateQueueTypes').OperateQueueItem> {
  const headers = authHeaders(true)
  const r = await fetch(`/api/v1/operate/queue/${encodeURIComponent(itemId)}/execution`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ execution_job_id: executionJobId }),
  })
  if (!r.ok) throw await parseError('operate queue execution', r)
  return r.json() as Promise<import('./operateQueueTypes').OperateQueueItem>
}
