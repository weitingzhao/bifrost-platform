import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { defaultMcpTokenFilePath, readMcpTokenFile, resolveTokenFrom } from './tokenResolve.js'
import { bifrostSessionId } from './sessionId.js'
import { OWNER_WAIT_NOTE, planWrite, writesMode } from './writeGate.js'

function apiBase(): string {
  return process.env.PLATFORM_API_URL?.replace(/\/$/, '') ?? 'http://127.0.0.1:8780'
}

// Process environment wins. When the pinned key is absent, read
// ~/.config/bifrost/mcp-tokens.env only if its mode is exactly 0600.
// A loopback PLATFORM_API_URL may then fall back to bifrost-platform/.env.
// A local .env token is never sent to a non-loopback host.
const dotenvPath = fileURLToPath(new URL('../../../.env', import.meta.url))

function isLoopbackBase(): boolean {
  try {
    const host = new URL(apiBase()).hostname
    return host === '127.0.0.1' || host === 'localhost' || host === '[::1]'
  } catch {
    return false
  }
}

function dotenvValue(key: string): string {
  let text: string
  try {
    text = readFileSync(dotenvPath, 'utf8')
  } catch {
    return ''
  }
  for (const line of text.split('\n')) {
    const m = line.match(/^\s*([A-Z0-9_]+)\s*=\s*(.*)$/)
    if (m && m[1] === key) return m[2].trim().replace(/^["']|["']$/g, '')
  }
  return ''
}

let cachedToken: string | undefined

function resolveToken(): string {
  if (cachedToken !== undefined) return cachedToken
  cachedToken = resolveTokenFrom(process.env, (key) => {
    const fromFile = readMcpTokenFile(defaultMcpTokenFilePath(), key)
    if (fromFile !== '') return fromFile
    return isLoopbackBase() ? dotenvValue(key) : ''
  })
  return cachedToken
}

/** Tests change env between cases. Not used by the server process. */
export function resetClientCacheForTests(): void {
  cachedToken = undefined
}

function requestHeaders(json: boolean): Record<string, string> {
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'X-Bifrost-Session': bifrostSessionId(),
  }
  const token = resolveToken()
  if (token !== '') headers.Authorization = `Bearer ${token}`
  if (json) headers['Content-Type'] = 'application/json'
  return headers
}

function sendsJsonBody(method: string): boolean {
  return method === 'POST' || method === 'PATCH' || method === 'PUT'
}

async function rawSend(method: string, path: string, body?: unknown): Promise<unknown> {
  const withBody = sendsJsonBody(method)
  const headers = requestHeaders(withBody)
  const init: RequestInit = { method, headers }
  if (withBody) init.body = body == null ? '{}' : JSON.stringify(body)
  const r = await fetch(`${apiBase()}${path}`, init)
  const text = await r.text()
  if (!r.ok) throw new Error(`${method} ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

type ApprovalAnswer = { kind: 'direct' } | { kind: 'stop'; body: unknown }

function asObject(value: unknown): Record<string, unknown> {
  if (value != null && typeof value === 'object' && !Array.isArray(value)) return value as Record<string, unknown>
  return {}
}

/** Ask the API which tier this action is. 400 call-directly means the original route. */
async function consultApproval(action: string, params: Record<string, unknown>): Promise<ApprovalAnswer> {
  const r = await fetch(`${apiBase()}/api/v1/approvals`, {
    method: 'POST',
    headers: requestHeaders(true),
    body: JSON.stringify({
      action,
      params,
      reason: `mcp:${action}`,
      rollback: 'Reverse this action with a new approval if it is executed.',
    }),
  })
  const text = await r.text()
  let parsed: Record<string, unknown> = {}
  if (text !== '') {
    try {
      parsed = asObject(JSON.parse(text) as unknown)
    } catch {
      parsed = { error: text }
    }
  }
  if (r.status === 400 && parsed.error === 'call directly') return { kind: 'direct' }
  if (r.status === 201) {
    return { kind: 'stop', body: { ...parsed, note: OWNER_WAIT_NOTE } }
  }
  if (r.status === 403) {
    const error = typeof parsed.error === 'string' && parsed.error !== '' ? parsed.error : 'forbidden'
    return { kind: 'stop', body: { ...parsed, error } }
  }
  const error = typeof parsed.error === 'string' && parsed.error !== '' ? parsed.error : `approval HTTP ${r.status}`
  return { kind: 'stop', body: { ...parsed, error, action, status: r.status } }
}

async function sendWrite(method: 'POST' | 'DELETE' | 'PUT', path: string, body?: unknown): Promise<unknown> {
  const plan = planWrite(method, path, body, writesMode())
  if (plan.kind === 'blocked' || plan.kind === 'unmapped') return plan.body
  if (plan.kind === 'consult') {
    const answer = await consultApproval(plan.action, plan.params)
    if (answer.kind === 'stop') return answer.body
  }
  return rawSend(method, path, body)
}

export async function platformGet(path: string): Promise<unknown> {
  return rawSend('GET', path)
}

/** Bypass the write gate. Local bdev tools and the approval routes use this. */
export async function platformSend(method: 'GET' | 'POST' | 'DELETE' | 'PATCH', path: string, body?: unknown): Promise<unknown> {
  if (method === 'POST' || method === 'PATCH') return rawSend(method, path, body)
  return rawSend(method, path)
}

export async function platformPost(path: string, body?: unknown): Promise<unknown> {
  return sendWrite('POST', path, body)
}

export async function platformPut(path: string, body?: unknown): Promise<unknown> {
  return sendWrite('PUT', path, body)
}

export async function platformPatch(path: string, body?: unknown): Promise<unknown> {
  return rawSend('PATCH', path, body)
}

export async function platformDelete(path: string): Promise<unknown> {
  return sendWrite('DELETE', path)
}

export function jsonResult(data: unknown) {
  return {
    content: [{ type: 'text' as const, text: JSON.stringify(data, null, 2) }],
  }
}
