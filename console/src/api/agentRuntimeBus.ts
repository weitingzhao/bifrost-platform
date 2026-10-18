import { authedFetch } from '@/api/client'
import { record, records, text, type RuntimeRecord } from '@/api/agentRuntime'
import { signInSafeError } from '@/lib/signInError'

export type AttemptAction = { id: string; kind: string; target: string; at: string; code?: number; truncated: boolean }
export type AttemptActions = { attempt_id: string; actions: AttemptAction[]; next: string; unavailable: boolean }
export type ChiefMessage = { message_id: string; from: string; to: string; kind: string; body: string; created_at: string; acked_at: string; task_id: string }
export type ChiefThread = { actor: string; with: string; messages: ChiefMessage[] }
const number = (v: unknown) => typeof v === 'number' && Number.isInteger(v) ? v : undefined
export function parseAttemptActions(value: unknown): AttemptActions {
  const r = record(value)
  return { attempt_id: text(r.attempt_id), next: text(r.next), unavailable: r.error === 'unavailable',
    actions: records(r.actions).map(a => ({ id: text(a.id), kind: text(a.kind), target: text(a.target), at: text(a.at),
      code: number(a.code), truncated: a.truncated === true })) }
}
export function parseChiefThread(value: unknown): ChiefThread {
  const r = record(value)
  return { actor: text(r.actor), with: text(r.with), messages: records(r.messages).map(m => ({
    message_id: text(m.message_id), from: text(m.from), to: text(m.to), kind: text(m.kind), body: text(m.body),
    created_at: text(m.created_at), acked_at: text(m.acked_at), task_id: text(m.task_id),
  })) }
}
class ActionsUnavailable extends Error {}
async function busError(prefix: string, response: Response, token = '') {
  if (prefix === 'Actions' && response.status === 503) return new ActionsUnavailable()
  if (prefix === 'Messages' && [400, 401].includes(response.status)) return new Error('Connect with the Owner token to talk to the chief of staff.')
  const r = record(await response.json().catch(() => ({})))
  return new Error(signInSafeError(text(r.message) || text(r.error) || `${prefix}: HTTP ${response.status}`, token, response.status))
}
async function request(prefix: string, path: string, init?: RequestInit): Promise<RuntimeRecord> {
  const response = await authedFetch(prefix, `/api/v1/agentrt${path}`, init, busError)
  return record(await response.json())
}
export async function fetchAttemptActions(id: string, options: { last?: number; after?: string; limit?: number } = {}, signal?: AbortSignal): Promise<AttemptActions> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(options)) if (value !== undefined) params.set(key, String(value))
  try { return parseAttemptActions(await request('Actions', `/attempts/${encodeURIComponent(id)}/actions?${params}`, { signal })) }
  catch (e) { if (e instanceof ActionsUnavailable) return { attempt_id: id, actions: [], next: options.after ?? '', unavailable: true }; throw e }
}
export function setGlobalStop(reason: string) {
  return request('Control', '/controls', { method: 'POST', body: JSON.stringify({ idempotency_key: crypto.randomUUID(), scope: 'global', kind: 'stop', reason }) })
}
export async function liftControl(id: string) {
  await authedFetch('Control', `/api/v1/agentrt/controls/${encodeURIComponent(id)}`, { method: 'DELETE' }, busError)
}
export const messageBytes = (body: string) => new TextEncoder().encode(body).length
export function sendChiefMessage(body: string) {
  if (messageBytes(body) < 1 || messageBytes(body) > 8192) return Promise.reject(new Error('Message must be 1 to 8192 bytes.'))
  return request('Messages', '/messages', { method: 'POST', body: JSON.stringify({ to: 'seat:chief_of_staff', kind: 'ask', body }) })
}
export async function fetchChiefThread(signal?: AbortSignal) {
  return parseChiefThread(await request('Messages', '/messages/thread?with=seat:chief_of_staff&limit=50', { signal }))
}
export const ackMessages = (ids: string[]) => request('Messages', '/messages/ack', { method: 'POST', body: JSON.stringify({ message_ids: ids }) })
