import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { resolveTokenFrom } from './tokenResolve.js'

const base = process.env.PLATFORM_API_URL?.replace(/\/$/, '') ?? 'http://127.0.0.1:8780'

// Local :8780 tokens live only in bifrost-platform/.env, so MCP configs carry none.
// The fallback is read for a loopback base only: a local token must never be sent
// to another host.
const dotenvPath = fileURLToPath(new URL('../../../.env', import.meta.url))

function isLoopbackBase(): boolean {
  try {
    const host = new URL(base).hostname
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
  cachedToken = resolveTokenFrom(process.env, isLoopbackBase() ? dotenvValue : () => '')
  return cachedToken
}

function authHeaders(): HeadersInit {
  const token = resolveToken()
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (token !== '') headers.Authorization = `Bearer ${token}`
  return headers
}

export async function platformGet(path: string): Promise<unknown> {
  const r = await fetch(`${base}${path}`, { headers: authHeaders() })
  const text = await r.text()
  if (!r.ok) throw new Error(`GET ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export async function platformPost(path: string, body?: unknown): Promise<unknown> {
  const headers: Record<string, string> = {
    ...(authHeaders() as Record<string, string>),
    'Content-Type': 'application/json',
  }
  const r = await fetch(`${base}${path}`, {
    method: 'POST',
    headers,
    body: body == null ? '{}' : JSON.stringify(body),
  })
  const text = await r.text()
  if (!r.ok) throw new Error(`POST ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export async function platformPatch(path: string, body?: unknown): Promise<unknown> {
  const headers: Record<string, string> = {
    ...(authHeaders() as Record<string, string>),
    'Content-Type': 'application/json',
  }
  const r = await fetch(`${base}${path}`, {
    method: 'PATCH',
    headers,
    body: body == null ? '{}' : JSON.stringify(body),
  })
  const text = await r.text()
  if (!r.ok) throw new Error(`PATCH ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export async function platformDelete(path: string): Promise<unknown> {
  const r = await fetch(`${base}${path}`, { method: 'DELETE', headers: authHeaders() })
  const text = await r.text()
  if (!r.ok) throw new Error(`DELETE ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export function jsonResult(data: unknown) {
  return {
    content: [{ type: 'text' as const, text: JSON.stringify(data, null, 2) }],
  }
}
