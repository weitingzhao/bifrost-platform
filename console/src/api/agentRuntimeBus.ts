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
/** What the message routes answer a browser not signed in as the Owner (400, 401, 403). */
export const SIGN_IN_REFUSED = 'Connect with the Owner token to talk to the chief of staff.'
export const COORDINATOR_SIGN_IN = 'Connect with the Owner token to read the coordinator’s notes.'
/**
 * The coordinating thread (2.5 G3): it tells the Owner what it is doing and what is next as a note
 * in the Owner's mailbox (send_message to owner, kind note), and Work shows the latest one. Change it when
 * the coordinating thread changes.
 */
export const COORDINATOR_THREAD = import.meta.env.VITE_COORDINATOR_THREAD || 'thread:local_accccb4e-410c-4b5f-9f1b-b54c6915dc9a'
class ActionsUnavailable extends Error {}
async function busError(prefix: string, response: Response, token = '') {
  if (prefix === 'Messages' && [400, 401, 403].includes(response.status)) return new Error(SIGN_IN_REFUSED)
  if (prefix === 'Coordinator' && [400, 401, 403].includes(response.status)) return new Error(COORDINATOR_SIGN_IN)
  const r = record(await response.json().catch(() => ({})))
  // Only a platform without redis-agents keeps no actions; a 503 of a runtime
  // not connected yet is an error to show (review of 4a, C1).
  if (prefix === 'Actions' && response.status === 503 && r.error === 'unavailable') return new ActionsUnavailable()
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
export type CoordinatorNote = { body: string; at: string }
/** The latest note from `from` in a conversation read oldest first, or null. */
export function latestNote(thread: ChiefThread, from: string): CoordinatorNote | null {
  for (let i = thread.messages.length - 1; i >= 0; i--) {
    const m = thread.messages[i]
    if (m.from === from && m.kind === 'note') return { body: m.body, at: m.created_at }
  }
  return null
}
/** The coordinating thread's latest note to the Owner, among the latest 50 messages between them. */
export async function fetchCoordinatorNote(signal?: AbortSignal, thread = COORDINATOR_THREAD) {
  const read = parseChiefThread(await request('Coordinator', `/messages/thread?with=${encodeURIComponent(thread)}&limit=50`, { signal }))
  return latestNote(read, thread)
}
export const ackMessages = (ids: string[]) => request('Messages', '/messages/ack', { method: 'POST', body: JSON.stringify({ message_ids: ids }) })
