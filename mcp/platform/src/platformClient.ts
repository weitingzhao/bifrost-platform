import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { defaultMcpTokenFilePath, readMcpTokenFile, resolveTokenFrom } from './tokenResolve.js'
import { bifrostSessionId } from './sessionId.js'
import { decideWrite, writesEnabled } from './writeGate.js'

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

async function rawSend(method: string, path: string, body?: unknown): Promise<unknown> {
  const withBody = method === 'POST' || method === 'PATCH'
  const headers = requestHeaders(withBody)
  const init: RequestInit = { method, headers }
  if (withBody) init.body = body == null ? '{}' : JSON.stringify(body)
  const r = await fetch(`${apiBase()}${path}`, init)
  const text = await r.text()
  if (!r.ok) throw new Error(`${method} ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
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
  const decision = decideWrite('POST', path, body, writesEnabled())
  if (decision.kind === 'blocked') return decision.body
  if (decision.kind === 'approval') return rawSend('POST', '/api/v1/approvals', decision.request)
  return rawSend('POST', path, body)
}

export async function platformPatch(path: string, body?: unknown): Promise<unknown> {
  return rawSend('PATCH', path, body)
}

export async function platformDelete(path: string): Promise<unknown> {
  const decision = decideWrite('DELETE', path, undefined, writesEnabled())
  if (decision.kind === 'blocked') return decision.body
  if (decision.kind === 'approval') return rawSend('POST', '/api/v1/approvals', decision.request)
  return rawSend('DELETE', path)
}

export function jsonResult(data: unknown) {
  return {
    content: [{ type: 'text' as const, text: JSON.stringify(data, null, 2) }],
  }
}
